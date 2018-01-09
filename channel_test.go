package ttrpc

import (
	"bytes"
	"context"
	"io"
	"net"
	"reflect"
	"testing"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReadWriteMessage(t *testing.T) {
	var (
		ctx      = context.Background()
		w, r     = net.Pipe()
		ch       = newChannel(w)
		rch      = newChannel(r)
		messages = [][]byte{
			[]byte("hello"),
			[]byte("this is a test"),
			[]byte("of message framing"),
		}
		received [][]byte
		errs     = make(chan error, 1)
	)

	go func() {
		for i, msg := range messages {
			if err := ch.send(ctx, uint32(i), 1, msg); err != nil {
				errs <- err
				return
			}
		}

		w.Close()
	}()

	for {
		_, p, err := rch.recv(ctx)
		if err != nil {
			if errors.Cause(err) != io.EOF {
				t.Fatal(err)
			}

			break
		}
		received = append(received, p)

		// make sure we don't have send errors
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		default:
		}
	}

	if !reflect.DeepEqual(received, messages) {
		t.Fatalf("didn't received expected set of messages: %v != %v", received, messages)
	}

	select {
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}
}

func TestMessageOversize(t *testing.T) {
	var (
		ctx      = context.Background()
		w, r     = net.Pipe()
		wch, rch = newChannel(w), newChannel(r)
		msg      = bytes.Repeat([]byte("a message of massive length"), 512<<10)
		errs     = make(chan error, 1)
	)

	go func() {
		if err := wch.send(ctx, 1, 1, msg); err != nil {
			errs <- err
		}
	}()

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

	select {
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}
}
