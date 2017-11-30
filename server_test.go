package ttrpc

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gogo/protobuf/proto"
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
	Foo string `protobuf:"bytes,1,opt,name=foo,proto3"`
}

func (r *testPayload) Reset()         { *r = testPayload{} }
func (r *testPayload) String() string { return fmt.Sprintf("%+#v", r) }
func (r *testPayload) ProtoMessage()  {}

// testingServer is what would be implemented by the user of this package.
type testingServer struct{}

func (s *testingServer) Test(ctx context.Context, req *testPayload) (*testPayload, error) {
	return &testPayload{Foo: strings.Repeat(req.Foo, 2)}, nil
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
		server          = NewServer()
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		tclient         = newTestingClient(client)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(listener)
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
		server          = NewServer()
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(listener)
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
		server      = NewServer()
		_, listener = newTestListener(t)
		errs        = make(chan error, 1)
	)

	go func() {
		errs <- server.Serve(listener)
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
		server                   = NewServer()
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
		serveErrs <- server.Serve(listener)
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
		if err := <-callErrs; err != nil {
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
		server      = NewServer()
		_, listener = newTestListener(t)
		startClose  = make(chan struct{})
		errs        = make(chan error, 1)
	)

	go func() {
		close(startClose)
		errs <- server.Serve(listener)
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
		server          = NewServer()
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(listener)
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
		server          = NewServer()
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(listener)
	}()

	registerTestingService(server, &testingServer{})

	tp := &testPayload{}
	// do a regular call
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// shutdown the server so the client stops receiving stuff.
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != ErrServerClosed {
		t.Fatal(err)
	}

	// server shutdown, but we still make a call.
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err == nil {
		t.Fatalf("expected error when calling against shutdown server")
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

	resp, err := client.Test(ctx, tp)
	if err != nil {
		t.Fatal(err)
	}

	results <- callResult{
		input:    tp,
		expected: &testPayload{Foo: strings.Repeat(tp.Foo, 2)},
		received: resp,
	}
}

func newTestClient(t *testing.T, addr string) (*Client, func()) {
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

func newTestListener(t *testing.T) (string, net.Listener) {
	addr := "\x00" + t.Name()
	listener, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}

	return addr, listener
}
