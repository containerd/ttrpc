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
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/ttrpc/internal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const serviceName = "testService"

// testingService is our prototype service definition for use in testing the full model.
//
// Typically, this is generated. We define it here to ensure that that package
// primitive has what is required for generated code.
type testingService interface {
	Test(ctx context.Context, req *internal.TestPayload) (*internal.TestPayload, error)
}

type testingClient struct {
	client *Client
}

func newTestingClient(client *Client) *testingClient {
	return &testingClient{
		client: client,
	}
}

func (tc *testingClient) Test(ctx context.Context, req *internal.TestPayload) (*internal.TestPayload, error) {
	var tp internal.TestPayload
	return &tp, tc.client.Call(ctx, serviceName, "Test", req, &tp)
}

// testingServer is what would be implemented by the user of this package.
type testingServer struct {
	echoOnce bool
}

func (s *testingServer) Test(ctx context.Context, req *internal.TestPayload) (*internal.TestPayload, error) {
	tp := &internal.TestPayload{}
	if s.echoOnce {
		tp.Foo = req.Foo
	} else {
		tp.Foo = strings.Repeat(req.Foo, 2)
	}
	if dl, ok := ctx.Deadline(); ok {
		tp.Deadline = dl.UnixNano()
	}

	if v, ok := GetMetadataValue(ctx, "foo"); ok {
		tp.Metadata = v
	}

	return tp, nil
}

// registerTestingService mocks more of what is generated code. Unlike grpc, we
// register with a closure so that the descriptor is allocated only on
// registration.
func registerTestingService(srv *Server, svc testingService) {
	srv.Register(serviceName, map[string]Method{
		"Test": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			var req internal.TestPayload
			if err := unmarshal(&req); err != nil {
				return nil, err
			}
			return svc.Test(ctx, &req)
		},
	})
}

func protoEqual(a, b proto.Message) (bool, error) {
	ma, err := proto.Marshal(a)
	if err != nil {
		return false, err
	}
	mb, err := proto.Marshal(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ma, mb), nil
}

func TestServer(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		tclient         = newTestingClient(client)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	testCases := []string{"bar", "baz"}
	results := make(chan callResult, len(testCases))
	for _, tc := range testCases {
		go func(expected string) {
			results <- roundTrip(ctx, tclient, expected)
		}(tc)
	}

	for i := 0; i < len(testCases); {
		result := <-results
		if result.err != nil {
			t.Fatalf("(%s): %v", result.name, result.err)
		}
		equal, err := protoEqual(result.received, result.expected)
		if err != nil {
			t.Fatalf("failed to compare %s and %s: %s", result.received, result.expected, err)
		}
		if !equal {
			t.Fatalf("unexpected response: %+#v != %+#v", result.received, result.expected)
		}
		i++
	}
}

func TestServerUnimplemented(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(ctx, listener)
	}()

	var tp internal.TestPayload
	if err := client.Call(ctx, "Not", "Found", &tp, &tp); err == nil {
		t.Fatalf("expected error from non-existent service call")
	} else if status, ok := status.FromError(err); !ok {
		t.Fatalf("expected status present in error: %v", err)
	} else if status.Code() != codes.Unimplemented {
		t.Fatalf("expected not found for method")
	}

	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != ErrServerClosed {
		t.Fatal(err)
	}
}

func TestServerListenerClosed(t *testing.T) {
	var (
		ctx         = context.Background()
		server      = mustServer(t)(NewServer())
		_, listener = newTestListener(t)
		errs        = make(chan error, 1)
	)

	go func() {
		errs <- server.Serve(ctx, listener)
	}()

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	err := <-errs
	if err == nil {
		t.Fatal(err)
	}
}

func TestServerShutdown(t *testing.T) {
	const ncalls = 5
	var (
		ctx              = context.Background()
		server           = mustServer(t)(NewServer())
		addr, listener   = newTestListener(t)
		shutdownStarted  = make(chan struct{})
		shutdownFinished = make(chan struct{})
		handlersStarted  sync.WaitGroup
		proceed          = make(chan struct{})
		serveErrs        = make(chan error, 1)
		callErrs         = make(chan error, ncalls)
		shutdownErrs     = make(chan error, 1)
		client, cleanup  = newTestClient(t, addr)
		_, cleanup2      = newTestClient(t, addr) // secondary connection
	)
	defer cleanup()
	defer cleanup2()

	// register a service that takes until we tell it to stop
	server.Register(serviceName, map[string]Method{
		"Test": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			var req internal.TestPayload
			if err := unmarshal(&req); err != nil {
				return nil, err
			}

			handlersStarted.Done()
			<-proceed
			return &internal.TestPayload{Foo: "waited"}, nil
		},
	})

	go func() {
		serveErrs <- server.Serve(ctx, listener)
	}()

	// send a series of requests that will get blocked
	for i := 0; i < ncalls; i++ {
		handlersStarted.Add(1)
		go func(i int) {
			tp := internal.TestPayload{Foo: "half" + fmt.Sprint(i)}
			callErrs <- client.Call(ctx, serviceName, "Test", &tp, &tp)
		}(i)
	}

	handlersStarted.Wait()
	go func() {
		close(shutdownStarted)
		shutdownErrs <- server.Shutdown(ctx)
		close(shutdownFinished)
	}()

	<-shutdownStarted
	close(proceed)
	<-shutdownFinished

	for i := 0; i < ncalls; i++ {
		if err := <-callErrs; err != nil && err != ErrClosed {
			t.Fatal(err)
		}
	}

	if err := <-shutdownErrs; err != nil {
		t.Fatal(err)
	}

	if err := <-serveErrs; err != ErrServerClosed {
		t.Fatal(err)
	}
	checkServerShutdown(t, server)
}

