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

// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/otelttrpc"
	"github.com/containerd/ttrpc/otelttrpc/example/api"
	"github.com/containerd/ttrpc/otelttrpc/example/config"
	// "google.golang.org/grpc/metadata"
)

func main() {
	tp, err := config.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	var conn net.Conn
	conn, err = net.Dial("tcp", ":7777")

	if err != nil {
		log.Fatalf("failed to dial/connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := ttrpc.NewClient(conn,
		ttrpc.WithUnaryClientInterceptor(
			otelttrpc.UnaryClientInterceptor(),
		),
	)

	c := api.NewHelloServiceClient(client)

	for i := 0; i < 10; i++ {
		if err := callSayHello(c); err != nil {
			log.Fatal(err)
		}
		time.Sleep(250 * time.Millisecond)
	}

	time.Sleep(10 * time.Millisecond)
}

func callSayHello(c api.HelloServiceService) error {
	md := ttrpc.MD{}
	md.Set("timestamp", time.Now().Format(time.StampNano))
	md.Set("client-id", "web-api-client-us-east-1")
	md.Set("user-id", "some-test-user-id")
	ctx := ttrpc.WithMetadata(context.Background(), md)

	response, err := c.SayHello(ctx, &api.HelloRequest{Greeting: "World"})
	if err != nil {
		return fmt.Errorf("calling SayHello: %w", err)
	}

	log.Printf("Response from server: %s", response.Reply)

	return nil
}
