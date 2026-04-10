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

package client

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	iresolver "github.com/kubewharf/kubebrain-client/client/balancer/resolver"
	ierrors "github.com/kubewharf/kubebrain-client/errors"
)

type leaderChecker struct {
	Config
	grpcDialOpts []grpc.DialOption
}

func newLeaderChecker(config Config, opts []grpc.DialOption) *leaderChecker {
	return &leaderChecker{
		Config:       config,
		grpcDialOpts: opts,
	}
}

func (l *leaderChecker) checkLeader() (resolver.Address, error) {
	klog.V(l.LogLevel).InfoS("start leader checking", "endpoints", l.Endpoints)
	size := len(l.Endpoints)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	addrc := make(chan resolver.Address)
	errc := make(chan error)
	var wg sync.WaitGroup
	wg.Add(size)
	for i := 0; i < size; i++ {
		go func(i int) {
			defer wg.Done()
			ep := l.Endpoints[i]
			if isHealth, _ := l.grpcHealthCheck(ctx, ep); isHealth {
				addr := ep2Addr(ep)
				select {
				case addrc <- addr: // only accept the first one
				case <-ctx.Done():
				}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		select {
		case <-ctx.Done():
		case errc <- ierrors.ErrNoLeader:
		}
	}()

	select {
	case addr := <-addrc:
		klog.V(l.LogLevel).InfoS("cur leader", "leader", addr.Addr)
		return addr, nil
	case err := <-errc:
		klog.V(l.ErrLogLevel).ErrorS(err, "no leader")
		return resolver.Address{}, err
	case <-ctx.Done(): // cause only by timeout
		klog.V(l.LogLevel).InfoS("time out")
		return resolver.Address{}, ierrors.ErrNoLeader
	}

}

func (l *leaderChecker) grpcHealthCheck(ctx context.Context, ep string) (isHealth bool, err error) {
	_, host, _ := iresolver.ParseEndpoint(ep)
	ep = "passthrough:///" + host

	klog.V(l.LogLevel).InfoS("start check endpoints", "endpoint", ep)
	conn, err := grpc.DialContext(ctx, ep, l.grpcDialOpts...)
	if err != nil {
		if status.Code(err) != codes.Canceled {
			klog.V(l.ErrLogLevel).ErrorS(err, "dial failed", "endpoints", ep)
		}
		return false, err
	}
	defer conn.Close()
	cli := grpc_health_v1.NewHealthClient(conn)
	resp, err := cli.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		if status.Code(err) != codes.Canceled {
			klog.V(l.ErrLogLevel).ErrorS(err, "check failed", "endpoints", ep)
		}
		return false, err
	}

	klog.V(l.LogLevel).InfoS("health check", "endpoints", ep, "status", resp.Status.String())
	return resp.Status == grpc_health_v1.HealthCheckResponse_SERVING, nil
}

func ep2Addr(ep string) resolver.Address {
	return resolver.Address{Addr: ep}
}
