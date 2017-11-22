package ttrpc

import (
	"context"
	"fmt"
	"net"
	"reflect"
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
type testingServer struct {
	payload *testPayload
}

func (s *testingServer) Test(ctx context.Context, req *testPayload) (*testPayload, error) {
	return s.payload, nil
}

func init() {
	proto.RegisterType((*testPayload)(nil), "testPayload")
	proto.RegisterType((*Request)(nil), "Request")
	proto.RegisterType((*Response)(nil), "Response")
}

func TestServer(t *testing.T) {
	var (
		ctx              = context.Background()
		server           = NewServer()
		expectedResponse = &testPayload{Foo: "baz"}
		testImpl         = &testingServer{payload: expectedResponse}
	)

	// more mocking of what is generated code. Unlike grpc, we register with a
	// closure so that the descriptor is allocated only on registration.
	registerTestingService := func(srv *Server, svc testingService) {
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

	registerTestingService(server, testImpl)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go server.Serve(listener)
	defer server.Shutdown(ctx)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := newTestingClient(NewClient(conn))

	tp := &testPayload{
		Foo: "bar",
	}

	resp, err := client.Test(ctx, tp)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(resp, expectedResponse) {
		t.Fatalf("unexpected response: %+#v != %+#v", resp, expectedResponse)
	}
}
