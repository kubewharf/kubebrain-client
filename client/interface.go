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
	"fmt"

	rpc "github.com/kubewharf/kubebrain-client/api/v2rpc"
)

type (
	CreateRequest        rpc.CreateRequest
	UpdateRequest        rpc.UpdateRequest
	DeleteRequest        rpc.DeleteRequest
	CompactRequest       rpc.CompactRequest
	GetRequest           rpc.GetRequest
	CountRequest         rpc.CountRequest
	ListPartitionRequest rpc.ListPartitionRequest
	RangeRequest         rpc.RangeRequest
	WatchRequest         rpc.WatchRequest

	CreateResponse        rpc.CreateResponse
	UpdateResponse        rpc.UpdateResponse
	DeleteResponse        rpc.DeleteResponse
	CompactResponse       rpc.CompactResponse
	GetResponse           rpc.GetResponse
	RangeResponse         rpc.RangeResponse
	CountResponse         rpc.CountResponse
	ListPartitionResponse rpc.ListPartitionResponse
	Event                 rpc.Event

	RangeStreamChan <-chan StreamRangeResponse
	WatchChan       <-chan WatchResponse
)

type RangeStreamRequest struct {
	*rpc.RangeRequest
	maxConcurrent uint
}

type StreamRangeResponse struct {
	*RangeResponse
	streamErr
}

type WatchResponse struct {
	Header *rpc.ResponseHeader
	Events []*rpc.Event
	streamErr
}

type streamErr struct {
	// closeErr store the error return by stream.Recv
	closeErr error

	// cancelReason store the cancel msg return by server
	cancelReason string
}

func (s *streamErr) Err() error {
	switch {
	case s.closeErr != nil:
		return s.closeErr
	case s.cancelReason != "":
		return fmt.Errorf(s.cancelReason)
	}
	return nil
}

type Client interface {
	// Create stores a new kv
	Create(ctx context.Context, key, val string, opts ...CreateOption) (*CreateResponse, error)

	// Update updates value indexed by given key
	Update(ctx context.Context, key, val string, rev uint64, opts ...UpdateOption) (*UpdateResponse, error)

	// Get reads single kv
	Get(ctx context.Context, key string, opts ...GetOption) (*GetResponse, error)

	// Range reads multiple kvs indexed by keys in [start, end)
	Range(ctx context.Context, start string, end string, opts ...RangeOption) (*RangeResponse, error)

	// Delete removes data
	Delete(ctx context.Context, key string, opts ...DeleteOption) (*DeleteResponse, error)

	// Compact compacts data
	Compact(ctx context.Context, rev uint64, opts ...CompactOption) (*CompactResponse, error)

	// Watch traces event with prefix
	Watch(ctx context.Context, key string, opts ...WatchOption) WatchChan

	// RangeStream reads kvs indexed by keys in [start, end) through stream
	RangeStream(ctx context.Context, start string, end string, opts ...RangeStreamOption) RangeStreamChan

	// Count counts num of kvs in [start, end)
	Count(ctx context.Context, start string, end string, opts ...CountOption) (*CountResponse, error)

	// ListPartition return all partitions in [start, end)
	ListPartition(ctx context.Context, start string, end string, opts ...ListPartitionOption) (*ListPartitionResponse, error)

	// Close closes client
	Close() error
}

type CreateOption interface {
	decorateCreateReq(request *CreateRequest)
}

type UpdateOption interface {
	decorateUpdateReq(request *UpdateRequest)
}

type DeleteOption interface {
	decorateDeleteReq(request *DeleteRequest)
}

type GetOption interface {
	decorateGetReq(request *GetRequest)
}

type RangeOption interface {
	decorateRangeReq(request *RangeRequest)
}

type WatchOption interface {
	decorateWatchReq(request *WatchRequest)
}

type CompactOption interface {
	decorateCompactReq(request *CompactRequest)
}

type CountOption interface {
	decorateCountReq(request *CountRequest)
}

type RangeStreamOption interface {
	decorateStreamRangeReq(request *RangeStreamRequest)
}

type ListPartitionOption interface {
	decorateListPartitionReq(request *ListPartitionRequest)
}
