package ttrpc

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"testing"

	"github.com/pkg/errors"
)

func TestReadWriteMessage(t *testing.T) {
	var (
		channel  bytes.Buffer
		w        = bufio.NewWriter(&channel)
		messages = [][]byte{
			[]byte("hello"),
			[]byte("this is a test"),
			[]byte("of message framing"),
		}
	)

	for _, msg := range messages {
		if err := writemsg(w, msg); err != nil {
			t.Fatal(err)
		}
	}

	var (
		received [][]byte
		r        = bufio.NewReader(bytes.NewReader(channel.Bytes()))
	)

	for {
		var p [4096]byte
		n, err := readmsg(r, p[:])
		if err != nil {
			if errors.Cause(err) != io.EOF {
				t.Fatal(err)
			}

			break
		}
		received = append(received, p[:n])
	}

	if !reflect.DeepEqual(received, messages) {
		t.Fatal("didn't received expected set of messages: %v != %v", received, messages)
	}
}

func TestSmallBuffer(t *testing.T) {
	var (
		channel bytes.Buffer
		w       = bufio.NewWriter(&channel)
		msg     = []byte("a message of massive length")
	)

	if err := writemsg(w, msg); err != nil {
		t.Fatal(err)
	}

	// now, read it off the channel with a small buffer
	var (
		p = make([]byte, len(msg)-1)
		r = bufio.NewReader(bytes.NewReader(channel.Bytes()))
	)
	_, err := readmsg(r, p[:])
	if err == nil {
		t.Fatalf("error expected reading with small buffer")
	}

	if errors.Cause(err) != io.ErrShortBuffer {
		t.Fatalf("errors.Cause(err) should equal io.ErrShortBuffer: %v != %v", err, io.ErrShortBuffer)
	}
}

func BenchmarkReadWrite(b *testing.B) {
	b.StopTimer()
	var (
		messages = [][]byte{
			[]byte("hello"),
			[]byte("this is a test"),
			[]byte("of message framing"),
		}
		total   int64
		channel bytes.Buffer
		w       = bufio.NewWriter(&channel)
		p       [4096]byte
	)

	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		msg := messages[i%len(messages)]
		if err := writemsg(w, msg); err != nil {
			b.Fatal(err)
		}
		total += int64(len(msg))
	}
	b.SetBytes(total)

	r := bufio.NewReader(bytes.NewReader(channel.Bytes()))
	for i := 0; i < b.N; i++ {
		_, err := readmsg(r, p[:])
		if err != nil {
			if errors.Cause(err) != io.EOF {
				b.Fatal(err)
			}

			break
		}
	}
}
