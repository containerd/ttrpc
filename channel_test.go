package ttrpc

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReadWriteMessage(t *testing.T) {
	var (
		ctx      = context.Background()
		buffer   bytes.Buffer
		w        = bufio.NewWriter(&buffer)
		ch       = newChannel(w, nil)
		messages = [][]byte{
			[]byte("hello"),
			[]byte("this is a test"),
			[]byte("of message framing"),
		}
	)

	for i, msg := range messages {
		if err := ch.send(ctx, uint32(i), 1, msg); err != nil {
			t.Fatal(err)
		}
	}

	var (
		received [][]byte
		r        = bufio.NewReader(bytes.NewReader(buffer.Bytes()))
		rch      = newChannel(nil, r)
	)

	for {
		_, p, err := rch.recv(ctx)
		if err != nil {
			if errors.Cause(err) != io.EOF {
				t.Fatal(err)
			}

			break
		}
		received = append(received, p)
	}

	if !reflect.DeepEqual(received, messages) {
		t.Fatalf("didn't received expected set of messages: %v != %v", received, messages)
	}
}

func TestMessageOversize(t *testing.T) {
	var (
		ctx    = context.Background()
		buffer bytes.Buffer
		w      = bufio.NewWriter(&buffer)
		ch     = newChannel(w, nil)
		msg    = bytes.Repeat([]byte("a message of massive length"), 512<<10)
	)

	if err := ch.send(ctx, 1, 1, msg); err != nil {
		t.Fatal(err)
	}

	// now, read it off the channel with a small buffer
	var (
		r   = bufio.NewReader(bytes.NewReader(buffer.Bytes()))
		rch = newChannel(nil, r)
	)

	_, _, err := rch.recv(ctx)
	if err == nil {
		t.Fatalf("error expected reading with small buffer")
	}

	status, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected grpc status error: %v", err)
	}

	if status.Code() != codes.ResourceExhausted {
		t.Fatalf("expected grpc status code: %v != %v", status.Code(), codes.ResourceExhausted)
	}
}
