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

package resolver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"google.golang.org/grpc/resolver"
	_ "google.golang.org/grpc/resolver/dns"
	_ "google.golang.org/grpc/resolver/passthrough"
)

var (
	builder *customResolverBuilder
)

const (
	scheme = "brain"
)

func init() {
	builder = &customResolverBuilder{
		scheme:         scheme,
		resolverGroups: make(map[string]*customResolverGroup),
	}
	resolver.Register(builder)
}

type customResolverBuilder struct {
	// config
	scheme string

	// state
	mu             sync.RWMutex
	resolverGroups map[string]*customResolverGroup
}

var _ resolver.Builder = &customResolverBuilder{}

// Build implements resolver.Builder interface
func (b *customResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	id := target.Authority
	group, err := b.getResolverGroup(id)
	if err != nil {
		return nil, fmt.Errorf("failed to build resolver: %v", err)
	}
	cr := &customResolver{
		endpointID: id,
		cc:         cc,
	}

	group.addResolver(cr)
	return cr, nil
}

// Scheme implements resolver.Builder interface
func (b *customResolverBuilder) Scheme() string {
	return b.scheme
}

func (b *customResolverBuilder) getResolverGroup(id string) (*customResolverGroup, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rg, ok := b.resolverGroups[id]
	if !ok {
		return nil, fmt.Errorf("ResolverGroup not found for id %s", id)
	}
	return rg, nil
}

func (b *customResolverBuilder) removeResolverGroup(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.resolverGroups, id)
}

func (b *customResolverBuilder) newResolverGroup(id string) *customResolverGroup {
	b.mu.Lock()
	defer b.mu.Unlock()
	crg := &customResolverGroup{id: id, b: b}
	b.resolverGroups[id] = crg
	return crg
}

type ResolverGroup interface {
	// Target generates a target for grpc to dial
	Target() string

	// SetAddrs input a list of rawAddrs as addresses to added
	SetAddrs(addrs []string)

	// Close closes the ResolverGroup
	Close()
}

func NewResolverGroup(id string) ResolverGroup {
	return builder.newResolverGroup(id)
}

// customResolver maps an endpoint to all rawAddrs in the cluster
type customResolverGroup struct {
	b         *customResolverBuilder
	mu        sync.RWMutex
	id        string
	addrs     []resolver.Address
	resolvers []*customResolver
}

// SetAddrs implement ResolverGroup interface
func (crg *customResolverGroup) SetAddrs(rawAddrs []string) {
	addrs := raw2Addrs(rawAddrs...)
	crg.mu.Lock()
	crg.addrs = addrs
	for _, cr := range crg.resolvers {
		cr.cc.UpdateState(resolver.State{Addresses: addrs})
	}
	crg.mu.Unlock()
}

func (crg *customResolverGroup) addResolver(cr *customResolver) {
	crg.mu.Lock()
	addrs := crg.addrs
	crg.resolvers = append(crg.resolvers, cr)
	crg.mu.Unlock()

	cr.cc.UpdateState(resolver.State{
		Addresses: addrs,
	})
}

// Target implement ResolverGroup interface
func (crg *customResolverGroup) Target() string {
	return fmt.Sprintf("%s://%s/endpoint", crg.b.scheme, crg.id)
}

func (crg *customResolverGroup) removeResolver(r *customResolver) {
	crg.mu.Lock()
	defer crg.mu.Unlock()

	// remove customResolver from list
	for i, er := range crg.resolvers {
		if er == r {
			crg.resolvers = append(crg.resolvers[:i], crg.resolvers[i+1:]...)
			break
		}
	}
}

// Close implement ResolverGroup interface
func (crg *customResolverGroup) Close() {
	crg.b.removeResolverGroup(crg.id)
}

func raw2Addrs(rawAddrs ...string) (addrs []resolver.Address) {
	addrs = make([]resolver.Address, 0, len(rawAddrs))
	for _, rawAddr := range rawAddrs {
		addrs = append(addrs, resolver.Address{Addr: rawAddr})
	}
	return addrs
}

func ParseEndpoint(endpoint string) (proto string, host string, scheme string) {
	proto = "tcp"
	host = endpoint
	url, uerr := url.Parse(endpoint)
	if uerr != nil || !strings.Contains(endpoint, "://") {
		return proto, host, scheme
	}
	scheme = url.Scheme

	// strip scheme:// prefix since grpc dials by host
	host = url.Host
	switch url.Scheme {
	case "http", "https":
	case "unix", "unixs":
		proto = "unix"
		host = url.Host + url.Path
	default:
		proto, host = "", ""
	}
	return proto, host, scheme
}

// Dialer dials an endpoint using net.Dialer.
// Context cancellation and timeout are supported.
func Dialer(ctx context.Context, dialEp string) (net.Conn, error) {
	proto, host, _ := ParseEndpoint(dialEp)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	dialer := &net.Dialer{}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	return dialer.DialContext(ctx, proto, host)
}

var _ resolver.Resolver = &customResolver{}

type customResolver struct {
	endpointID string
	cc         resolver.ClientConn
	mu         sync.RWMutex
}

// ResolveNow implements resolver.Resolver interface
func (*customResolver) ResolveNow(o resolver.ResolveNowOption) {}

// Close implement resolver.Resolver interface
func (cr *customResolver) Close() {
	rg, err := builder.getResolverGroup(cr.endpointID)
	if err != nil {
		return
	}
	rg.removeResolver(cr)
}
