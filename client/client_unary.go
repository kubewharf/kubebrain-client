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

	rpc "github.com/kubewharf/kubebrain-client/api/v2rpc"
)

func (c *clientImpl) Create(ctx context.Context, key, val string, opts ...CreateOption) (*CreateResponse, error) {
	req := &CreateRequest{
		Key:   []byte(key),
		Value: []byte(val),
		Lease: 0,
	}

	for _, opt := range opts {
		opt.decorateCreateReq(req)
	}

	resp, err := c.rpcClient.Create(ctx, (*rpc.CreateRequest)(req))
	return (*CreateResponse)(resp), err
}

func (c *clientImpl) Update(ctx context.Context, key, val string, rev uint64, opts ...UpdateOption) (*UpdateResponse, error) {
	req := &UpdateRequest{
		Kv: &rpc.KeyValue{
			Key:      []byte(key),
			Revision: rev,
			Value:    []byte(val),
		},
	}

	for _, opt := range opts {
		opt.decorateUpdateReq(req)
	}

	resp, err := c.rpcClient.Update(ctx, (*rpc.UpdateRequest)(req))
	return (*UpdateResponse)(resp), err
}

func (c *clientImpl) Get(ctx context.Context, key string, opts ...GetOption) (*GetResponse, error) {
	req := &GetRequest{
		Key: []byte(key),
	}

	for _, opt := range opts {
		opt.decorateGetReq(req)
	}

	resp, err := c.rpcClient.Get(ctx, (*rpc.GetRequest)(req))
	return (*GetResponse)(resp), err
}

func (c *clientImpl) Range(ctx context.Context, start string, end string, opts ...RangeOption) (*RangeResponse, error) {
	req := &RangeRequest{
		Key: []byte(start),
		End: []byte(end),
	}

	for _, opt := range opts {
		opt.decorateRangeReq(req)
	}

	resp, err := c.rpcClient.Range(ctx, (*rpc.RangeRequest)(req))
	return (*RangeResponse)(resp), err
}

func (c *clientImpl) Delete(ctx context.Context, key string, opts ...DeleteOption) (*DeleteResponse, error) {
	req := &DeleteRequest{
		Key: []byte(key),
	}

	for _, opt := range opts {
		opt.decorateDeleteReq(req)
	}

	resp, err := c.rpcClient.Delete(ctx, (*rpc.DeleteRequest)(req))
	return (*DeleteResponse)(resp), err
}

func (c *clientImpl) Compact(ctx context.Context, rev uint64, opts ...CompactOption) (*CompactResponse, error) {
	req := &CompactRequest{
		Revision: rev,
	}

	for _, opt := range opts {
		opt.decorateCompactReq(req)
	}

	resp, err := c.rpcClient.Compact(ctx, (*rpc.CompactRequest)(req))
	return (*CompactResponse)(resp), err
}

func (c *clientImpl) Count(ctx context.Context, start string, end string, opts ...CountOption) (*CountResponse, error) {
	req := &CountRequest{}
	req.Key = []byte(start)
	req.End = []byte(end)

	for _, opt := range opts {
		opt.decorateCountReq(req)
	}

	resp, err := c.rpcClient.Count(ctx, (*rpc.CountRequest)(req))
	return (*CountResponse)(resp), err
}
