package ttrpc

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
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
		ctx      = context.Background()
		server   = NewServer()
		testImpl = &testingServer{}
	)

	registerTestingService(server, testImpl)

	addr := "\x00" + t.Name()
	listener, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go server.Serve(listener)
	defer server.Shutdown(ctx)

	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := newTestingClient(NewClient(conn))

	const calls = 2
	results := make(chan callResult, 2)
	go roundTrip(ctx, t, client, "bar", results)
	go roundTrip(ctx, t, client, "baz", results)

	for i := 0; i < calls; i++ {
		result := <-results
		if !reflect.DeepEqual(result.received, result.expected) {
			t.Fatalf("unexpected response: %+#v != %+#v", result.received, result.expected)
		}
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
