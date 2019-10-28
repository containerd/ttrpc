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
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/prometheus/procfs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const serviceName = "testService"

// testingService is our prototype service definition for use in testing the full model.
//
// Typically, this is generated. We define it here to ensure that that package
// primitive has what is required for generated code.
type testingService interface {
	Test(ctx context.Context, req *testPayload) (*testPayload, error)
}

type testingClient struct {
	client *Client
}

func newTestingClient(client *Client) *testingClient {
	return &testingClient{
		client: client,
	}
}

func (tc *testingClient) Test(ctx context.Context, req *testPayload) (*testPayload, error) {
	var tp testPayload
	return &tp, tc.client.Call(ctx, serviceName, "Test", req, &tp)
}

type testPayload struct {
	Foo      string `protobuf:"bytes,1,opt,name=foo,proto3"`
	Deadline int64  `protobuf:"varint,2,opt,name=deadline,proto3"`
	Metadata string `protobuf:"bytes,3,opt,name=metadata,proto3"`
}

func (r *testPayload) Reset()         { *r = testPayload{} }
func (r *testPayload) String() string { return fmt.Sprintf("%+#v", r) }
func (r *testPayload) ProtoMessage()  {}

// testingServer is what would be implemented by the user of this package.
type testingServer struct{}

func (s *testingServer) Test(ctx context.Context, req *testPayload) (*testPayload, error) {
	tp := &testPayload{Foo: strings.Repeat(req.Foo, 2)}
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
			var req testPayload
			if err := unmarshal(&req); err != nil {
				return nil, err
			}
			return svc.Test(ctx, &req)
		},
	})
}

func init() {
	proto.RegisterType((*testPayload)(nil), "testPayload")
	proto.RegisterType((*Request)(nil), "Request")
	proto.RegisterType((*Response)(nil), "Response")
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

	const calls = 2
	results := make(chan callResult, 2)
	go roundTrip(ctx, t, tclient, "bar", results)
	go roundTrip(ctx, t, tclient, "baz", results)

	for i := 0; i < calls; i++ {
		result := <-results
		if !reflect.DeepEqual(result.received, result.expected) {
			t.Fatalf("unexpected response: %+#v != %+#v", result.received, result.expected)
		}
	}
}

