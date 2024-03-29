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
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/resolver"
	"k8s.io/klog/v2"
)

type builder struct {
	logLevel klog.Level
}

func NewBuilder(logLevel klog.Level) base.PickerBuilder {
	return &builder{
		logLevel: logLevel,
	}
}

func (b *builder) Build(readySCs map[resolver.Address]balancer.SubConn) balancer.Picker {
	return newRWSeparatedRoundRobinBalanced(config{
		readySCs: readySCs,
		logLevel: b.logLevel,
	})
}
