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
	"io"

	"github.com/pkg/errors"

	rpc "github.com/kubewharf/kubebrain-client/api/v2rpc"
)

func (c *clientImpl) Watch(ctx context.Context, key string, opts ...WatchOption) WatchChan {
	req := &WatchRequest{
		Key: []byte(key),
	}

	for _, opt := range opts {
		opt.decorateWatchReq(req)
	}

	ch := make(chan WatchResponse, 1)
	streamClient, err := c.rpcClient.Watch(ctx, (*rpc.WatchRequest)(req))
	if err != nil {
		resp := WatchResponse{streamErr: newStreamErrWithRPCErr(err)}
		ch <- resp
		close(ch)
		return ch
	}

	go fetchWatchResp(ch, streamClient)
	return ch
}

func (c *clientImpl) RangeStream(ctx context.Context, key string, end string, opts ...RangeStreamOption) RangeStreamChan {
	req := &RangeStreamRequest{RangeRequest: &rpc.RangeRequest{}}
	req.Key = []byte(key)
	req.End = []byte(end)
	req.maxConcurrent = 1
	for _, opt := range opts {
		opt.decorateStreamRangeReq(req)
	}

	if req.maxConcurrent == 1 {
		return c.singleRangeStream(ctx, req.RangeRequest)
	} else {
		return c.concurrentRangeStream(ctx, req)
	}
}

func (c *clientImpl) singleRangeStream(ctx context.Context, req *rpc.RangeRequest) RangeStreamChan {
	listReq := &rpc.ListPartitionRequest{
		Key: req.Key,
		End: req.End,
	}

	resp, err := c.rpcClient.ListPartition(ctx, listReq)
	if err != nil {
		ch := make(chan StreamRangeResponse, 1)
		ch <- StreamRangeResponse{streamErr: newStreamErrWithRPCErr(err)}
		close(ch)
		return ch
	} else if len(resp.PartitionKeys) <= 1 {
		ch := make(chan StreamRangeResponse, 1)
		close(ch)
		return ch
	}

	req.Key = resp.PartitionKeys[0]
	req.End = resp.PartitionKeys[len(resp.PartitionKeys)-1]
	if req.Revision == 0 {
		req.Revision = resp.Header.Revision
	}
	return c.rangeStream(ctx, req)
}

func (c *clientImpl) rangeStream(ctx context.Context, req *rpc.RangeRequest) RangeStreamChan {
	ch := make(chan StreamRangeResponse, 1)
	streamClient, err := c.rpcClient.RangeStream(ctx, req)
	if err != nil {
		resp := StreamRangeResponse{streamErr: newStreamErrWithRPCErr(err)}
		ch <- resp
		close(ch)
		return ch
	}

	go fetchStreamRangeResp(ch, streamClient)
	return ch
}

func (c *clientImpl) concurrentRangeStream(ctx context.Context, req *RangeStreamRequest) RangeStreamChan {
	listReq := &rpc.ListPartitionRequest{
		Key: req.Key,
		End: req.End,
	}

	ch := make(chan StreamRangeResponse, 1)
	resp, err := c.rpcClient.ListPartition(ctx, listReq)
	if err != nil {
		ch <- StreamRangeResponse{streamErr: newStreamErrWithRPCErr(err)}
		close(ch)
		return ch
	}

	g, ctx := NewConcurrentLimitGroup(ctx, int(req.maxConcurrent))

	pNum := len(resp.PartitionKeys) - 1
	for i := 0; i < pNum; i++ {
		partitionReq := *req.RangeRequest
		if partitionReq.Revision == 0 {
			partitionReq.Revision = resp.Header.Revision
		}
		partitionReq.Key = resp.PartitionKeys[i]
		partitionReq.End = resp.PartitionKeys[i+1]
		cb := c.rangeStreamCallBack(ctx, &partitionReq, ch)
		g.Go(cb)
	}

	go func() {
		g.Wait()
		close(ch)
	}()

	return ch
}

func (c *clientImpl) rangeStreamCallBack(ctx context.Context, req *rpc.RangeRequest, output chan StreamRangeResponse) func() error {
	return func() error {
		input := c.rangeStream(ctx, req)
		for resp := range input {
			select {
			case <-ctx.Done():
				return errors.Wrap(context.Canceled, "cancel range stream")
			default:
			}

			output <- resp
			if err := resp.Err(); err != nil {
				return err
			}
		}
		return nil
	}
}

func fetchStreamRangeResp(output chan<- StreamRangeResponse, client rpc.Read_RangeStreamClient) {
	for {
		pbResp, err := client.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			resp := StreamRangeResponse{streamErr: newStreamErrWithRPCErr(err)}
			output <- resp
			break
		}
		resp := StreamRangeResponse{
			RangeResponse: (*RangeResponse)(pbResp.RangeResponse),
			streamErr:     newStreamErrWithReason(pbResp.Err),
		}
		output <- resp
	}
	close(output)
	return
}

func fetchWatchResp(output chan<- WatchResponse, client rpc.Watch_WatchClient) {
	for {
		pbResp, err := client.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			resp := WatchResponse{streamErr: newStreamErrWithRPCErr(err)}
			output <- resp
			break
		}
		resp := WatchResponse{
			Header:    pbResp.Header,
			Events:    pbResp.Events,
			streamErr: newStreamErrWithReason(pbResp.Err),
		}
		output <- resp
	}
	close(output)
	return
}

func newStreamErrWithReason(reason string) streamErr {
	return streamErr{
		closeErr:     nil,
		cancelReason: reason,
	}
}

func newStreamErrWithRPCErr(err error) streamErr {
	return streamErr{
		closeErr:     err,
		cancelReason: "",
	}
}
