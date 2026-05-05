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
	"io"
	"testing"
	"time"

	"github.com/containerd/ttrpc/internal"
)

// TestStreamNotConsumedDoesNotBlockConnection verifies that a stream whose
// receive buffer fills up (because the client stopped consuming) does not
// block other streams or unary calls on the same connection.
//
// This guards against a deadlock where the client's receiveLoop blocks
// trying to deliver a message to a full stream, which prevents all other
// streams on the same connection from receiving anything.
func TestStreamNotConsumedDoesNotBlockConnection(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		serviceName     = "streamService"
	)

	defer listener.Close()
	defer cleanup()

	desc := &ServiceDesc{
		Methods: map[string]Method{
			"Echo": func(_ context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req internal.EchoPayload
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				req.Seq++
				return &req, nil
			},
		},
		Streams: map[string]Stream{
			"EchoStream": {
				Handler: func(_ context.Context, ss StreamServer) (interface{}, error) {
					for {
						var req internal.EchoPayload
						if err := ss.RecvMsg(&req); err != nil {
							if err == io.EOF {
								err = nil
							}
							return nil, err
						}
						req.Seq++
						if err := ss.SendMsg(&req); err != nil {
							return nil, err
						}
					}
				},
				StreamingClient: true,
				StreamingServer: true,
			},
		},
	}
	server.RegisterService(serviceName, desc)

	go server.Serve(ctx, listener)
	defer server.Close()

	// Create a bidirectional streaming RPC and send messages into it,
	// but never call RecvMsg. This will fill up the stream's receive
	// buffer (capacity 1) once the server echoes back.
	abandonedStream, err := client.NewStream(ctx, &StreamDesc{true, true}, serviceName, "EchoStream", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Send enough messages to guarantee the server has echoed back more
	// than the client-side buffer (capacity 1) can hold.
	for i := 0; i < 10; i++ {
		if err := abandonedStream.SendMsg(&internal.EchoPayload{
			Seq: int64(i),
			Msg: "abandoned",
		}); err != nil {
			// Send may fail if the stream is closed due to buffer full,
			// which is acceptable.
			break
		}
	}

	// Wait for the receive loop to detect the abandoned stream. The buffer
	// fills immediately, then the 1-second timeout fires, closing the
	// stream and unblocking the receive loop for other streams.
	time.Sleep(2 * time.Second)

	// A unary call on the same connection must succeed. Without the
	// timeout in stream.receive, the receiveLoop would still be blocked
	// trying to deliver to the abandoned stream.
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req, resp internal.EchoPayload
	req.Seq = 42
	req.Msg = "must not deadlock"
	if err := client.Call(callCtx, serviceName, "Echo", &req, &resp); err != nil {
		t.Fatalf("unary Call blocked by unconsumed stream: %v", err)
	}
	if resp.Seq != 43 {
		t.Fatalf("unexpected sequence: got %d, want 43", resp.Seq)
	}

	// Also verify a second stream works.
	stream2, err := client.NewStream(callCtx, &StreamDesc{true, true}, serviceName, "EchoStream", nil)
	if err != nil {
		t.Fatalf("NewStream blocked by unconsumed stream: %v", err)
	}
	if err := stream2.SendMsg(&internal.EchoPayload{Seq: 1, Msg: "hello"}); err != nil {
		t.Fatalf("SendMsg on second stream failed: %v", err)
	}
	var resp2 internal.EchoPayload
	if err := stream2.RecvMsg(&resp2); err != nil {
		t.Fatalf("RecvMsg on second stream failed: %v", err)
	}
	if resp2.Seq != 2 {
		t.Fatalf("unexpected sequence on stream2: got %d, want 2", resp2.Seq)
	}
}

// TestStreamFullOnServer verifies that when a server-side stream handler
// stops consuming messages, the server's receive goroutine is not blocked
// and can still process other streams. This guards against the same
// deadlock as TestStreamNotConsumedDoesNotBlockConnection but on the
// server side, where streamHandler.data() blocks the receive goroutine.
func TestStreamFullOnServer(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
		serviceName     = "streamService"
		handlerReady    = make(chan struct{})
	)

	defer listener.Close()
	defer cleanup()

	desc := &ServiceDesc{
		Methods: map[string]Method{
			"Echo": func(_ context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req internal.EchoPayload
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				req.Seq++
				return &req, nil
			},
		},
		Streams: map[string]Stream{
			"SlowConsumer": {
				Handler: func(ctx context.Context, _ StreamServer) (interface{}, error) {
					// Signal that the handler is running, then stop consuming.
					close(handlerReady)
					// Block until the context is cancelled (server shutdown).
					<-ctx.Done()
					return nil, ctx.Err()
				},
				StreamingClient: true,
				StreamingServer: false,
			},
		},
	}
	server.RegisterService(serviceName, desc)

	go server.Serve(ctx, listener)
	defer server.Close()

	// Open a stream whose server handler stops consuming after setup.
	slowStream, err := client.NewStream(ctx, &StreamDesc{StreamingClient: true}, serviceName, "SlowConsumer", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the handler to be ready (and stopped consuming).
	select {
	case <-handlerReady:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	// Send many messages to fill up the server's recv buffer (capacity 5).
	// The server handler is not consuming, so these will pile up.
	// We send in a goroutine because sends may eventually block.
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		for i := 0; i < 20; i++ {
			if err := slowStream.SendMsg(&internal.EchoPayload{
				Seq: int64(i),
				Msg: "filling buffer",
			}); err != nil {
				break
			}
		}
	}()

	// Wait for the server receive goroutine to detect the full buffer.
	// The 1-second timeout in data() fires, after which the receive
	// goroutine can process other streams again.
	time.Sleep(2 * time.Second)

	// Verify we can still make a unary call on the same connection.
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req, resp internal.EchoPayload
	req.Seq = 99
	if err := client.Call(callCtx, serviceName, "Echo", &req, &resp); err != nil {
		t.Fatalf("unary Call blocked by full server stream: %v", err)
	}
	if resp.Seq != 100 {
		t.Fatalf("unexpected sequence: got %d, want 100", resp.Seq)
	}
}
