// Copyright 2022 ByteDance and/or its affiliates
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package picker

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/resolver"
)

var globalLeaderCtrl = &leaderCtrl{
	leaderIndex: make(map[string]*leaderRecord),
}

type leaderCtrl struct {
	// leaderIndex restore the leaders of clusters
	leaderIndex map[string]*leaderRecord
	sync.RWMutex
	singleflight.Group
}

func (l *leaderCtrl) getLeaderRecord(key string) *leaderRecord {
	l.RLock()
	defer l.RUnlock()
	ret := l.leaderIndex[key]
	if ret == nil {
		ret = &leaderRecord{}
	}
	return ret
}

func (l *leaderCtrl) needUpdateLeader(key string, leader string) bool {
	ret := false
	l.RLock()
	defer l.RUnlock()
	record, ok := l.leaderIndex[key]
	if !ok {
		return false
	}
	ret = record.getLeader() == leader
	return ret
}

// updateClusterLeader is safe for calling concurrently to update cluster info
func (l *leaderCtrl) updateClusterLeader(clusterKey string, leader string) {
	l.RLock()
	defer l.RUnlock()
	record, ok := l.leaderIndex[clusterKey]
	if !ok {
		return
	}
	record.setLeader(leader)
}

func (l *leaderCtrl) updateClusterAmount(eps []string, amount int, cb func() (resolver.Address, error)) {
	if len(eps) < 0 {
		return
	}

	clusterKey := marshalEndpoints(eps)

	l.Lock()
	defer l.Unlock()

	record, ok := l.leaderIndex[clusterKey]
	if !ok && amount > 0 {
		record = newLeaderRecord(cb)
		l.leaderIndex[clusterKey] = record
		if len(eps) == 1 {
			return
		}

		for _, ep := range eps {
			if _, ok := l.leaderIndex[ep]; ok {
				record.close()
				panic("do not support one endpoint in different cluster")
			}
			l.leaderIndex[ep] = record
		}

		return
	} else if !ok && amount <= 0 {
		return
	}

	record.amount += amount
	if record.amount <= 0 {
		delete(l.leaderIndex, clusterKey)
		for _, ep := range eps {
			delete(l.leaderIndex, ep)
		}
		record.cancel()
		l.Forget(clusterKey)
	}
}

func (l *leaderCtrl) getLeaderAddr(key string) string {
	addr := ""
	l.RLock()
	defer l.RUnlock()
	record, ok := l.leaderIndex[key]
	if !ok {
		return ""
	}
	addr = record.getLeader()
	return addr
}

type leaderRecord struct {
	amount int
	leader string
	ctx    context.Context
	mu     sync.RWMutex
	cancel func()
	cb     func() (resolver.Address, error)
}

func newLeaderRecord(cb func() (resolver.Address, error)) *leaderRecord {
	ctx, cancel := context.WithCancel(context.Background())
	l := &leaderRecord{
		amount: 1,
		ctx:    ctx,
		mu:     sync.RWMutex{},
		cancel: cancel,
		cb:     cb,
	}
	addr, _ := cb()
	l.leader = addr.Addr
	go l.healthCheck()
	return l
}

func (l *leaderRecord) setLeader(leader string) {
	if l.getLeader() == leader {
		// if leader is not change,
		// just return to avoid exclusive lock
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.leader = leader
}

func (l *leaderRecord) getLeader() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.leader
}

func (l *leaderRecord) healthCheck() {
	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			addr, err := l.cb()
			if err != nil {
				continue
			}
			l.setLeader(addr.Addr)
		}
	}
}

func (l *leaderRecord) close() {
	l.cancel()
}

func marshalEndpoints(endpoints []string) string {
	sortedEndpoints := make([]string, len(endpoints))
	copy(sortedEndpoints, endpoints)
	sort.Strings(sortedEndpoints)
	return strings.Join(sortedEndpoints, ",")
}

func RegisterCluster(endpoint []string, cb func() (resolver.Address, error)) {
	globalLeaderCtrl.updateClusterAmount(endpoint, 1, cb)
	updateClusterLeader(marshalEndpoints(endpoint), cb)
}

func UpdateClusterLeader(endpoints []string, cb func() (resolver.Address, error)) {
	key := marshalEndpoints(endpoints)
	updateClusterLeader(key, cb)
}

func updateClusterLeader(clusterKey string, cb func() (resolver.Address, error)) {
	if cb == nil {
		return
	}
	// use single flight to avoid duplicate updating on the same cluster
	// but concurrent updating on different cluster is allowed
	_, _, _ = globalLeaderCtrl.Do(clusterKey, func() (interface{}, error) {
		leaderAddr := globalLeaderCtrl.getLeaderAddr(clusterKey)
		if leaderAddr != "" {
			// if leader is set, just return
			// it will be maintained by periodic health checking
			return nil, nil
		}

		leader, err := cb()
		if err == nil {
			globalLeaderCtrl.updateClusterLeader(clusterKey, leader.Addr)
		}
		return nil, err
	})
}

func UnregisterCluster(endpoints []string) {
	if len(endpoints) < 0 {
		return
	}
	globalLeaderCtrl.updateClusterAmount(endpoints, -1, nil)
}

func GetLeader(endpoints []string) string {
	if len(endpoints) < 0 {
		return ""
	}
	return globalLeaderCtrl.getLeaderAddr(endpoints[0])
}
