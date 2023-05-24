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

package example

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"k8s.io/klog/v2"

	"github.com/kubewharf/kubebrain-client/api/v2rpc"
	"github.com/kubewharf/kubebrain-client/client"
)

func getTestCert() *tls.Config {
	dir := "./testdata"
	conf, _ := getTLSConfig(path.Join(dir, "client.crt"),
		path.Join(dir, "client.key"),
		path.Join(dir, "ca.crt"))
	return conf
}

func getTLSConfig(certFile, keyFile, ca string) (tlsConfig *tls.Config, err error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		klog.ErrorS(err, "can not load key pair", "cert", certFile, "key", keyFile)
		return nil, errors.Wrapf(err, "can not load key pair")
	}

	tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// load ca file
	certPool := x509.NewCertPool()
	caFileBytes, err := ioutil.ReadFile(ca)
	if err != nil {
		klog.ErrorS(err, "can not load ca cert", "ca", ca)
		return nil, errors.Wrapf(err, "can not load ca cert")
	}
	certPool.AppendCertsFromPEM(caFileBytes)
	tlsConfig.RootCAs = certPool

	klog.InfoS("init tls config success")
	return tlsConfig, nil
}

const host = "127.0.0.1"

func getEndpointsOnSingleHost(scheme, host string, ports ...int64) []string {
	ret := make([]string, len(ports))
	for i, port := range ports {
		ret[i] = fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}
	return ret
}

func TestExample(t *testing.T) {

	testConfigs := []struct {
		name   string
		config client.Config
	}{
		{
			name: "insecure access",
			config: client.Config{
				Endpoints: getEndpointsOnSingleHost("http", host, 3379, 4379, 5369),
				LogLevel:  0,
			},
		},
		{
			name: "secure access",
			config: client.Config{
				Endpoints: getEndpointsOnSingleHost("https", host, 3379, 4379),
				LogLevel:  0,
				TLS:       getTestCert(),
			},
		},
	}

	for _, config := range testConfigs {
		t.Run(config.name, func(t *testing.T) {
			testExample(t, config.config)
		})
	}

}

func testExample(t *testing.T, conf client.Config) {
	//t.Skip()

	ast := assert.New(t)
	prefix := fmt.Sprintf("/test/%d", time.Now().UnixNano())

	c, err := client.NewClient(conf)
	ast.NoError(err)
	ctx := context.Background()

	prevRev := uint64(0)

	t.Run("health", func(t *testing.T) {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = conf.TLS
		url := conf.Endpoints[0] + "/health"
		t.Logf("testing url %s", url)
		resp, err := http.Get(url)
		ast.NoError(err)
		bs, err := ioutil.ReadAll(resp.Body)
		ast.NoError(err)
		ast.Equal(`{"health":"true"}`, string(bs))
		resp.Body.Close()
	})

	t.Run("create", func(t *testing.T) {
		key, val := prefix, prefix
		resp, err := c.Create(ctx, key, val)
		ast.NoError(err)
		ast.True(resp.Succeeded)
		prevRev = resp.Header.Revision

		getResp, err := c.Get(ctx, key)
		ast.NoError(err)
		ast.Equal([]byte(val), getResp.Kv.Value)

		getResp, err = c.Get(ctx, key, client.WithRevision(getResp.Kv.Revision-1))
		ast.NoError(err)
		ast.Nil(getResp.Kv)
	})

	t.Run("watch", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)
		num := 10
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			wch := c.Watch(ctx, prefix, client.WithPrefix(), client.WithRevision(prevRev+1), client.WithBookmark())
			counter := 0
			for wresp := range wch {
				ast.NoError(wresp.Err())
				for _, event := range wresp.Events {
					ast.True(bytes.HasPrefix(event.Kv.Key, []byte(prefix)))
				}
				counter++
				if counter == num {
					cancel()
					return
				}
			}
		}()

		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("%s/%d", prefix, i)
			resp, err := c.Create(ctx, key, key)
			ast.NoError(err)
			ast.True(resp.Succeeded)
		}

		wg.Wait()
	})

	t.Run("range", func(t *testing.T) {
		resp, err := c.Range(ctx, prefix, client.PrefixEnd(prefix))
		ast.NoError(err)
		ast.Equal(11, len(resp.Kvs))
	})

	t.Run("range stream", func(t *testing.T) {
		rch := c.RangeStream(ctx, prefix, client.PrefixEnd(prefix))
		num := 11
		got := make(map[string]uint64)
		for rresp := range rch {
			ast.NoError(rresp.Err())
			for _, kv := range rresp.Kvs {
				t.Log(string(kv.Key))
				ast.Equal(kv.Key, kv.Value)
				got[string(kv.Key)] = kv.Revision
			}
		}

		ast.Equal(num, len(got))
		for k, r := range got {
			dresp, err := c.Delete(ctx, k, client.WithRevision(r))
			ast.NoError(err)
			ast.True(dresp.Succeeded)
		}

		rch = c.RangeStream(ctx, prefix, client.PrefixEnd(prefix))
		counter := 0
		more := true
		for resp := range rch {
			for _, kv := range resp.Kvs {
				t.Log(string(kv.Key))
			}
			counter++
			more = resp.More
			ast.NoError(resp.Err())
		}
		ast.Equal(1, counter)
		ast.False(more)
	})
}

func TestWatchBookmark(t *testing.T) {
	conf := client.Config{
		Endpoints: getEndpointsOnSingleHost("http", host, 3379),
		LogLevel:  1,
	}

	c, err := client.NewClient(conf)
	if err != nil {
		t.Errorf("failed to build client err: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	getResp, err := c.Get(ctx, "/test")
	if err != nil {
		t.Errorf("failed to get err: %v", err)
		return
	}

	rev := getResp.Kv.GetRevision()
	rev = 0

	watcher := c.Watch(ctx, "/", client.WithRevision(rev), client.WithBookmark())
	for watchResp := range watcher {
		if watchResp.Err() != nil {
			t.Errorf("watcher closed: %v", err)
			return
		}

		if watchResp.IsBookmark() {
			klog.Infof("BOOKMARK rev:%d", watchResp.Header.GetRevision())
			continue
		}

		for _, event := range watchResp.Events {
			switch event.Type {
			case v2rpc.Event_PUT:
				klog.Infof("PUT rev:%d key:%s", event.GetRevision(), string(event.GetKv().GetKey()))
			case v2rpc.Event_DELETE:
				klog.Infof("DELETE rev:%d key:%s", event.GetRevision(), string(event.GetKv().GetKey()))
			}
		}

	}

}
