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

package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/integration/streaming"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/protobuf/types/known/emptypb"
)

func runService(ctx context.Context, t testing.TB, service streaming.TTRPCStreamingService) (streaming.TTRPCStreamingClient, func()) {
	server, err := ttrpc.NewServer()
	if err != nil {
		t.Fatal(err)
	}

	streaming.RegisterTTRPCStreamingService(server, service)

	addr := t.Name() + ".sock"
	if err := os.RemoveAll(addr); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		if t.Failed() {
			cancel()
			server.Close()
		}
	}()

	go func() {
		err := server.Serve(ctx, listener)
		if err != nil && !errors.Is(err, ttrpc.ErrServerClosed) {
			t.Error(err)
		}
	}()

	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatal(err)
	}

	client := ttrpc.NewClient(conn)
	return streaming.NewTTRPCStreamingClient(client), func() {
		client.Close()
		server.Close()
		conn.Close()
		cancel()
	}
}

type testStreamingService struct {
	t testing.TB
}

func (tss *testStreamingService) Echo(_ context.Context, e *streaming.EchoPayload) (*streaming.EchoPayload, error) {
	e.Seq++
	return e, nil
}

func (tss *testStreamingService) EchoStream(_ context.Context, es streaming.TTRPCStreaming_EchoStreamServer) error {
	for {
		var e streaming.EchoPayload
		if err := es.RecvMsg(&e); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		e.Seq++
		if err := es.SendMsg(&e); err != nil {
			return err
		}

	}
}

func (tss *testStreamingService) SumStream(_ context.Context, ss streaming.TTRPCStreaming_SumStreamServer) (*streaming.Sum, error) {
	var sum streaming.Sum
	for {
		var part streaming.Part
		if err := ss.RecvMsg(&part); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		sum.Sum = sum.Sum + part.Add
		sum.Num++
	}

	return &sum, nil
}

func (tss *testStreamingService) DivideStream(_ context.Context, sum *streaming.Sum, ss streaming.TTRPCStreaming_DivideStreamServer) error {
	parts := divideSum(sum)
	for _, part := range parts {
		if err := ss.Send(part); err != nil {
			return err
		}
	}
	return nil
}
func (tss *testStreamingService) EchoNull(_ context.Context, es streaming.TTRPCStreaming_EchoNullServer) (*empty.Empty, error) {
	msg := "non-empty empty"
	for seq := uint32(0); ; seq++ {
		var e streaming.EchoPayload
		if err := es.RecvMsg(&e); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if e.Seq != seq {
			return nil, fmt.Errorf("unexpected sequence %d, expected %d", e.Seq, seq)
		}
		if e.Msg != msg {
			return nil, fmt.Errorf("unexpected message %q, expected %q", e.Msg, msg)
		}
	}

	return &empty.Empty{}, nil
}

func (tss *testStreamingService) EchoNullStream(_ context.Context, es streaming.TTRPCStreaming_EchoNullStreamServer) error {
	msg := "non-empty empty"
	empty := &empty.Empty{}
	var wg sync.WaitGroup
	var sendErr error
	var errOnce sync.Once
	for seq := uint32(0); ; seq++ {
		var e streaming.EchoPayload
		if err := es.RecvMsg(&e); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if e.Seq != seq {
			return fmt.Errorf("unexpected sequence %d, expected %d", e.Seq, seq)
		}
		if e.Msg != msg {
			return fmt.Errorf("unexpected message %q, expected %q", e.Msg, msg)
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := es.SendMsg(empty); err != nil {
					errOnce.Do(func() {
						sendErr = err
					})
				}
			}()
		}
	}
	wg.Wait()

	return sendErr
}

func (tss *testStreamingService) EmptyPayloadStream(_ context.Context, _ *emptypb.Empty, streamer streaming.TTRPCStreaming_EmptyPayloadStreamServer) error {
	if err := streamer.Send(&streaming.EchoPayload{Seq: 1}); err != nil {
		return err
	}

	return streamer.Send(&streaming.EchoPayload{Seq: 2})
}

func TestStreamingService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := runService(ctx, t, &testStreamingService{t})
	defer cleanup()

	t.Run("Echo", echoTest(ctx, client))
	t.Run("EchoStream", echoStreamTest(ctx, client))
	t.Run("SumStream", sumStreamTest(ctx, client))
	t.Run("DivideStream", divideStreamTest(ctx, client))
	t.Run("EchoNull", echoNullTest(ctx, client))
	t.Run("EchoNullStream", echoNullStreamTest(ctx, client))
	t.Run("EmptyPayloadStream", emptyPayloadStream(ctx, client))
}

func echoTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		echo1 := &streaming.EchoPayload{
			Seq: 1,
			Msg: "Echo Me",
		}
		resp, err := client.Echo(ctx, echo1)
		if err != nil {
			t.Fatal(err)
		}
		assertNextEcho(t, echo1, resp)
	}

}

func echoStreamTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		stream, err := client.EchoStream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i = i + 2 {
			echoi := &streaming.EchoPayload{
				Seq: uint32(i),
				Msg: fmt.Sprintf("%d: Echo in a stream", i),
			}
			if err := stream.Send(echoi); err != nil {
				t.Fatal(err)
			}

			resp, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			assertNextEcho(t, echoi, resp)
		}

		if err := stream.CloseSend(); err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); err != io.EOF {
			t.Fatalf("Expected io.EOF, got %v", err)
		}
	}
}

func sumStreamTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		stream, err := client.SumStream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var sum streaming.Sum
		if err := stream.Send(&streaming.Part{}); err != nil {
			t.Fatal(err)
		}
		sum.Num++
		for i := -99; i <= 100; i++ {
			addi := &streaming.Part{
				Add: int32(i),
			}
			if err := stream.Send(addi); err != nil {
				t.Fatal(err)
			}
			sum.Sum = sum.Sum + int32(i)
			sum.Num++
		}
		if err := stream.Send(&streaming.Part{}); err != nil {
			t.Fatal(err)
		}
		sum.Num++

		ssum, err := stream.CloseAndRecv()
		if err != nil {
			t.Fatal(err)
		}
		assertSum(t, ssum, &sum)
	}
}

func divideStreamTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		expected := &streaming.Sum{
			Sum: 392,
			Num: 30,
		}

		stream, err := client.DivideStream(ctx, expected)
		if err != nil {
			t.Fatal(err)
		}

		var actual streaming.Sum
		for {
			part, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Fatal(err)
			}
			actual.Sum = actual.Sum + part.Add
			actual.Num++
		}
		assertSum(t, &actual, expected)
	}
}
func echoNullTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		stream, err := client.EchoNull(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			echoi := &streaming.EchoPayload{
				Seq: uint32(i),
				Msg: "non-empty empty",
			}
			if err := stream.Send(echoi); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := stream.CloseAndRecv(); err != nil {
			t.Fatal(err)
		}

	}
}
func echoNullStreamTest(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		stream, err := client.EchoNullStream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var c int
		wait := make(chan error)
		go func() {
			defer close(wait)
			for {
				_, err := stream.Recv()
				if err != nil {
					if err != io.EOF {
						wait <- err
					}
					return
				}
				c++
			}

		}()

		for i := 0; i < 100; i++ {
			echoi := &streaming.EchoPayload{
				Seq: uint32(i),
				Msg: "non-empty empty",
			}
			if err := stream.Send(echoi); err != nil {
				t.Fatal(err)
			}

		}

		if err := stream.CloseSend(); err != nil {
			t.Fatal(err)
		}

		select {

		case err := <-wait:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second * 10):
			t.Fatal("did not receive EOF within 10 seconds")
		}

	}
}

func emptyPayloadStream(ctx context.Context, client streaming.TTRPCStreamingClient) func(t *testing.T) {
	return func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		stream, err := client.EmptyPayloadStream(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		for i := uint32(1); i < 3; i++ {
			first, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}

			if first.Seq != i {
				t.Fatalf("unexpected seq: %d != %d", first.Seq, i)
			}
		}

		if _, err := stream.Recv(); err != io.EOF {
			t.Fatalf("Expected io.EOF, got %v", err)
		}
	}
}

func assertNextEcho(t testing.TB, a, b *streaming.EchoPayload) {
	t.Helper()
	if a.Msg != b.Msg {
		t.Fatalf("Mismatched messages: %q != %q", a.Msg, b.Msg)
	}
	if b.Seq != a.Seq+1 {
		t.Fatalf("Wrong sequence ID: got %d, expected %d", b.Seq, a.Seq+1)
	}
}

func assertSum(t testing.TB, a, b *streaming.Sum) {
	t.Helper()
	if a.Sum != b.Sum {
		t.Fatalf("Wrong sum %d, expected %d", a.Sum, b.Sum)
	}
	if a.Num != b.Num {
		t.Fatalf("Wrong num %d, expected %d", a.Num, b.Num)
	}
}

func divideSum(sum *streaming.Sum) []*streaming.Part {
	r := rand.New(rand.NewSource(14))
	var total int32
	parts := make([]*streaming.Part, sum.Num)
	for i := int32(1); i < sum.Num-2; i++ {
		add := r.Int31()%1000 - 500
		parts[i] = &streaming.Part{
			Add: add,
		}
		total = total + add
	}
	parts[0] = &streaming.Part{}
	parts[sum.Num-2] = &streaming.Part{
		Add: sum.Sum - total,
	}
	parts[sum.Num-1] = &streaming.Part{}
	return parts
}
