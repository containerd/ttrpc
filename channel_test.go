package ttrpc

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/pkg/errors"
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
		var p [4096]byte
		mh, err := rch.recv(ctx, p[:])
		if err != nil {
			if errors.Cause(err) != io.EOF {
				t.Fatal(err)
			}

			break
		}
		received = append(received, p[:mh.Length])
	}

	if !reflect.DeepEqual(received, messages) {
		t.Fatalf("didn't received expected set of messages: %v != %v", received, messages)
	}
}

func TestSmallBuffer(t *testing.T) {
	var (
		ctx    = context.Background()
		buffer bytes.Buffer
		w      = bufio.NewWriter(&buffer)
		ch     = newChannel(w, nil)
		msg    = []byte("a message of massive length")
	)

	if err := ch.send(ctx, 1, 1, msg); err != nil {
		t.Fatal(err)
	}

	// now, read it off the channel with a small buffer
	var (
		p   = make([]byte, len(msg)-1)
		r   = bufio.NewReader(bytes.NewReader(buffer.Bytes()))
		rch = newChannel(nil, r)
	)

	_, err := rch.recv(ctx, p[:])
	if err == nil {
		t.Fatalf("error expected reading with small buffer")
	}

	if errors.Cause(err) != io.ErrShortBuffer {
		t.Fatalf("errors.Cause(err) should equal io.ErrShortBuffer: %v != %v", err, io.ErrShortBuffer)
	}
}