func TestServerClose(t *testing.T) {
	var (
		ctx         = context.Background()
		server      = mustServer(t)(NewServer())
		_, listener = newTestListener(t)
		startClose  = make(chan struct{})
		errs        = make(chan error, 1)
	)

	go func() {
		close(startClose)
		errs <- server.Serve(ctx, listener)
	}()

	<-startClose
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	err := <-errs
	if err != ErrServerClosed {
		t.Fatal("expected an error from a closed server", err)
	}

	checkServerShutdown(t, server)
}

func TestImmediateServerShutdown(t *testing.T) {
	for i := 0; i < 1024; i++ {
		var (
			ctx            = context.Background()
			server         = mustServer(t)(NewServer())
			addr, listener = newTestListener(t)
			errs           = make(chan error, 1)
			_, cleanup     = newTestClient(t, addr)
		)
		defer cleanup()
		defer listener.Close()
		go func() {
			errs <- server.Serve(ctx, listener)
		}()

		registerTestingService(server, &testingServer{})

		if err := server.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-errs:
			if err != ErrServerClosed {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("retreiving error from server.Shutdown() timed out")
		}
	}
}

func TestOversizeCall(t *testing.T) {
	type testCase struct {
		name        string
		echoOnce    bool
		clientLimit int
		serverLimit int
		requestSize int
		clientFail  bool
		sendFail    bool
		serverFail  bool
	}

	overhead := getWireMessageOverhead(t)

	clientOpts := func(tc *testCase) []ClientOpts {
		if tc.clientLimit == 0 {
			return nil
		}
		return []ClientOpts{WithClientWireMessageLimit(tc.clientLimit)}
	}
	serverOpts := func(tc *testCase) []ServerOpt {
		if tc.serverLimit == 0 {
			return nil
		}
		return []ServerOpt{WithServerWireMessageLimit(tc.serverLimit)}
	}

	runTest := func(t *testing.T, tc *testCase) {
		var (
			ctx             = context.Background()
			server          = mustServer(t)(NewServer(serverOpts(tc)...))
			addr, listener  = newTestListener(t)
			errs            = make(chan error, 1)
			client, cleanup = newTestClient(t, addr, clientOpts(tc)...)
		)
		defer cleanup()
		defer listener.Close()
		go func() {
			errs <- server.Serve(ctx, listener)
		}()

		registerTestingService(server, &testingServer{echoOnce: tc.echoOnce})

		req := &internal.TestPayload{
			Foo: strings.Repeat("a", tc.requestSize),
		}
		rsp := &internal.TestPayload{}

		err := client.Call(ctx, serviceName, "Test", req, rsp)
		if tc.clientFail {
			if err == nil {
				t.Fatalf("expected error from oversized message")
			} else if status, ok := status.FromError(err); !ok {
				t.Fatalf("expected status present in error: %v", err)
			} else if status.Code() != codes.ResourceExhausted {
				t.Fatalf("expected code: %v != %v", status.Code(), codes.ResourceExhausted)
			}
			if tc.sendFail {
				var msgLenErr *OversizedMessageErr
				if !errors.As(err, &msgLenErr) {
					t.Fatalf("failed to retrieve client send OversizedMessageErr")
				}
				rejLen, maxLen := msgLenErr.RejectedLength(), msgLenErr.MaximumLength()
				if rejLen == 0 {
					t.Fatalf("zero rejected length in client send oversized message error")
				}
				if maxLen == 0 {
					t.Fatalf("zero maximum length in client send oversized message error")
				}
				if rejLen <= maxLen {
					t.Fatalf("client send oversized message error rejected < max. length (%d < %d)",
						rejLen, maxLen)
				}
			}
		} else if tc.serverFail {
			if err == nil {
				t.Fatalf("expected error from server-side oversized message")
			} else {
				if status, ok := status.FromError(err); !ok {
					t.Fatalf("expected status present in error: %v", err)
				} else if status.Code() != codes.ResourceExhausted {
					t.Fatalf("expected code: %v != %v", status.Code(), codes.ResourceExhausted)
				}
				if msgLenErr, ok := OversizedMessageFromError(err); !ok {
					t.Fatalf("failed to retrieve oversized message error")
				} else {
					rejLen, maxLen := msgLenErr.RejectedLength(), msgLenErr.MaximumLength()
					if rejLen == 0 {
						t.Fatalf("zero rejected length in oversized message error")
					}
					if maxLen == 0 {
						t.Fatalf("zero maximum length in oversized message error")
					}
					if rejLen <= maxLen {
						t.Fatalf("oversized message error rejected < max. length (%d < %d)",
							rejLen, maxLen)
					}
				}
			}
		} else {
			if err != nil {
				t.Fatalf("expected success, got error %v", err)
			}
		}

		if err := server.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != ErrServerClosed {
			t.Fatal(err)
		}
	}

	for _, tc := range []*testCase{
		{
			name:        "default limits, fitting request and response",
			echoOnce:    true,
			clientLimit: 0,
			serverLimit: 0,
			requestSize: DefaultMessageLengthLimit - overhead,
		},
		{
			name:        "default limits, only recv side check",
			clientLimit: 0,
			serverLimit: 0,
			requestSize: DefaultMessageLengthLimit - overhead,
			serverFail:  true,
		},

		{
			name:        "default limits, oversized request",
			echoOnce:    true,
			clientLimit: 0,
			serverLimit: 0,
			requestSize: DefaultMessageLengthLimit,
			clientFail:  true,
		},
		{
			name:        "default limits, oversized response",
			clientLimit: 0,
			serverLimit: 0,
			requestSize: DefaultMessageLengthLimit / 2,
			serverFail:  true,
		},
		{
			name:        "8K limits, 4K request and response",
			echoOnce:    true,
			clientLimit: 8 * 1024,
			serverLimit: 8 * 1024,
			requestSize: 4 * 1024,
		},
		{
			name:        "4K limits, barely fitting cc. 4K request and response",
			echoOnce:    true,
			clientLimit: 4 * 1024,
			serverLimit: 4 * 1024,
			requestSize: 4*1024 - overhead,
		},
		{
			name:        "4K limits, oversized request on client side",
			echoOnce:    true,
			clientLimit: 4 * 1024,
			serverLimit: 4 * 1024,
			requestSize: 4 * 1024,
			clientFail:  true,
			sendFail:    true,
		},
		{
			name:        "4K limits, oversized request on server side",
			echoOnce:    true,
			clientLimit: 4*1024 + overhead,
			serverLimit: 4 * 1024,
			requestSize: 4 * 1024,
			serverFail:  true,
		},
		{
			name:        "4K limits, oversized response on client side",
			clientLimit: 4*1024 + overhead,
			serverLimit: 4 * 1024,
			requestSize: 8*1024 + overhead,
			clientFail:  true,
		},
		{
			name:        "4K limits, oversized response on server side",
			clientLimit: 4*1024 + overhead,
			serverLimit: 4 * 1024,
			requestSize: 4 * 1024,
			serverFail:  true,
		},
		{
			name:        "too small limits, adjusted to minimum accepted limit",
			echoOnce:    true,
			clientLimit: 4,
			serverLimit: 4,
			requestSize: 4*1024 - overhead,
		},
		{
			name:        "maximum allowed protocol limit",
			echoOnce:    true,
			clientLimit: MaxMessageLengthLimit,
			serverLimit: MaxMessageLengthLimit,
			requestSize: MaxMessageLengthLimit - overhead,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runTest(t, tc)
		})
	}
}

