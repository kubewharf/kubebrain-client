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
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
	"k8s.io/klog/v2"

	"github.com/kubewharf/kubebrain-client/client/balancer/resolver"
)

type transportCredentials struct {
	credentials.TransportCredentials
	config         Config
	mu             sync.RWMutex
	addrToEndpoint map[string]string
}

func newTransportCredentials(config Config) *transportCredentials {
	return &transportCredentials{
		TransportCredentials: credentials.NewTLS(config.TLS),
		addrToEndpoint:       make(map[string]string),
		config:               config,
	}
}

func (t *transportCredentials) ClientHandshake(ctx context.Context, auth string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {

	// there are mainly 2 cases of handshake
	// 1. health check with `passthrouth`: `auth` is set as `${ip}:${port}`, such as `127.0.0.1:3379`/ `127.0.0.1:4379`
	// 2. main conn in used: `auth` will be `endpoints` which is given by resolver
	// in all cases, it's expected that the given `auth` is replaced by value mapped by remote addr in `addrToEndpoints`
	t.mu.Lock()
	dialEp, ok := t.addrToEndpoint[conn.RemoteAddr().String()]
	t.mu.Unlock()
	actualAuth := auth
	if ok {
		_, host, _ := resolver.ParseEndpoint(dialEp)
		actualAuth = host
	}
	klog.V(t.config.LogLevel).InfoS("client handshake", "auth", auth,
		"actualAuth", actualAuth,
		"conn", conn.RemoteAddr().String())
	return t.TransportCredentials.ClientHandshake(ctx, actualAuth, conn)
}

func (t *transportCredentials) Clone() credentials.TransportCredentials {
	replica := map[string]string{}
	t.mu.Lock()
	for k, v := range t.addrToEndpoint {
		replica[k] = v
	}
	t.mu.Unlock()
	return &transportCredentials{
		TransportCredentials: t.TransportCredentials.Clone(),
		addrToEndpoint:       replica,
		config:               t.config,
	}
}

func (t *transportCredentials) updateAddrToEndpoint(dialEp string, conn net.Conn) {
	if conn != nil {
		t.mu.Lock()
		t.addrToEndpoint[conn.RemoteAddr().String()] = dialEp
		t.mu.Unlock()
	}
	return
}
