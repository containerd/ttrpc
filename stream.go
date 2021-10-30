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
	"errors"
	"sync"
)

var ErrStreamClosed = errors.New("ttrpc: stream closed")

type streamID uint32

type streamMessage struct {
	header  messageHeader
	payload []byte
}

type stream struct {
	id     streamID
	sender sender
	recv   chan *streamMessage

	closeOnce sync.Once
	recvErr   error
}

func newStream(id streamID, send sender) *stream {
	return &stream{
		id:     id,
		sender: send,
		recv:   make(chan *streamMessage, 1),
	}
}

func (s *stream) closeWithError(err error) error {
	s.closeOnce.Do(func() {
		if s.recv != nil {
			s.recvErr = ErrStreamClosed
			close(s.recv)
		}
	})
	return nil
}

func (s *stream) send(mt messageType, b []byte) error {
	return s.sender.send(uint32(s.id), mt, b)
}

func (s *stream) receive(ctx context.Context, msg *streamMessage) error {
	select {
	case s.recv <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return s.recvErr
	}
}

type sender interface {
	send(uint32, messageType, []byte) error
}

func synchronizedSender(s sender) sender {
	return &syncSender{
		sender: s,
	}
}

type syncSender struct {
	l      sync.Mutex
	sender sender
}

func (ss *syncSender) send(sid uint32, mt messageType, b []byte) error {
	ss.l.Lock()
	defer ss.l.Unlock()
	return ss.sender.send(sid, mt, b)
}
