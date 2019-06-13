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

package main

import (
	context "context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"

	ttrpc "github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/example"
	"github.com/gogo/protobuf/types"
)

const socket = "example-ttrpc-server"

func main() {
	if err := handle(); err != nil {
		log.Fatal(err)
	}
}

func handle() error {
	command := os.Args[1]
	switch command {
	case "server":
		return server()
	case "client":
		return client()
	default:
		return errors.New("invalid command")
	}
}

func serverIntercept(ctx context.Context, um ttrpc.Unmarshaler, i *ttrpc.UnaryServerInfo, m ttrpc.Method) (interface{}, error) {
	log.Println("server interceptor")
	dumpMetadata(ctx)
	return m(ctx, um)
}

func clientIntercept(ctx context.Context, req *ttrpc.Request, resp *ttrpc.Response, i *ttrpc.UnaryClientInfo, invoker ttrpc.Invoker) error {
	log.Println("client interceptor")
	dumpMetadata(ctx)
	return invoker(ctx, req, resp)
}

func dumpMetadata(ctx context.Context) {
	md, ok := ttrpc.GetMetadata(ctx)
	if !ok {
		panic("no metadata")
	}
	if err := json.NewEncoder(os.Stdout).Encode(md); err != nil {
		panic(err)
	}
}

func server() error {
	s, err := ttrpc.NewServer(
		ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()),
		ttrpc.WithUnaryServerInterceptor(serverIntercept),
	)
	if err != nil {
		return err
	}
	defer s.Close()
	example.RegisterExampleService(s, &exampleServer{})

	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() {
		l.Close()
		os.Remove(socket)
	}()
	return s.Serve(context.Background(), l)
}

func client() error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	tc := ttrpc.NewClient(conn, ttrpc.WithUnaryClientInterceptor(clientIntercept))
	client := example.NewExampleClient(tc)

	r := &example.Method1Request{
		Foo: os.Args[2],
		Bar: os.Args[3],
	}

	ctx := context.Background()
	md := ttrpc.MD{}
	md.Set("name", "koye")
	ctx = ttrpc.WithMetadata(ctx, md)

	resp, err := client.Method1(ctx, r)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(resp)
}

type exampleServer struct {
}

func (s *exampleServer) Method1(ctx context.Context, r *example.Method1Request) (*example.Method1Response, error) {
	return &example.Method1Response{
		Foo: r.Foo,
		Bar: r.Bar,
	}, nil
}

func (s *exampleServer) Method2(ctx context.Context, r *example.Method1Request) (*types.Empty, error) {
	return &types.Empty{}, nil
}
