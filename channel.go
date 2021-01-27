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
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	messageHeaderLength = 10
	messageLengthMax    = 4 << 20
)

type messageType uint8

const (
	messageTypeRequest        messageType = 0x1
	messageTypeResponse       messageType = 0x2
	messageTypeFileDescriptor messageType = 0x3
)

// messageHeader represents the fixed-length message header of 10 bytes sent
// with every request.
type messageHeader struct {
	Length   uint32      // length excluding this header. b[:4]
	StreamID uint32      // identifies which request stream message is a part of. b[4:8]
	Type     messageType // message type b[8]
	Flags    uint8       // reserved          b[9]
}

func readMessageHeader(p []byte, r io.Reader) (messageHeader, error) {
	_, err := io.ReadFull(r, p[:messageHeaderLength])
	if err != nil {
		return messageHeader{}, err
	}

	return messageHeader{
		Length:   binary.BigEndian.Uint32(p[:4]),
		StreamID: binary.BigEndian.Uint32(p[4:8]),
		Type:     messageType(p[8]),
		Flags:    p[9],
	}, nil
}

func writeMessageHeader(w io.Writer, p []byte, mh messageHeader) error {
	binary.BigEndian.PutUint32(p[:4], mh.Length)
	binary.BigEndian.PutUint32(p[4:8], mh.StreamID)
	p[8] = byte(mh.Type)
	p[9] = mh.Flags

	_, err := w.Write(p[:])
	return err
}

var buffers sync.Pool

type channel struct {
	conn  net.Conn
	bw    *bufio.Writer
	br    *bufio.Reader
	hrbuf [messageHeaderLength]byte // avoid alloc when reading header
	hwbuf [messageHeaderLength]byte
}

func newChannel(conn net.Conn) *channel {
	return &channel{
		conn: conn,
		bw:   bufio.NewWriter(conn),
		br:   bufio.NewReader(conn),
	}
}

// recv a message from the channel. The returned buffer contains the message.
//
// If a valid grpc status is returned, the message header
// returned will be valid and caller should send that along to
// the correct consumer. The bytes on the underlying channel
// will be discarded.
func (ch *channel) recv() (messageHeader, []byte, error) {
	mh, err := readMessageHeader(ch.hrbuf[:messageHeaderLength], ch.conn)
	if err != nil {
		return messageHeader{}, nil, err
	}

	if mh.Length > uint32(messageLengthMax) {
		if _, err := ch.br.Discard(int(mh.Length)); err != nil {
			return mh, nil, errors.Wrapf(err, "failed to discard after receiving oversized message")
		}

		return mh, nil, status.Errorf(codes.ResourceExhausted, "message length %v exceed maximum message size of %v", mh.Length, messageLengthMax)
	}

	p := ch.getmbuf(int(mh.Length))
	if _, err := io.ReadFull(ch.conn, p[:int(mh.Length)]); err != nil {
		return messageHeader{}, nil, errors.Wrapf(err, "failed reading message")
	}

	return mh, p, nil
}

func (ch *channel) recvFD(files *FileList) error {
	var (
		fds []int
		err error
	)

	switch t := ch.conn.(type) {
	case FdReceiver:
		fds, err = t.Recvfd()
		if err != nil {
			return err
		}
	case unixReader:
		oob := ch.getmbuf(unix.CmsgSpace(len(files.List) * 4))
		defer ch.putmbuf(oob)

		_, oobn, _, _, err := t.ReadMsgUnix(make([]byte, 1), oob)
		if err != nil {
			return err
		}

		ls, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return fmt.Errorf("error parsing socket controll message: %w", err)
		}

		for _, m := range ls {
			fdsTemp, err := unix.ParseUnixRights(&m)
			if err != nil {
				return fmt.Errorf("error parsing unix rights message: %w", err)
			}
			fds = append(fds, fdsTemp...)
		}
	default:
		return fmt.Errorf("receiving file descriptors is not supported on the transport")
	}

	if len(files.List) != len(fds) {
		return fmt.Errorf("received %d file descriptors, expected %d", len(fds), len(files.List))
	}
	for i, fd := range fds {
		files.List[i].Fileno = int64(fd)
	}
	return nil
}

func (ch *channel) sendFd(streamID uint32, mt messageType, files *FileList) error {
	fds := make([]int, len(files.List))

	for i, f := range files.List {
		fds[i] = int(f.Fileno)
	}

	switch t := ch.conn.(type) {
	case unixWriter:
		// Must send at least a single byte over unix sockets for the ancillary data to be accepted.
		_, _, err := t.WriteMsgUnix(make([]byte, 1), unix.UnixRights(fds...), nil)
		return err
	case FdSender:
		return t.SendFd(fds)
	default:
		return fmt.Errorf("sending file descriptors is not supported on the transport")
	}
}

func (ch *channel) send(streamID uint32, t messageType, p []byte) error {
	if err := writeMessageHeader(ch.bw, ch.hwbuf[:], messageHeader{Length: uint32(len(p)), StreamID: streamID, Type: t}); err != nil {
		return err
	}

	_, err := ch.bw.Write(p)
	if err != nil {
		return err
	}

	return ch.bw.Flush()
}

func (ch *channel) getmbuf(size int) []byte {
	// we can't use the standard New method on pool because we want to allocate
	// based on size.
	b, ok := buffers.Get().(*[]byte)
	if !ok || cap(*b) < size {
		// TODO(stevvooe): It may be better to allocate these in fixed length
		// buckets to reduce fragmentation but its not clear that would help
		// with performance. An ilogb approach or similar would work well.
		bb := make([]byte, size)
		b = &bb
	} else {
		*b = (*b)[:size]
	}
	return *b
}

func (ch *channel) putmbuf(p []byte) {
	buffers.Put(&p)
}

// FdReceiver is an interface used that the transport may implement to receive file descriptors from the client
type FdReceiver interface {
	Recvfd() ([]int, error)
}

// FdSender is an interface used that the transport may implement to send file descriptors to the server.
type FdSender interface {
	SendFd([]int) error
}

type unixReader interface {
	ReadMsgUnix(p, oob []byte) (n, oobn, flags int, addr *net.UnixAddr, err error)
}

type unixWriter interface {
	WriteMsgUnix(b, oob []byte, addr *net.UnixAddr) (n, oobn int, err error)
}
