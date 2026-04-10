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
	"os"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"k8s.io/klog/v2"

	"github.com/kubewharf/kubebrain-client/client/balancer/picker"
)

const Brain = "brain"

func init() {
	logLevel := 8
	errLogLevel := 4
	if os.Getenv("DEBUG") != "" {
		logLevel = 0
		errLogLevel = 0
	}
	balancer.Register(base.NewBalancerBuilder(Brain, picker.NewBuilder(klog.Level(logLevel), klog.Level(errLogLevel))))
}
