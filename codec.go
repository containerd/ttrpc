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
	"github.com/pkg/errors"

	vtproto "github.com/planetscale/vtprotobuf/codec/grpc"
	gproto "github.com/golang/protobuf/proto"
	"google.golang.org/grpc/encoding"
)

type codec struct{}

var proto encoding.Codec

func init() {
	proto = &vtcodec{vt:&vtproto.Codec{}}
}

func (c codec) Marshal(msg interface{}) ([]byte, error) {
	switch v := msg.(type) {
	case gproto.Message:
		return proto.Marshal(v)
	default:
		return nil, errors.Errorf("ttrpc: cannot marshal unknown type: %T", msg)
	}
}

func (c codec) Unmarshal(p []byte, msg interface{}) error {
	switch v := msg.(type) {
	case gproto.Message:
		return proto.Unmarshal(p, v)
	default:
		return errors.Errorf("ttrpc: cannot unmarshal into unknown type: %T", msg)
	}
}
