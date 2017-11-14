package mgrpc

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/gogo/protobuf/proto"
)

// var serverMethods = map[string]Handler{
// 	"Create": HandlerFunc(func(ctx context.Context, req interface{}) (interface{}, error) {

// 	},
// }

type testPayload struct {
	Foo string `protobuf:"bytes,1,opt,name=foo,proto3"`
}

func (r *testPayload) Reset()         { *r = testPayload{} }
func (r *testPayload) String() string { return fmt.Sprintf("%+#v", r) }
func (r *testPayload) ProtoMessage()  {}

func init() {
	proto.RegisterType((*testPayload)(nil), "testpayload")
	proto.RegisterType((*Request)(nil), "Request")
	proto.RegisterType((*Response)(nil), "Response")
}

func TestServer(t *testing.T) {
	server := NewServer()
	ctx := context.Background()

	if err := server.Register("test-service", map[string]Handler{
		"Test": HandlerFunc(func(ctx context.Context, req interface{}) (interface{}, error) {
			fmt.Println(req)

			return &testPayload{Foo: "baz"}, nil
		}),
	}); err != nil {
		t.Fatal(err)
	}

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

	client := NewClient(conn)

	tp := &testPayload{
		Foo: "bar",
	}
	resp, err := client.Call(ctx, "test-service", "Test", tp)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(resp)
}
