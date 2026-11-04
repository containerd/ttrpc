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
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/containerd/ttrpc/internal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testTimeout bounds every call, so that a server which fails to answer
// fails the test instead of hanging it.
const testTimeout = 5 * time.Second

// registerMethod registers a single "Test" method implemented by fn.
func registerMethod(srv *Server, fn func(context.Context) (any, error)) {
	srv.Register(serviceName, map[string]Method{
		"Test": func(ctx context.Context, unmarshal func(any) error) (any, error) {
			var req internal.TestPayload
			if err := unmarshal(&req); err != nil {
				return nil, err
			}
			return fn(ctx)
		},
	})
}

func TestCloseConnectionErrorPreservesCodeAndMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	registerMethod(server, func(context.Context) (any, error) {
		return nil, CloseConnectionError(status.Errorf(codes.PermissionDenied, "plugin already registered"))
	})

	go server.Serve(ctx, listener)

	client, cleanup := newTestClient(t, addr)
	defer cleanup()

	var tp internal.TestPayload
	err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp)

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected a grpc status error, got %v (%T)", err, err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Errorf("expected code %v, got %v", codes.PermissionDenied, st.Code())
	}
	if st.Message() != "plugin already registered" {
		t.Errorf("expected message %q, got %q", "plugin already registered", st.Message())
	}
}

func TestCloseConnectionErrorServesNoFurtherRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	var calls atomic.Int32
	registerMethod(server, func(context.Context) (any, error) {
		calls.Add(1)
		return nil, CloseConnectionError(status.Errorf(codes.PermissionDenied, "go away"))
	})

	go server.Serve(ctx, listener)

	client, cleanup := newTestClient(t, addr)
	defer cleanup()

	var tp internal.TestPayload
	if err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp); err == nil {
		t.Fatal("expected an error from the first call, got nil")
	}

	if err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp); err == nil {
		t.Fatal("expected an error from the second call, got nil")
	}

	// The calls are sequential, so the close is already requested when the
	// second request is sent. Requests already read off the wire are
	// dispatched concurrently and can still reach the handler.
	if n := calls.Load(); n != 1 {
		t.Errorf("expected the handler to run once, ran %d times", n)
	}

	if err := client.UserOnCloseWait(ctx); err != nil {
		t.Fatalf("connection was not closed after the error was delivered: %v", err)
	}
}

func TestCloseConnectionErrorLeavesOtherConnectionsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	registerMethod(server, func(ctx context.Context) (any, error) {
		if v, ok := GetMetadataValue(ctx, "reject"); ok && v == "yes" {
			return nil, CloseConnectionError(status.Errorf(codes.PermissionDenied, "go away"))
		}
		return &internal.TestPayload{Foo: "served"}, nil
	})

	go server.Serve(ctx, listener)

	rejected, cleanupRejected := newTestClient(t, addr)
	defer cleanupRejected()
	kept, cleanupKept := newTestClient(t, addr)
	defer cleanupKept()

	var tp internal.TestPayload
	rejectCtx := WithMetadata(ctx, MD{"reject": []string{"yes"}})
	if err := rejected.Call(rejectCtx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp); err == nil {
		t.Fatal("expected an error on the rejected connection, got nil")
	}

	if err := kept.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp); err != nil {
		t.Fatalf("the other connection stopped working: %v", err)
	}
	if tp.Foo != "served" {
		t.Errorf("expected payload %q on the other connection, got %q", "served", tp.Foo)
	}
}

func TestCloseConnectionErrorFromStreamHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	server.RegisterService(serviceName, &ServiceDesc{
		Streams: map[string]Stream{
			"Stream": {
				Handler: func(context.Context, StreamServer) (any, error) {
					return nil, CloseConnectionError(status.Errorf(codes.PermissionDenied, "go away"))
				},
				StreamingClient: true,
				StreamingServer: true,
			},
		},
	})

	go server.Serve(ctx, listener)

	client, cleanup := newTestClient(t, addr)
	defer cleanup()

	stream, err := client.NewStream(ctx, &StreamDesc{StreamingClient: true, StreamingServer: true}, serviceName, "Stream", nil)
	if err != nil {
		t.Fatal(err)
	}

	var tp internal.TestPayload
	st, ok := status.FromError(stream.RecvMsg(&tp))
	if !ok {
		t.Fatalf("expected a grpc status error from the stream, got %v", err)
	}
	if st.Code() != codes.PermissionDenied || st.Message() != "go away" {
		t.Errorf("expected PermissionDenied/%q, got %v/%q", "go away", st.Code(), st.Message())
	}

	if err := client.UserOnCloseWait(ctx); err != nil {
		t.Fatalf("connection was not closed after the error was delivered: %v", err)
	}
}