func TestServerNotFound(t *testing.T) {
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

	var tp testPayload
	if err := client.Call(ctx, "Not", "Found", &tp, &tp); err == nil {
		t.Fatalf("expected error from non-existent service call")
	} else if status, ok := status.FromError(err); !ok {
		t.Fatalf("expected status present in error: %v", err)
	} else if status.Code() != codes.NotFound {
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
		ctx                      = context.Background()
		server                   = mustServer(t)(NewServer())
		addr, listener           = newTestListener(t)
		shutdownStarted          = make(chan struct{})
		shutdownFinished         = make(chan struct{})
		handlersStarted          = make(chan struct{})
		handlersStartedCloseOnce sync.Once
		proceed                  = make(chan struct{})
		serveErrs                = make(chan error, 1)
		callwg                   sync.WaitGroup
		callErrs                 = make(chan error, ncalls)
		shutdownErrs             = make(chan error, 1)
		client, cleanup          = newTestClient(t, addr)
		_, cleanup2              = newTestClient(t, addr) // secondary connection
	)
	defer cleanup()
	defer cleanup2()

	// register a service that takes until we tell it to stop
	server.Register(serviceName, map[string]Method{
		"Test": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			var req testPayload
			if err := unmarshal(&req); err != nil {
				return nil, err
			}

			handlersStartedCloseOnce.Do(func() { close(handlersStarted) })
			<-proceed
			return &testPayload{Foo: "waited"}, nil
		},
	})

	go func() {
		serveErrs <- server.Serve(ctx, listener)
	}()

	// send a series of requests that will get blocked
	for i := 0; i < 5; i++ {
		callwg.Add(1)
		go func(i int) {
			callwg.Done()
			tp := testPayload{Foo: "half" + fmt.Sprint(i)}
			callErrs <- client.Call(ctx, serviceName, "Test", &tp, &tp)
		}(i)
	}

	<-handlersStarted
	go func() {
		close(shutdownStarted)
		shutdownErrs <- server.Shutdown(ctx)
		// server.Close()
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

func TestOversizeCall(t *testing.T) {
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

	tp := &testPayload{
		Foo: strings.Repeat("a", 1+messageLengthMax),
	}
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err == nil {
		t.Fatalf("expected error from non-existent service call")
	} else if status, ok := status.FromError(err); !ok {
		t.Fatalf("expected status present in error: %v", err)
	} else if status.Code() != codes.ResourceExhausted {
		t.Fatalf("expected code: %v != %v", status.Code(), codes.ResourceExhausted)
	}

	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != ErrServerClosed {
		t.Fatal(err)
	}
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

	tp := &testPayload{}
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

	// server shutdown, but we still make a call.
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err == nil {
		t.Fatalf("expected error when calling against shutdown server")
	} else if errors.Cause(err) != ErrClosed {
		t.Fatalf("expected to have a cause of ErrClosed, got %v", errors.Cause(err))
	}
}

func TestServerEOF(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()

	socketCountBefore := socketCount(t)

	go server.Serve(ctx, listener)

	registerTestingService(server, &testingServer{})

	tp := &testPayload{}
	// do a regular call
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err != nil {
		t.Fatalf("unexpected error during test call: %v", err)
	}

	// close the client, so that server gets EOF
	if err := client.Close(); err != nil {
		t.Fatalf("unexpected error while closing client: %v", err)
	}

	// server should eventually close the client connection
	maxAttempts := 20
	for i := 1; i <= maxAttempts; i++ {
		socketCountAfter := socketCount(t)
		if socketCountAfter < socketCountBefore {
			break
		}
		if i == maxAttempts {
			t.Fatalf("expected number of open sockets to be less than %d after client close, got %d open sockets",
				socketCountBefore, socketCountAfter)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestUnixSocketHandshake(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer(WithServerHandshaker(UnixSocketRequireSameUser())))
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

	var tp testPayload
	// server shutdown, but we still make a call.
	if err := client.Call(ctx, serviceName, "Test", &tp, &tp); err != nil {
		t.Fatalf("unexpected error making call: %v", err)
	}
}

func TestServerRequestTimeout(t *testing.T) {
	var (
		ctx, cancel     = context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		testImpl        = &testingServer{}
		client, cleanup = newTestClient(t, addr)
		result          testPayload
	)
	defer cancel()
	defer cleanup()
	defer listener.Close()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	if err := client.Call(ctx, serviceName, "Test", &testPayload{}, &result); err != nil {
		t.Fatalf("unexpected error making call: %v", err)
	}

	dl, _ := ctx.Deadline()
	if result.Deadline != dl.UnixNano() {
		t.Fatalf("expected deadline %v, actual: %v", dl, result.Deadline)
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

	var tp testPayload
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := tclient.Test(ctx, &tp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTripUnixSocketCreds(b *testing.B) {
	// TODO(stevvooe): Right now, there is a 5x performance decrease when using
	// unix socket credentials. See (UnixCredentialsFunc).Handshake for
	// details.

	var (
		ctx             = context.Background()
		server          = mustServer(b)(NewServer(WithServerHandshaker(UnixSocketRequireSameUser())))
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

	var tp testPayload
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
		t.Fatalf("expected listeners to be empty: %v", server.listeners)
	}

	if len(server.connections) > 0 {
		t.Fatalf("expected connections to be empty: %v", server.connections)
	}
}

type callResult struct {
	input    *testPayload
	expected *testPayload
	received *testPayload
}

func roundTrip(ctx context.Context, t *testing.T, client *testingClient, value string, results chan callResult) {
	t.Helper()
	var (
		tp = &testPayload{
			Foo: "bar",
		}
	)

	ctx = WithMetadata(ctx, MD{"foo": []string{"bar"}})

	resp, err := client.Test(ctx, tp)
	if err != nil {
		t.Fatal(err)
	}

	results <- callResult{
		input:    tp,
		expected: &testPayload{Foo: strings.Repeat(tp.Foo, 2), Metadata: "bar"},
		received: resp,
	}
}

func newTestClient(t testing.TB, addr string) (*Client, func()) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(conn)
	return client, func() {
		conn.Close()
		client.Close()
	}
}

func newTestListener(t testing.TB) (string, net.Listener) {
	addr := "\x00" + t.Name()
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

func socketCount(t *testing.T) int {
	proc, err := procfs.Self()
	if err != nil {
		t.Fatalf("unexpected error while reading procfs: %v", err)
	}
	fds, err := proc.FileDescriptorTargets()
	if err != nil {
		t.Fatalf("unexpected error while listing open file descriptors: %v", err)
	}

	sockets := 0
	for _, fd := range fds {
		if strings.Contains(fd, "socket") {
			sockets++
		}
	}
	return sockets
}
