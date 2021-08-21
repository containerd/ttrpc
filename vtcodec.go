/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package ttrpc

import (
	"fmt"
	"google.golang.org/grpc/encoding"

	gproto "github.com/golang/protobuf/proto"
	vtproto "github.com/planetscale/vtprotobuf/codec/grpc"
)

type vtcodec struct {
	vt *vtproto.Codec
}
var _ encoding.Codec = &vtcodec{}

type vtprotoMessage interface {
	MarshalVT() ([]byte, error)
	UnmarshalVT([]byte) error
}

func (vtcodec) Marshal(in interface{}) ([]byte, error) {
	vt, ok := in.(vtprotoMessage)
	if ok {
		return vt.MarshalVT()
	}
	g, ok := in.(gproto.Message)
	if ok {
		return gproto.Marshal(g)
	}
	return nil, fmt.Errorf("failed to marshal %T", in)
}

func (vtcodec) Unmarshal(out []byte, in interface{}) error {
	vt, ok := in.(vtprotoMessage)
	if ok {
		return vt.UnmarshalVT(out)
	}

	g, ok := in.(gproto.Message)
	if ok {
		return gproto.Unmarshal(out, g)
	}
	return fmt.Errorf("failed to unmarshal %T", in)
}

func (vtcodec) Name() string {
	return "proto"
}