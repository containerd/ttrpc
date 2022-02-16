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
