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
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/containerd/log"
	"github.com/containerd/ttrpc/internal"
	"github.com/sirupsen/logrus"
)

// TestClientStreamCleanupOnAbandon verifies the runtime.AddCleanup safety
// net attached to clientStream in NewStream. It guarantees three properties:
//
//  1. When the caller drops a clientStream without consuming it, the
//     underlying *stream is closed with errStreamAbandoned.
//  2. The cleanup unblocks the connection's receive loop, so other streams
//     and unary calls on the same connection continue to make progress
//     even when the buffer-full fallback timeout would otherwise apply.
//  3. errStreamAbandoned reaches the connection read loop's "failed to
//     handle message" log, identifying the abandon as the cause.
//
// streamFullTimeout is extended for the duration of the test so the only
// mechanism that can unblock the receive loop is the cleanup itself.
// Without the cleanup, the test would deadlock until the original 1-second
// timeout fired.
func TestClientStreamCleanupOnAbandon(t *testing.T) {
	prev := streamFullTimeout
	streamFullTimeout = time.Hour
	t.Cleanup(func() { streamFullTimeout = prev })

	// Build a context whose attached logger captures the abandon error
	// (forwarding all other entries to t.Log). The client is constructed
	// with this context so its internal receive loop logs through the
	// captured logger rather than the standard one.
	abandonSeen := make(chan struct{})
	ctx := withCaptureLogger(t, errStreamAbandoned, abandonSeen)

	server := mustServer(t)(NewServer())
	addr, listener := newTestListener(t)
	defer listener.Close()

	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClientWithContext(ctx, conn)
	defer func() {
		conn.Close()
		client.Close()
	}()

	const serviceName = "streamService"
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

	// Open a stream, fill the receive buffer to capacity so the
	// connection's read loop is reliably blocked in s.receive, and
	// abandon the *clientStream. Filling the buffer before returning is
	// what makes the next assertion deterministic: when the cleanup
	// fires, the blocked receive call is the path that surfaces
	// errStreamAbandoned to the log.
	s := abandonClientStream(t, ctx, client, serviceName)

	waitForStreamCleanup(t, s, 10*time.Second)

	// With streamFullTimeout pinned at one hour, only the cleanup can
	// have unblocked the receive loop. A unary call must therefore
	// complete promptly.
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req, resp internal.EchoPayload
	req.Seq = 42
	req.Msg = "must not deadlock"
	if err := client.Call(callCtx, serviceName, "Echo", &req, &resp); err != nil {
		t.Fatalf("unary Call did not complete after abandoned stream cleanup: %v", err)
	}
	if resp.Seq != 43 {
		t.Fatalf("unexpected sequence: got %d, want 43", resp.Seq)
	}

	// Wait for the abandon error to reach the receive-path log. This
	// is the path that surfaces the abandon to operators in production.
	// The log should already have fired by the time waitForStreamCleanup
	// returned (the cleanup itself is what unblocks the receive call
	// that emits the log), but allow a generous window to avoid any
	// scheduling-related flakiness.
	select {
	case <-abandonSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("expected errStreamAbandoned to surface in the connection read loop's error log")
	}
}

// waitForStreamCleanup drives GC repeatedly until the runtime.AddCleanup
// callback registered for the stream's parent clientStream has run, or the
// supplied timeout elapses. AddCleanup callbacks execute on a separate
// goroutine after a GC cycle marks the object unreachable, so polling is
// required.
func waitForStreamCleanup(t *testing.T, s *stream, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		runtime.GC()
		select {
		case <-s.recvClose:
			if s.recvErr != errStreamAbandoned {
				t.Fatalf("expected recvErr to be errStreamAbandoned, got %v", s.recvErr)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("clientStream cleanup did not run within deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// abandonClientStream creates a streaming RPC, sends enough messages to
// fill the client-side recv buffer (and thereby block the connection's
// read loop in s.receive), then returns the underlying *stream. The
// *clientStream is local to this function so it becomes unreachable as
// soon as the function returns, allowing GC to reclaim it.
//
// Waiting for the buffer to be full before returning is what makes the
// abandon-error log assertion deterministic: when the cleanup fires, the
// blocked s.receive call is the one path that emits "failed to handle
// message" with errStreamAbandoned. If the buffer were not full, the read
// loop might be idle between iterations when the cleanup runs, in which
// case the abandon never surfaces in the log.
//
//go:noinline
func abandonClientStream(t *testing.T, ctx context.Context, c *Client, service string) *stream {
	t.Helper()
	cs, err := c.NewStream(ctx, &StreamDesc{StreamingClient: true, StreamingServer: true}, service, "EchoStream", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := cs.(*clientStream).s

	// Send well above buffer capacity to overflow the channel once the
	// server echoes back; SendMsg only writes to the wire and does not
	// wait for the receive buffer to drain.
	for i := 0; i < cap(s.recv)*4; i++ {
		if err := cs.SendMsg(&internal.EchoPayload{Seq: int64(i), Msg: "fill"}); err != nil {
			break
		}
	}

	// Wait for the buffer to be full (read loop blocked in s.receive).
	bufferFull := time.Now().Add(5 * time.Second)
	for time.Now().Before(bufferFull) {
		if len(s.recv) == cap(s.recv) {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("recv buffer did not fill within deadline: have %d/%d", len(s.recv), cap(s.recv))
	return nil
}

// withCaptureLogger attaches a logger to t.Context() that records whether
// any log entry's "error" field matches target (signaling via the supplied
// channel) and forwards every other entry to t.Log. This mirrors the
// pattern used by github.com/containerd/log/logtest, but adds an
// assertion hook so a test can wait until a specific error has been
// observed without scraping or replacing the standard logger.
//
// Repeated "ttrpc: received message on inactive stream" entries are
// dropped: the test intentionally creates the condition that produces
// them, and they would otherwise drown out other diagnostic output.
func withCaptureLogger(t *testing.T, target error, seen chan<- struct{}) context.Context {
	t.Helper()
	hook := &captureHook{
		t:      t,
		target: target,
		seen:   seen,
		fmt: &logrus.TextFormatter{
			DisableColors:   true,
			TimestampFormat: log.RFC3339NanoFixed,
		},
	}

	l := logrus.New()
	l.SetLevel(logrus.DebugLevel)
	l.SetOutput(io.Discard)
	l.AddHook(hook)

	entry := logrus.NewEntry(l).WithField("testcase", t.Name())
	return log.WithLogger(t.Context(), entry)
}

// captureHook is a logrus.Hook that signals (via seen) the first time it
// observes an "error" field matching target, and forwards every other
// log entry to t.Log. Noisy logs intentionally produced by the test are
// dropped so they do not bury other output.
type captureHook struct {
	t      testing.TB
	target error
	seen   chan<- struct{}
	once   sync.Once
	fmt    logrus.Formatter
	mu     sync.Mutex
}

func (*captureHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *captureHook) Fire(e *logrus.Entry) error {
	if raw, ok := e.Data["error"]; ok {
		if err, ok := raw.(error); ok && errors.Is(err, h.target) {
			h.once.Do(func() { close(h.seen) })
		}
	}

	// Drop logs the test intentionally provokes; everything else is
	// forwarded to t.Log so failures retain useful diagnostic context.
	if e.Message == "ttrpc: received message on inactive stream" {
		return nil
	}

	formatted, err := h.fmt.Format(e)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.t.Log(string(bytes.TrimRight(formatted, "\n")))
	return nil
}
