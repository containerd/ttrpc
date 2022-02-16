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
	"strings"
	"testing"
	"time"

	"github.com/containerd/ttrpc/internal"
	"github.com/prometheus/procfs"
)

func TestUnixSocketHandshake(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer(WithServerHandshaker(UnixSocketRequireSameUser())))
		addr, listener  = newTestListener(t)
		errs            = make(chan error, 1)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()
	go func() {
		errs <- server.Serve(ctx, listener)
	}()

	registerTestingService(server, &testingServer{})

	var tp internal.TestPayload
	// server shutdown, but we still make a call.
	if err := client.Call(ctx, serviceName, "Test", &tp, &tp); err != nil {
		t.Fatalf("unexpected error making call: %v", err)
	}
}

func BenchmarkRoundTripUnixSocketCreds(b *testing.B) {
	// TODO(stevvooe): Right now, there is a 5x performance decrease when using
	// unix socket credentials. See (UnixCredentialsFunc).Handshake for
	// details.

	var (
		ctx             = context.Background()
		server          = mustServer(b)(NewServer(WithServerHandshaker(UnixSocketRequireSameUser())))
		testImpl        = &testingServer{}
		addr, listener  = newTestListener(b)
		client, cleanup = newTestClient(b, addr)
		tclient         = newTestingClient(client)
	)

	defer listener.Close()
	defer cleanup()

	registerTestingService(server, testImpl)

	go server.Serve(ctx, listener)
	defer server.Shutdown(ctx)

	var tp internal.TestPayload
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := tclient.Test(ctx, &tp); err != nil {
			b.Fatal(err)
		}
	}
}

func TestServerEOF(t *testing.T) {
	var (
		ctx             = context.Background()
		server          = mustServer(t)(NewServer())
		addr, listener  = newTestListener(t)
		client, cleanup = newTestClient(t, addr)
	)
	defer cleanup()
	defer listener.Close()

	socketCountBefore := socketCount(t)

	go server.Serve(ctx, listener)

	registerTestingService(server, &testingServer{})

	tp := &internal.TestPayload{}
	// do a regular call
	if err := client.Call(ctx, serviceName, "Test", tp, tp); err != nil {
		t.Fatalf("unexpected error during test call: %v", err)
	}

	// close the client, so that server gets EOF
	if err := client.Close(); err != nil {
		t.Fatalf("unexpected error while closing client: %v", err)
	}

	// server should eventually close the client connection
	maxAttempts := 20
	for i := 1; i <= maxAttempts; i++ {
		socketCountAfter := socketCount(t)
		if socketCountAfter < socketCountBefore {
			break
		}
		if i == maxAttempts {
			t.Fatalf("expected number of open sockets to be less than %d after client close, got %d open sockets",
				socketCountBefore, socketCountAfter)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func socketCount(t *testing.T) int {
	proc, err := procfs.Self()
	if err != nil {
		t.Fatalf("unexpected error while reading procfs: %v", err)
	}
	fds, err := proc.FileDescriptorTargets()
	if err != nil {
		t.Fatalf("unexpected error while listing open file descriptors: %v", err)
	}

	sockets := 0
	for _, fd := range fds {
		if strings.Contains(fd, "socket") {
			sockets++
		}
	}
	return sockets
}
