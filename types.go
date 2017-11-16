package ttrpc

import (
	"fmt"

	"github.com/containerd/containerd/protobuf/google/rpc"
	"github.com/gogo/protobuf/types"
)

type Request struct {
	Service string     `protobuf:"bytes,1,opt,name=service,proto3"`
	Method  string     `protobuf:"bytes,2,opt,name=method,proto3"`
	Payload *types.Any `protobuf:"bytes,3,opt,name=payload,proto3"`
}

func (r *Request) Reset()         { *r = Request{} }
func (r *Request) String() string { return fmt.Sprintf("%+#v", r) }
func (r *Request) ProtoMessage()  {}

type Response struct {
	Status  *rpc.Status `protobuf:"bytes,1,opt,name=status,proto3"`
	Payload *types.Any  `protobuf:"bytes,2,opt,name=payload,proto3"`
}

func (r *Response) Reset()         { *r = Response{} }
func (r *Response) String() string { return fmt.Sprintf("%+#v", r) }
func (r *Response) ProtoMessage()  {}