func TestCloseConnectionErrorWithPlainError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	registerMethod(server, func(context.Context) (any, error) {
		return nil, CloseConnectionError(errors.New("plugin already registered"))
	})

	go server.Serve(ctx, listener)

	client, cleanup := newTestClient(t, addr)
	defer cleanup()

	var tp internal.TestPayload
	err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp)

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected a grpc status error, got %v (%T)", err, err)
	}
	if st.Code() != codes.Unknown {
		t.Errorf("expected code %v, got %v", codes.Unknown, st.Code())
	}
	if st.Message() != "plugin already registered" {
		t.Errorf("expected message %q, got %q", "plugin already registered", st.Message())
	}

	if err := client.UserOnCloseWait(ctx); err != nil {
		t.Fatalf("connection was not closed after the error was delivered: %v", err)
	}
}

func TestOrdinaryHandlerErrorKeepsTheConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()
	defer server.Close()

	registerMethod(server, func(context.Context) (any, error) {
		return nil, status.Errorf(codes.InvalidArgument, "bad request")
	})

	go server.Serve(ctx, listener)

	client, cleanup := newTestClient(t, addr)
	defer cleanup()

	var tp internal.TestPayload
	for i := range 3 {
		err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{Foo: "bar"}, &tp)
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("call %d: expected a grpc status error, got %v (%T)", i, err, err)
		}
		if st.Code() != codes.InvalidArgument {
			t.Fatalf("call %d: expected code %v, got %v", i, codes.InvalidArgument, st.Code())
		}
	}
}

func TestCloseConnectionErrorOfNilIsNil(t *testing.T) {
	if err := CloseConnectionError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// A close request must always carry an error to encode a response from. A
// wrapper around a nil error encodes as a response with no status, which
// reaches the client as a successful empty call.
//
// A nil pointer in a non-nil interface still defeats this, but that applies
// to any error returned from any handler.
func TestCloseConnectionErrorNeverWrapsNil(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"plain", errors.New("denied")},
		{"status", status.Errorf(codes.PermissionDenied, "denied")},
		{"sentinel", context.Canceled},
		{"wrapped twice", CloseConnectionError(errors.New("denied"))},
		{"nested", fmt.Errorf("outer: %w", CloseConnectionError(errors.New("denied")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CloseConnectionError(tc.err)
			if err == nil {
				if tc.err != nil {
					t.Fatalf("a non-nil error was dropped")
				}
				return
			}

			closeConn, unwrapped := unwrapCloseConn(err)
			if !closeConn {
				t.Fatal("wrapped error did not ask to close the connection")
			}
			if unwrapped == nil {
				t.Fatal("the error to encode a response from is nil")
			}
		})
	}
}

// Wrapping twice, as a handler and an interceptor might each do, must not
// bury the error deeper than the server unwraps it.
func TestCloseConnectionErrorIsIdempotent(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code codes.Code
		msg  string
	}{
		{
			name: "status error",
			err:  status.Errorf(codes.PermissionDenied, "go away"),
			code: codes.PermissionDenied,
			msg:  "go away",
		},
		{
			// convertCode compares errors for equality, so an extra
			// wrapper would cost this its own code.
			name: "sentinel error",
			err:  context.Canceled,
			code: codes.Canceled,
			msg:  context.Canceled.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			once := CloseConnectionError(tc.err)
			twice := CloseConnectionError(once)

			closeConn, unwrapped := unwrapCloseConn(twice)
			if !closeConn {
				t.Fatal("double wrapped error did not ask to close the connection")
			}

			st, ok := status.FromError(unwrapped)
			if !ok {
				st = status.New(convertCode(unwrapped), unwrapped.Error())
			}
			if st.Code() != tc.code {
				t.Errorf("expected code %v, got %v", tc.code, st.Code())
			}
			if st.Message() != tc.msg {
				t.Errorf("expected message %q, got %q", tc.msg, st.Message())
			}
		})
	}
}

// A marker nested inside another error must keep the enclosing error's
// message, rather than reporting only the error the marker wraps.
func TestCloseConnectionErrorNestedKeepsOuterMessage(t *testing.T) {
	err := fmt.Errorf("authentication failed: %w", CloseConnectionError(errors.New("denied")))

	closeConn, unwrapped := unwrapCloseConn(err)
	if !closeConn {
		t.Fatal("nested marker did not ask to close the connection")
	}

	st, ok := status.FromError(unwrapped)
	if !ok {
		st = status.New(convertCode(unwrapped), unwrapped.Error())
	}
	if want := "authentication failed: denied"; st.Message() != want {
		t.Errorf("expected message %q, got %q", want, st.Message())
	}
}
