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
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	ttrpc "github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/tracing/otelttrpc"
	"github.com/containerd/ttrpc/tracing/otelttrpc/otelexample"
	"go.opentelemetry.io/otel"
	stdout "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/protobuf/types/known/emptypb"
)

const socket = "example-ttrpc-socket"

// Init configures an OpenTelemetry exporter and trace provider.
func initTracer() (*sdktrace.TracerProvider, error) {
	exporter, err := stdout.New(stdout.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp, nil
}

func main() {
	// Setup OpenTelemetry tracer with stdout exporter
	initTracer()

	rand.Seed(time.Now().UnixNano())
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

func server() error {
	s, err := ttrpc.NewServer(
		ttrpc.WithServerHandshaker(defaultHandshaker()),
		ttrpc.WithUnaryServerInterceptor(otelttrpc.UnaryServerInterceptor()),
	)
	if err != nil {
		return err
	}
	defer s.Close()
	otelexample.RegisterExampleService(s, &exampleServer{})

	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}

	//Handle interrupts and exit gracefully
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a SIGINT or SIGKILL:
		sig := <-c
		log.Printf("Caught Signal %s: Shutting down.", sig)
		l.Close()
		os.Exit(0)
	}(sigc)

	return s.Serve(context.Background(), l)
}

func client() error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	tc := ttrpc.NewClient(conn, ttrpc.WithUnaryClientInterceptor(otelttrpc.UnaryClientInterceptor()))
	client := otelexample.NewExampleClient(tc)

	r := &otelexample.Method1Request{
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

func (s *exampleServer) Method1(ctx context.Context, r *otelexample.Method1Request) (*otelexample.Method1Response, error) {
	return &otelexample.Method1Response{
		Foo: r.Foo,
		Bar: r.Bar,
	}, nil
}

func (s *exampleServer) Method2(ctx context.Context, r *otelexample.Method1Request) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