func getWireMessageOverhead(t *testing.T) int {
	emptyReq, err := codec{}.Marshal(&Request{
		Service: serviceName,
		Method:  "Test",
	})
	if err != nil {
		t.Fatalf("failed to marshal empty request: %v", err)
	}

	emptyRsp, err := codec{}.Marshal(&Response{
		Status: status.New(codes.OK, "").Proto(),
	})
	if err != nil {
		t.Fatalf("failed to marshal empty response: %v", err)
	}

	reqLen := len(emptyReq)
	rspLen := len(emptyRsp)
	if reqLen > rspLen {
		return reqLen + messageHeaderLength
	}

	return rspLen + messageHeaderLength
}

func TestClientEOF(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(ctx, listener)
	}()

	registerTestingService(server, &testingServer{})

	tp := &internal.TestPayload{}
	// do a regular call
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// shutdown the server so the client stops receiving stuff.
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != ErrServerClosed {
		t.Fatal(err)
	}

	client.UserOnCloseWait(ctx)

	// server shutdown, but we still make a call.
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err == nil {
		t.Fatalf("expected error when calling against shutdown server")
	} else if !errors.Is(err, ErrClosed) {
		var errno syscall.Errno
		if errors.As(err, &errno) {
			t.Logf("errno=%d", errno)
		}

		t.Fatalf("expected to have a cause of ErrClosed, got %v", err)
	}
}

