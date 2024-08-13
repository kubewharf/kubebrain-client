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
	"crypto/tls"
	"net"
	"net/url"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"

	rpc "github.com/kubewharf/kubebrain-client/api/v2rpc"
	"github.com/kubewharf/kubebrain-client/client/balancer/picker"
	iresolver "github.com/kubewharf/kubebrain-client/client/balancer/resolver"
)

// Config is the configuration of client
type Config struct {
	// Endpoints is a list of URLs
	Endpoints []string `json:"endpoints"`

	// TLS is the tls config for handshake with server
	TLS *tls.Config

	// LogLevel is the level of info log
	LogLevel klog.Level `json:"log-level"`

	// ErrLogLevel is the level of error log
	ErrLogLevel klog.Level `json:"err-log-level"`

	// MaxCallSendMsgSize is the client-side request send limit in bytes.
	// If 0, it defaults to 2.0 MiB (2 * 1024 * 1024).
	// Make sure that "MaxCallSendMsgSize" < server-side default send/recv limit.
	MaxCallSendMsgSize int `json:"max-call-send-msg-size"`

	// MaxCallRecvMsgSize is the client-side response receive limit.
	// If 0, it defaults to "math.MaxInt32", because range response can
	// easily exceed request send limits.
	// Make sure that "MaxCallRecvMsgSize" >= server-side default send/recv limit.
	MaxCallRecvMsgSize int `json:"max-call-recv-msg-size"`
}

type clientImpl struct {
	config Config

	// cc is a client conn dialed by a client uid, which connect multiple server by resolver
	// and sub conn will be picked with the policy that separates the request of reading and writing
	cc *grpc.ClientConn

	rg iresolver.ResolverGroup

	rpcClient *rpcClient

	scheme string
	creds  *transportCredentials
	ctx    context.Context
	cancel context.CancelFunc
}

type rpcClient struct {
	rpc.ReadClient
	rpc.WriteClient
	rpc.WatchClient
}

func newRpcClient(conn *grpc.ClientConn) *rpcClient {
	c := &rpcClient{}
	c.ReadClient = rpc.NewReadClient(conn)
	c.WriteClient = rpc.NewWriteClient(conn)
	c.WatchClient = rpc.NewWatchClient(conn)
	return c
}

func NewClient(config Config) (Client, error) {

	// check if config is valid
	if len(config.Endpoints) == 0 {
		return nil, errors.New("no available endpoint")
	}
	return newClient(config)
}

func newClient(config Config) (*clientImpl, error) {
	var err error
	c := &clientImpl{config: config}

	if config.TLS != nil {
		c.creds = newTransportCredentials(config)
	}

	// init scheme before all dial
	c.initScheme()

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.rg = iresolver.NewResolverGroup(uuid.New().String())
	c.rg.SetAddrs(config.Endpoints)
	c.cc, err = c.dialWithBalancer()
	if err != nil {
		return nil, err
	}

	c.rpcClient = newRpcClient(c.cc)

	// register cluster to run background heath check periodically
	lc := newLeaderChecker(config, c.getDailOpts())
	picker.RegisterCluster(config.Endpoints, lc.checkLeader)
	return c, nil
}

func (c *clientImpl) initScheme() {
	u, err := url.Parse(c.config.Endpoints[0])
	if err != nil {
		klog.V(c.config.ErrLogLevel).ErrorS(err, "invalid url")
		return
	}
	c.scheme = u.Scheme
}

func (c *clientImpl) Close() error {
	c.rg.Close()
	_ = c.cc.Close()
	picker.UnregisterCluster(c.config.Endpoints)
	return nil
}

func (c *clientImpl) dialWithBalancer() (*grpc.ClientConn, error) {
	target := c.rg.Target()

	// grpc msg size dial option
	grpcMsgSizeOpts := []grpc.CallOption{
		defaultMaxCallSendMsgSize,
		defaultMaxCallRecvMsgSize,
	}
	if c.config.MaxCallSendMsgSize > 0 {
		grpcMsgSizeOpts[0] = grpc.MaxCallSendMsgSize(c.config.MaxCallSendMsgSize)
	}
	if c.config.MaxCallRecvMsgSize > 0 {
		grpcMsgSizeOpts[1] = grpc.MaxCallRecvMsgSize(c.config.MaxCallRecvMsgSize)
	}
	grpcMsgSizeDialOption := grpc.WithDefaultCallOptions(grpcMsgSizeOpts...)

	return c.dial(target, grpc.WithBalancerName(Brain), grpcMsgSizeDialOption)
}

func (c *clientImpl) dial(target string, dops ...grpc.DialOption) (*grpc.ClientConn, error) {
	dops = append(dops, c.getDailOpts()...)
	conn, err := grpc.DialContext(c.ctx, target, dops...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *clientImpl) getDailOpts() []grpc.DialOption {
	switch c.scheme {
	case "https", "unixs":
		return []grpc.DialOption{
			grpc.WithTransportCredentials(c.creds),
			grpc.WithContextDialer(c.dialer),
		}
	}

	return []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithContextDialer(c.dialer),
	}
}

func (c *clientImpl) dialer(ctx context.Context, dialEp string) (net.Conn, error) {
	klog.V(c.config.LogLevel).InfoS("dial", "endpoint", dialEp)
	conn, err := iresolver.Dialer(ctx, dialEp)
	if c.creds != nil {
		c.creds.updateAddrToEndpoint(dialEp, conn)
	}
	return conn, err
}
