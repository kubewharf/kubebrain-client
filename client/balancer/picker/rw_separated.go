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
	"strings"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"k8s.io/klog/v2"
)

type config struct {
	readySCs map[resolver.Address]balancer.SubConn
	logLevel klog.Level
}

const (
	rwSeparatedRoundRobinPicker = "picker-rw-separated-round-robin"
)

func newRWSeparatedRoundRobinBalanced(conf config) balancer.Picker {

	addrToIdx := make(map[string]int)
	rawAddrToSC := make(map[string]balancer.SubConn)
	scs := make([]balancer.SubConn, 0, len(conf.readySCs))
	addrs := make([]string, 0, len(conf.readySCs))
	key := ""

	for addr, sc := range conf.readySCs {
		addrToIdx[addr.Addr] = len(scs)
		rawAddrToSC[addr.Addr] = sc
		scs = append(scs, sc)
		key = addr.Addr // set key to the last value (random one)
		addrs = append(addrs, addr.Addr)
	}

	klog.V(conf.logLevel).InfoS("new rw separated rr balancer", "addresses", addrs)
	return &rwSeparatedRoundBalanced{
		addrToSC:    conf.readySCs,
		rawAddrToSC: rawAddrToSC,
		logLevel:    conf.logLevel,
		key:         key,
		record:      globalLeaderCtrl.getLeaderRecord(key),
		readerAddrs: make([]resolver.Address, 0, len(conf.readySCs)),
	}
}

type rwSeparatedRoundBalanced struct {
	logLevel    klog.Level
	key         string
	record      *leaderRecord
	mu          sync.RWMutex
	rawAddrs    []string
	addrToSC    map[resolver.Address]balancer.SubConn
	rawAddrToSC map[string]balancer.SubConn
	nextReader  int64
	leader      string             // protected by mu
	readerAddrs []resolver.Address // protected by mu
}

// Pick is called for every client request.
func (rw *rwSeparatedRoundBalanced) Pick(ctx context.Context, opts balancer.PickOptions) (sc balancer.SubConn, f func(balancer.DoneInfo), err error) {

	n := len(rw.addrToSC)
	if n == 0 {
		klog.ErrorS(balancer.ErrNoSubConnAvailable, "addrs", rw.rawAddrs)
		return nil, nil, balancer.ErrNoSubConnAvailable
	}

	//! there may be a difference between the pick result and real expected.
	//! the difference can not be avoided by lock, but it can work well.
	//! - for read method:
	//! 	current resp should be right which may be returned by leader,
	//!		and other reqs will be assigned to reader soon
	//! - for write method:
	//!		current resp should be wrong while req was assigned to reader or conn has been closed,
	//!		and a healthCheck will be triggered to correct the assignment soon.
	p := rw.selectPickFunc(opts)
	sc, picked, err := p()
	if err != nil {
		return nil, nil, balancer.ErrNoSubConnAvailable
	}

	klog.V(rw.logLevel).InfoS("picked",
		"picker", rwSeparatedRoundRobinPicker,
		"address", picked,
		"subconn-size", n,
	)

	doneFunc := func(info balancer.DoneInfo) {
		// TODO: error handling?
		fss := []interface{}{
			"picker", rwSeparatedRoundRobinPicker,
			"address", picked,
			"success", info.Err == nil,
			"bytes-sent", info.BytesSent,
			"bytes-received", info.BytesReceived,
		}
		if info.Err == nil {
			klog.V(rw.logLevel).InfoS("balancer done", fss...)
		} else {
			klog.ErrorS(info.Err, "balancer failed", fss...)
		}
	}
	return sc, doneFunc, nil
}

type pickerFunc func() (balancer.SubConn, string, error)

func (rw *rwSeparatedRoundBalanced) pickReader() (sc balancer.SubConn, picked string, err error) {
	_ = rw.updateLeader()

	rw.mu.RLock()
	defer rw.mu.RUnlock()
	l := len(rw.readerAddrs)
	if l < 1 {
		sc, picked, err = rw.pickWriter()
		return
	}

	cur := int(atomic.AddInt64(&rw.nextReader, 1)-1) % l
	picked = rw.readerAddrs[cur].Addr
	sc = rw.rawAddrToSC[picked]

	return sc, picked, err

}

func (rw *rwSeparatedRoundBalanced) pickWriter() (sc balancer.SubConn, picked string, err error) {
	// pick the leaders
	picked = rw.updateLeader()
	// if there is no leader, just return no available and wait for updating
	exist := false
	sc, exist = rw.rawAddrToSC[picked]
	if !exist {
		klog.ErrorS(balancer.ErrNoSubConnAvailable, "failed to pick writer", "picked", picked, "addrs", rw.rawAddrs)
		sc, picked, err = nil, "", balancer.ErrNoSubConnAvailable
	}

	return sc, picked, err

}

func (rw *rwSeparatedRoundBalanced) updateLeader() string {

	// fast compare without write lock
	if equal, curLeader := rw.compareLeader(); equal {
		return curLeader
	}

	// if there is no new leader, lock will be released quickly
	rw.mu.Lock()
	defer rw.mu.Unlock()

	// double check
	curLeader := rw.record.getLeader()
	if curLeader == rw.leader {
		return curLeader
	}

	// update leader and reader scs
	rw.leader = curLeader               // curLeader may be "", which means no leader now
	rw.readerAddrs = rw.readerAddrs[:0] // reset
	for addr := range rw.addrToSC {
		if addr.Addr != curLeader {
			rw.readerAddrs = append(rw.readerAddrs, addr)
		}
	}
	return curLeader
}

func (rw *rwSeparatedRoundBalanced) compareLeader() (bool, string) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	cur := rw.record.getLeader()
	return cur == rw.leader, cur
}

const (
	read = "/Read"
)

func (rw *rwSeparatedRoundBalanced) selectPickFunc(opts balancer.PickOptions) pickerFunc {
	if strings.HasPrefix(opts.FullMethodName, read) {
		return rw.pickReader
	} else {
		return rw.pickWriter
	}
}
