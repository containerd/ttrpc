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
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/ttrpc/internal"
)

func TestUnaryClientInterceptor(t *testing.T) {
	var (
		intercepted = false
		interceptor = func(ctx context.Context, req *Request, reply *Response, ci *UnaryClientInfo, i Invoker) error {
			intercepted = true
			return i(ctx, req, reply)
		}

		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr, WithUnaryClientInterceptor(interceptor))
		message         = strings.Repeat("a", 16)
		reply           = strings.Repeat(message, 2)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	request := &internal.TestPayload{
		Foo: message,
	}
	response := &internal.TestPayload{}

	if err := client.Call(ctx, serviceName, "Test", request, response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !intercepted {
		t.Fatalf("ttrpc client call not intercepted")
	}

	if response.Foo != reply {
		t.Fatalf("unexpected test service reply: %q != %q", response.Foo, reply)
	}
}

func TestChainUnaryClientInterceptor(t *testing.T) {
	var (
		orderIdx  = 0
		recorded  = []string{}
		intercept = func(idx int, tag string) UnaryClientInterceptor {
			return func(ctx context.Context, req *Request, reply *Response, ci *UnaryClientInfo, i Invoker) error {
				if idx != orderIdx {
					t.Fatalf("unexpected interceptor invocation order (%d != %d)", orderIdx, idx)
				}
				recorded = append(recorded, tag)
				orderIdx++
				return i(ctx, req, reply)
			}
		}

		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr,
			WithChainUnaryClientInterceptor(),
			WithChainUnaryClientInterceptor(
				intercept(0, "seen it"),
				intercept(1, "been"),
				intercept(2, "there"),
				intercept(3, "done"),
				intercept(4, "that"),
			),
		)
		expected = []string{
			"seen it",
			"been",
			"there",
			"done",
			"that",
		}
		message = strings.Repeat("a", 16)
		reply   = strings.Repeat(message, 2)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	request := &internal.TestPayload{
		Foo: message,
	}
	response := &internal.TestPayload{}
	if err := client.Call(ctx, serviceName, "Test", request, response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(recorded, expected) {
		t.Fatalf("unexpected ttrpc chained client unary interceptor order (%s != %s)",
			strings.Join(recorded, " "), strings.Join(expected, " "))
	}

	if response.Foo != reply {
		t.Fatalf("unexpected test service reply: %q != %q", response.Foo, reply)
	}
}

func TestUnaryServerInterceptor(t *testing.T) {
	var (
		intercepted = false
		interceptor = func(ctx context.Context, unmarshal Unmarshaler, _ *UnaryServerInfo, method Method) (interface{}, error) {
			intercepted = true
			return method(ctx, unmarshal)
		}

		ctx             = context.Background()
		server          = mustServer(t)(NewServer(WithUnaryServerInterceptor(interceptor)))
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		message         = strings.Repeat("a", 16)
		reply           = strings.Repeat(message, 2)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	request := &internal.TestPayload{
		Foo: message,
	}
	response := &internal.TestPayload{}
	if err := client.Call(ctx, serviceName, "Test", request, response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !intercepted {
		t.Fatalf("ttrpc server call not intercepted")
	}

	if response.Foo != reply {
		t.Fatalf("unexpected test service reply: %q != %q", response.Foo, reply)
	}
}

func TestChainUnaryServerInterceptor(t *testing.T) {
	var (
		orderIdx  = 0
		recorded  = []string{}
		intercept = func(idx int, tag string) UnaryServerInterceptor {
			return func(ctx context.Context, unmarshal Unmarshaler, _ *UnaryServerInfo, method Method) (interface{}, error) {
				if orderIdx != idx {
					t.Fatalf("unexpected interceptor invocation order (%d != %d)", orderIdx, idx)
				}
				recorded = append(recorded, tag)
				orderIdx++
				return method(ctx, unmarshal)
			}
		}

		ctx    = context.Background()
		server = mustServer(t)(NewServer(
			WithUnaryServerInterceptor(
				intercept(0, "seen it"),
			),
			WithChainUnaryServerInterceptor(
				intercept(1, "been"),
				intercept(2, "there"),
				intercept(3, "done"),
				intercept(4, "that"),
			),
		))
		expected = []string{
			"seen it",
			"been",
			"there",
			"done",
			"that",
		}
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		message         = strings.Repeat("a", 16)
		reply           = strings.Repeat(message, 2)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	request := &internal.TestPayload{
		Foo: message,
	}
	response := &internal.TestPayload{}

	if err := client.Call(ctx, serviceName, "Test", request, response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(recorded, expected) {
		t.Fatalf("unexpected ttrpc chained server unary interceptor order (%s != %s)",
			strings.Join(recorded, " "), strings.Join(expected, " "))
	}

	if response.Foo != reply {
		t.Fatalf("unexpected test service reply: %q != %q", response.Foo, reply)
	}
}
