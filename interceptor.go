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

import "context"

type UnaryServerInfo struct {
	FullMethod string
}

type UnaryClientInfo struct {
	FullMethod string
}

type Unmarshaler func(interface{}) error

type Invoker func(context.Context, *Request, *Response) error

type UnaryServerInterceptor func(context.Context, Unmarshaler, *UnaryServerInfo, Method) (interface{}, error)

type UnaryClientInterceptor func(context.Context, *Request, *Response, *UnaryClientInfo, Invoker) error

func defaultServerInterceptor(ctx context.Context, unmarshal Unmarshaler, info *UnaryServerInfo, method Method) (interface{}, error) {
	return method(ctx, unmarshal)
}

func defaultClientInterceptor(ctx context.Context, req *Request, resp *Response, _ *UnaryClientInfo, invoker Invoker) error {
	return invoker(ctx, req, resp)
}
