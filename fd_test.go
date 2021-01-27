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
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestSendRecvFd(t *testing.T) {
	var (
		ctx, cancel    = context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
		addr, listener = newTestListener(t)
	)

	defer cancel()

	// Spin up an out of process ttrpc server
	if err := listenerCmd(ctx, t.Name(), listener); err != nil {
		t.Fatal(err)
	}

	var (
		client, cleanup = newTestClient(t, addr)

		tclient = testFdClient{client}
	)
	defer cleanup()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err, "error creating test pipe")
	}
	defer r.Close()

	type readResp struct {
		buf []byte
		err error
	}

	expect := []byte("hello")

	chResp := make(chan readResp, 1)
	go func() {
		buf := make([]byte, len(expect))
		_, err := io.ReadFull(r, buf)
		chResp <- readResp{buf, err}
	}()

	if err := tclient.Test(ctx, w); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case resp := <-chResp:
		if resp.err != nil {
			t.Error(err)
		}
		if !bytes.Equal(resp.buf, expect) {
			t.Fatalf("got unexpected respone data, exepcted %q, got %q", string(expect), string(resp.buf))
		}
	}
}

type testFdPayload struct {
	Fds []int64 `protobuf:"varint,1,opt,name=fds,proto3"`
}

func (r *testFdPayload) Reset()         { *r = testFdPayload{} }
func (r *testFdPayload) String() string { return fmt.Sprintf("%+#v", r) }
func (r *testFdPayload) ProtoMessage()  {}

type testingServerFd struct {
	respData []byte
}

func (s *testingServerFd) Test(ctx context.Context, req *testFdPayload) error {
	for i, fd := range req.Fds {
		f := os.NewFile(uintptr(fd), "TEST_FILE_"+strconv.Itoa(i))
		go func() {
			f.Write(s.respData)
			f.Close()
		}()
	}

	return nil
}

type testFdClient struct {
	client *Client
}

func (c *testFdClient) Test(ctx context.Context, files ...*os.File) error {
	fds, err := c.client.Sendfd(ctx, files)
	if err != nil {
		return fmt.Errorf("error sending fds: %w", err)
	}

	tp := testFdPayload{}
	return c.client.Call(ctx, "Test", "Test", &testFdPayload{Fds: fds}, &tp)
}

func handleTestSendRecvFd(l net.Listener) error {
	s, err := NewServer()
	if err != nil {
		return err
	}
	testImpl := &testingServerFd{respData: []byte("hello")}

	s.Register("Test", map[string]Method{
		"Test": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			req := &testFdPayload{}

			if err := unmarshal(req); err != nil {
				return nil, err
			}

			return &testFdPayload{}, testImpl.Test(ctx, req)
		},
	})

	return s.Serve(context.TODO(), l)
}
