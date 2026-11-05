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
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/containerd/ttrpc/internal"
)

func TestUserOnCloseWait(t *testing.T) {
	var (
		ctx, cancel    = context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
		server         = mustServer(t)(NewServer())
		testImpl       = &testingServer{}
		addr, listener = newTestListener(t)
	)

	defer cancel()
	defer listener.Close()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	var (
		dataCh          = make(chan string)
		client, cleanup = newTestClient(t, addr,
			WithOnClose(func() {
				dataCh <- time.Now().String()
			}),
		)

		tp      internal.TestPayload
		tclient = newTestingClient(client)
	)

	if _, err := tclient.Test(ctx, &tp); err != nil {
		t.Fatal(err)
	}

	cleanup()

	fctx, fcancel := context.WithDeadline(ctx, time.Now().Add(1*time.Second))
	defer fcancel()
	if err := client.UserOnCloseWait(fctx); err == nil || err != context.DeadlineExceeded {
		t.Fatalf("expected error %v, but got %v", context.DeadlineExceeded, err)
	}

	<-dataCh

	if err := client.UserOnCloseWait(ctx); err != nil {
		t.Fatalf("expected error nil , but got %v", err)
	}
}

// gatedConn discards what is written to it, and holds the first write until
// it is released.
type gatedConn struct {
	net.Conn
	wrote   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *gatedConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.wrote) })
	<-c.release
	return len(p), nil
}

// callRacingClose returns what a call gets back when the client is closed
// before the call reaches its receive select, with a response already
// buffered for it when bufferResponse is set.
func callRacingClose(t *testing.T, bufferResponse bool) error {
	t.Helper()

	srv, cli := net.Pipe()
	gated := &gatedConn{
		Conn:    cli,
		wrote:   make(chan struct{}),
		release: make(chan struct{}),
	}

	client := NewClient(gated)
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		var tp internal.TestPayload
		errCh <- client.Call(context.Background(), serviceName, "Test",
			&internal.TestPayload{Foo: "bar"}, &tp)
	}()

	// The write is held inside createStream, which has already registered
	// stream 1. A response for it can be received before the call selects.
	//
	// Pinning that ordering relies on four properties of the client. The
	// tests still pass if any of them changes, but then cover nothing:
	//
	//   - createStream registers the stream before it sends the request,
	//     so holding the write leaves stream 1 able to receive.
	//   - nextStreamID starts at 1, so the response below finds the call.
	//   - a unary stream buffers one message, so the receive loop can
	//     deliver the response and go on to read the EOF.
	//   - run closes the client, which cancels c.ctx, before it cleans the
	//     streams up, so c.ctx is what the call selects on.
	<-gated.wrote

	if bufferResponse {
		payload, err := (codec{}).Marshal(&internal.TestPayload{Foo: "served"})
		if err != nil {
			t.Fatal(err)
		}
		b, err := (codec{}).Marshal(&Response{Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if err := newChannel(srv).send(1, messageTypeResponse, 0, b); err != nil {
			t.Fatal(err)
		}
	}

	srv.Close()
	<-client.ctx.Done()
	close(gated.release)

	select {
	case err := <-errCh:
		return err
	case <-time.After(testTimeout):
		t.Fatal("the call did not return")
		return nil
	}
}

// A response already received must be delivered even though the connection
// is gone, because the select that reads it also sees a closed client and
// picks between the two at random.
func TestClientPrefersBufferedResponseOverClose(t *testing.T) {
	const iterations = 50

	for i := range iterations {
		if err := callRacingClose(t, true); err != nil {
			t.Fatalf("run %d: buffered response lost: %v", i, err)
		}
	}
}

func TestClientReturnsErrClosedWithEmptyRecvBuffer(t *testing.T) {
	err := callRacingClose(t, false)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected %v, got %v", ErrClosed, err)
	}
}
