package mgrpc

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"

	"github.com/containerd/containerd/log"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
)

const maxMessageSize = 8 << 10 // TODO(stevvooe): Cut these down, since they are pre-alloced.

type channel struct {
	conn net.Conn
	bw   *bufio.Writer
	br   *bufio.Reader
}

func newChannel(conn net.Conn) *channel {
	return &channel{
		conn: conn,
		bw:   bufio.NewWriterSize(conn, maxMessageSize),
		br:   bufio.NewReaderSize(conn, maxMessageSize),
	}
}

func (ch *channel) recv(ctx context.Context, msg interface{}) error {
	defer log.G(ctx).WithField("msg", msg).Info("recv")

	// TODO(stevvooe): Use `bufio.Reader.Peek` here to remove this allocation.
	var p [maxMessageSize]byte
	n, err := readmsg(ch.br, p[:])
	if err != nil {
		return err
	}

	switch msg := msg.(type) {
	case proto.Message:
		return proto.Unmarshal(p[:n], msg)
	default:
		return errors.Errorf("unnsupported type in channel: %#v", msg)
	}
}

func (ch *channel) send(ctx context.Context, msg interface{}) error {
	log.G(ctx).WithField("msg", msg).Info("send")
	var p []byte
	switch msg := msg.(type) {
	case proto.Message:
		var err error
		// TODO(stevvooe): trickiest allocation of the bunch. This will be hard
		// to get rid of without using `MarshalTo` directly.
		p, err = proto.Marshal(msg)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("unsupported type recv from channel: %#v", msg)
	}

	return writemsg(ch.bw, p)
}

func readmsg(r *bufio.Reader, p []byte) (int, error) {
	mlen, err := binary.ReadVarint(r)
	if err != nil {
		return 0, errors.Wrapf(err, "failed reading message size")
	}

	if mlen > int64(len(p)) {
		return 0, errors.Wrapf(io.ErrShortBuffer, "message length %v over buffer size %v", mlen, len(p))
	}

	nn, err := io.ReadFull(r, p[:mlen])
	if err != nil {
		return 0, errors.Wrapf(err, "failed reading message size")
	}

	if int64(nn) != mlen {
		return 0, errors.Errorf("mismatched read against message length %v != %v", nn, mlen)
	}

	return int(mlen), nil
}

func writemsg(w *bufio.Writer, p []byte) error {
	var (
		mlenp [binary.MaxVarintLen64]byte
		n     = binary.PutVarint(mlenp[:], int64(len(p)))
	)

	if _, err := w.Write(mlenp[:n]); err != nil {
		return errors.Wrapf(err, "failed writing message header")
	}

	if _, err := w.Write(p); err != nil {
		return errors.Wrapf(err, "failed writing message")
	}

	return w.Flush()
}
