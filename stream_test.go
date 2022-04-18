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

	"github.com/containerd/ttrpc/internal"
)

func TestStreamClient(t *testing.T) {
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
			"Echo": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
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
				Handler: func(ctx context.Context, ss StreamServer) (interface{}, error) {
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
	defer server.Shutdown(ctx)

	//func (c *Client) NewStream(ctx context.Context, desc *StreamDesc, service, method string) (ClientStream, error) {
	var req, resp internal.EchoPayload
	if err := client.Call(ctx, serviceName, "Echo", &req, &resp); err != nil {
		t.Fatal(err)
	}

	stream, err := client.NewStream(ctx, &StreamDesc{true, true}, serviceName, "EchoStream", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 100; i++ {
		req := internal.EchoPayload{
			Seq: int64(i),
			Msg: "should be returned",
		}
		if err := stream.SendMsg(&req); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		var resp internal.EchoPayload
		if err := stream.RecvMsg(&resp); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		if resp.Seq != int64(i)+1 {
			t.Fatalf("%d: unexpected sequence value: %d, expected %d", i, resp.Seq, i+1)
		}
		if resp.Msg != req.Msg {
			t.Fatalf("%d: unexpected message: %q, expected %q", i, resp.Msg, req.Msg)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}

	err = stream.RecvMsg(&resp)
	if err == nil {
		t.Fatal("expected io.EOF after close send")
	}
	if err != io.EOF {
		t.Fatalf("expected io.EOF after close send, got %v", err)
	}
}
