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
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

const (
	writeTest = "/Write/Test"
	readTest  = "/Read/Test"
)

type mockSubConn struct{}

func newMockSubConn() balancer.SubConn {
	return &mockSubConn{}
}

func (m *mockSubConn) UpdateAddresses(addresses []resolver.Address) {
	panic("implement me")
}

func (m *mockSubConn) Connect() {
	panic("implement me")
}

func TestRwSeperatedRoundBalanced_Pick(t *testing.T) {

	mockLeaderSubConn := newMockSubConn()
	mockFollower1SubConn := newMockSubConn()
	mockFollower2SubConn := newMockSubConn()
	addr1 := resolver.Address{Addr: "1"}
	addr2 := resolver.Address{Addr: "2"}
	addr3 := resolver.Address{Addr: "3"}

	addrToSC := map[resolver.Address]balancer.SubConn{
		addr1: mockLeaderSubConn,
		addr2: mockFollower1SubConn,
		addr3: mockFollower2SubConn,
	}

	var epsStrs []string
	for addr := range addrToSC {
		epsStrs = append(epsStrs, addr.Addr)
	}
	leaderAddr := addr1
	RegisterCluster(epsStrs, func() (resolver.Address, error) {
		return leaderAddr, nil
	})
	defer UnregisterCluster(epsStrs)

	picker := newRWSeparatedRoundRobinBalanced(config{
		logLevel: 8,
		readySCs: addrToSC,
	})

	times := 10
	ctx := context.Background()

	// leader only
	t.Run("leader", func(t *testing.T) {
		ast := assert.New(t)
		for i := 0; i < times; i++ {
			sc, _, err := picker.Pick(ctx, balancer.PickInfo{
				FullMethodName: writeTest,
				Ctx:            ctx,
			})
			ast.NoError(err)
			actual := sc.(*mockSubConn)
			ast.Same(actual, mockLeaderSubConn)
		}
	})

	// follower only
	t.Run("follower", func(t *testing.T) {
		ast := assert.New(t)
		followerAddrs := picker.(*rwSeparatedRoundBalanced).readerAddrs
		ast.NotEmpty(followerAddrs)
		for _, followerAddr := range followerAddrs {
			ast.NotEqual(leaderAddr, followerAddr)
		}

		expected := 0
		for i := 0; i < times; i++ {
			actual, _, err := picker.Pick(ctx, balancer.PickInfo{
				FullMethodName: readTest,
				Ctx:            nil,
			})
			ast.NoError(err)
			ast.Equal(addrToSC[followerAddrs[expected%len(followerAddrs)]], actual)
			expected++
		}
	})

}

func TestRwSeperatedRoundBalanced_PickWhileConnFail(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockLeaderSubConn := newMockSubConn()
	mockFollower2SubConn := newMockSubConn()
	addr1 := resolver.Address{Addr: "1"}
	addr2 := resolver.Address{Addr: "2"}
	addr3 := resolver.Address{Addr: "3"}

	addrToSC := map[resolver.Address]balancer.SubConn{
		addr1: mockLeaderSubConn,
		addr3: mockFollower2SubConn,
	}
	var epsStrs []string
	for _, addr := range []resolver.Address{addr1, addr2, addr3} {
		epsStrs = append(epsStrs, addr.Addr)
	}

	leaderAddr := addr1
	RegisterCluster(epsStrs, func() (resolver.Address, error) { // register with 3 endpoints
		return leaderAddr, nil
	})
	defer UnregisterCluster(epsStrs)

	picker := newRWSeparatedRoundRobinBalanced(config{
		logLevel: 8,
		readySCs: addrToSC,
	})

	times := 10
	ctx := context.Background()

	// leader only
	t.Run("leader", func(t *testing.T) {
		ast := assert.New(t)
		for i := 0; i < times; i++ {
			sc, _, err := picker.Pick(ctx, balancer.PickInfo{
				FullMethodName: writeTest,
				Ctx:            ctx,
			})
			ast.NoError(err)
			actual := sc.(*mockSubConn)
			ast.Same(mockLeaderSubConn, actual)
		}
	})

	// follower only
	t.Run("follewer", func(t *testing.T) {
		ast := assert.New(t)
		followerAddrs := picker.(*rwSeparatedRoundBalanced).readerAddrs
		ast.Equal(1, len(followerAddrs))
		for _, followerAddr := range followerAddrs {
			ast.NotEqual(leaderAddr, followerAddr)
		}

		expected := 0
		for i := 0; i < times; i++ {
			actual, _, err := picker.Pick(ctx, balancer.PickInfo{
				FullMethodName: readTest,
				Ctx:            nil,
			})
			ast.NoError(err)
			ast.Equal(addrToSC[followerAddrs[expected%len(followerAddrs)]], actual)
			expected++
		}
	})

}

func TestRwSeperatedRoundBalanced_SingleNodeCluster(t *testing.T) {
	ast := assert.New(t)

	epsStrs := []string{"single"}
	addr := resolver.Address{Addr: "single"}

	ast.NotPanics(func() {
		RegisterCluster(epsStrs, func() (resolver.Address, error) { // register with 1 endpoint
			return addr, nil
		})

		defer UnregisterCluster(epsStrs)
		sc := newMockSubConn()

		addrToSC := map[resolver.Address]balancer.SubConn{
			addr: sc,
		}
		ctx := context.Background()
		// expected no panic
		picker := newRWSeparatedRoundRobinBalanced(config{
			logLevel: 8,
			readySCs: addrToSC,
		})

		actual, done, err := picker.Pick(ctx, balancer.PickInfo{
			FullMethodName: readTest,
			Ctx:            ctx,
		})
		ast.NoError(err)
		ast.Equal(actual, sc)

		done(balancer.DoneInfo{})
		return
	})

}