func TestServerRequestTimeout(t *testing.T) {
	var (
		ctx, cancel     = context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		testImpl        = &testingServer{}
		client, cleanup = newTestClient(t, addr)
		result          internal.TestPayload
	)
	defer cancel()
	defer cleanup()
	defer listener.Close()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	if err := client.Call(ctx, serviceName, "Test", &internal.TestPayload{}, &result); err != nil {
		t.Fatalf("unexpected error making call: %v", err)
	}

	dl, _ := ctx.Deadline()
	if result.Deadline != dl.UnixNano() {
		t.Fatalf("expected deadline %v, actual: %v", dl, time.Unix(0, result.Deadline))
	}
}

func TestServerConnectionsLeak(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()

	connectionCountBefore := server.countConnection()

	go server.Serve(ctx, listener)

	registerTestingService(server, &testingServer{})

	tp := &internal.TestPayload{}
	// do a regular call
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err != nil {
		t.Fatalf("unexpected error during test call: %v", err)
	}

	connectionCount := server.countConnection()
	if connectionCount != 1 {
		t.Fatalf("unexpected connection count: %d, expected: %d", connectionCount, 1)
	}

	// close the client, so that server gets EOF
	if err := client.Close(); err != nil {
		t.Fatalf("unexpected error while closing client: %v", err)
	}

	// server should eventually close the client connection
	maxAttempts := 20
	for i := 1; i <= maxAttempts; i++ {
		connectionCountAfter := server.countConnection()
		if connectionCountAfter == connectionCountBefore {
			break
		}
		if i == maxAttempts {
			t.Fatalf("expected number of connections to be equal %d after client close, got %d connections",
				connectionCountBefore, connectionCountAfter)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	var (
		ctx             = context.Background()
		server          = mustServer(b)(NewServer())
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(b)
		client, cleanup = newTestClient(b, addr)
		tclient         = newTestingClient(client)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	var tp internal.TestPayload
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := tclient.Test(ctx, &tp); err != nil {
			b.Fatal(err)
		}
	}
}

func checkServerShutdown(t *testing.T, server *Server) {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()

	if len(server.listeners) > 0 {
		t.Errorf("expected listeners to be empty: %v", server.listeners)
	}
	for listener := range server.listeners {
		t.Logf("listener addr=%s", listener.Addr())
	}

	if len(server.connections) > 0 {
		t.Errorf("expected connections to be empty: %v", server.connections)
	}
	for conn := range server.connections {
		state, ok := conn.getState()
		if !ok {
			t.Errorf("failed to get state from %v", conn)
		}
		t.Logf("conn state=%s", state)
	}
}

type callResult struct {
	name     string
	err      error
	input    *internal.TestPayload
	expected *internal.TestPayload
	received *internal.TestPayload
}

func roundTrip(ctx context.Context, client *testingClient, name string) callResult {
	var (
		tp = &internal.TestPayload{
			Foo: name,
		}
	)

	ctx = WithMetadata(ctx, MD{"foo": []string{name}})

	resp, err := client.Test(ctx, tp)
	if err != nil {
		return callResult{
			name: name,
			err:  err,
		}
	}

	return callResult{
		name:     name,
		input:    tp,
		expected: &internal.TestPayload{Foo: strings.Repeat(tp.Foo, 2), Metadata: name},
		received: resp,
	}
}

func newTestClient(t testing.TB, addr string, opts ...ClientOpts) (*Client, func()) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(conn, opts...)
	return client, func() {
		conn.Close()
		client.Close()
	}
}

func newTestListener(t testing.TB) (string, net.Listener) {
	var (
		name   = t.Name()
		prefix string
	)

	// Abstracts sockets are only available on Linux.
	if runtime.GOOS == "linux" {
		prefix = "\x00"
	} else {
		if split := strings.SplitN(name, "/", 2); len(split) == 2 {
			name = split[0] + "-" + fmt.Sprintf("%x", md5.Sum([]byte(split[1])))
		}
	}
	addr := prefix + name
	listener, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}

	return addr, listener
}

func mustServer(t testing.TB) func(server *Server, err error) *Server {
	return func(server *Server, err error) *Server {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		return server
	}
}
