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
	"net"
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

// holdWriteConn blocks the request write until gate closes, so the call
// reaches its select after the response and the close have been processed.
type holdWriteConn struct {
	net.Conn
	gate <-chan struct{}
}

func (c *holdWriteConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	<-c.gate
	return n, err
}

func TestClientResponseBeforeClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Every select case is ready when the call reaches dispatch. Repeat so a
	// random pick of the wrong case is caught.
	for i := range 100 {
		clientConn, serverConn := net.Pipe()
		go func() {
			defer serverConn.Close()
			ch := newChannel(serverConn)
			if mh, _, err := ch.recv(); err == nil {
				ch.send(mh.StreamID, messageTypeResponse, 0, nil)
			}
		}()

		gate := make(chan struct{})
		client := NewClient(&holdWriteConn{Conn: clientConn, gate: gate})
		go func() {
			client.UserOnCloseWait(ctx)
			close(gate)
		}()

		if err := client.Call(ctx, "service", "method", &Request{}, &Response{}); err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
	}
}
