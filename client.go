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
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrClosed is returned by client methods when the underlying connection is
// closed.
var ErrClosed = errors.New("ttrpc: closed")

// Client for a ttrpc server
type Client struct {
	codec   codec
	conn    net.Conn
	channel *channel

	streamLock   sync.RWMutex
	streams      map[streamID]*stream
	nextStreamID streamID

	sender sender

	ctx    context.Context
	closed func()

	closeOnce       sync.Once
	userCloseFunc   func()
	userCloseWaitCh chan struct{}

	errOnce     sync.Once
	err         error
	interceptor UnaryClientInterceptor
}

// ClientOpts configures a client
type ClientOpts func(c *Client)

// WithOnClose sets the close func whenever the client's Close() method is called
func WithOnClose(onClose func()) ClientOpts {
	return func(c *Client) {
		c.userCloseFunc = onClose
	}
}

// WithUnaryClientInterceptor sets the provided client interceptor
func WithUnaryClientInterceptor(i UnaryClientInterceptor) ClientOpts {
	return func(c *Client) {
		c.interceptor = i
	}
}

func NewClient(conn net.Conn, opts ...ClientOpts) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	channel := newChannel(conn)
	c := &Client{
		codec:           codec{},
		conn:            conn,
		channel:         channel,
		streams:         make(map[streamID]*stream),
		nextStreamID:    1,
		sender:          synchronizedSender(channel),
		closed:          cancel,
		ctx:             ctx,
		userCloseFunc:   func() {},
		userCloseWaitCh: make(chan struct{}),
		interceptor:     defaultClientInterceptor,
	}

	for _, o := range opts {
		o(c)
	}

	go c.run()
	return c
}

func (c *Client) Call(ctx context.Context, service, method string, req, resp interface{}) error {
	payload, err := c.codec.Marshal(req)
	if err != nil {
		return err
	}

	var (
		creq = &Request{
			Service: service,
			Method:  method,
			Payload: payload,
		}

		cresp = &Response{}
	)

	if metadata, ok := GetMetadata(ctx); ok {
		metadata.setRequest(creq)
	}

	if dl, ok := ctx.Deadline(); ok {
		creq.TimeoutNano = dl.Sub(time.Now()).Nanoseconds()
	}

	info := &UnaryClientInfo{
		FullMethod: fullPath(service, method),
	}
	if err := c.interceptor(ctx, creq, cresp, info, c.dispatch); err != nil {
		return err
	}

	if err := c.codec.Unmarshal(cresp.Payload, resp); err != nil {
		return err
	}

	if cresp.Status != nil && cresp.Status.Code != int32(codes.OK) {
		return status.ErrorProto(cresp.Status)
	}
	return nil
}

func (c *Client) dispatch(ctx context.Context, req *Request, resp *Response) error {
	p, err := c.codec.Marshal(req)
	if err != nil {
		return err
	}

	s, err := c.createStream()
	if err != nil {
		return err
	}
	defer c.deleteStream(s)

	err = s.send(messageTypeRequest, p)
	if err != nil {
		return filterCloseErr(err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return ErrClosed
	case msg, ok := <-s.recv:
		if !ok {
			return s.recvErr
		}

		if msg.header.Type == messageTypeResponse {
			err = proto.Unmarshal(msg.payload[:msg.header.Length], resp)
		} else {
			err = errors.New("unknown message type received")
		}

		// return the payload buffer for reuse
		c.channel.putmbuf(msg.payload)

		return err
	}
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closed()

		c.conn.Close()
	})
	return nil
}

// UserOnCloseWait is used to blocks untils the user's on-close callback
// finishes.
func (c *Client) UserOnCloseWait(ctx context.Context) error {
	select {
	case <-c.userCloseWaitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) run() {
	err := c.receiveLoop()
	c.Close()
	c.cleanupStreams(err)

	c.userCloseFunc()
	close(c.userCloseWaitCh)
}

func (c *Client) receiveLoop() error {
	for {
		select {
		case <-c.ctx.Done():
			return ErrClosed
		default:
			var (
				msg = &streamMessage{}
				err error
			)

			msg.header, msg.payload, err = c.channel.recv()
			if err != nil {
				_, ok := status.FromError(err)
				if !ok {
					// treat all errors that are not an rpc status as terminal.
					// all others poison the connection.
					return filterCloseErr(err)
				}
			}
			sid := streamID(msg.header.StreamID)
			s := c.getStream(sid)
			if s == nil {
				logrus.WithField("stream", sid).Errorf("ttrpc: received message on inactive stream")
			}

			if err != nil {
				s.closeWithError(err)
			} else {
				if err := s.receive(c.ctx, msg); err != nil {
					logrus.WithError(err).WithField("stream", sid).Errorf("ttrpc: failed to handle message")
				}
			}
		}
	}
}

// createStream creates a new stream and registers it with the client
// Introduce stream types for multiple or single response
func (c *Client) createStream() (*stream, error) {
	c.streamLock.Lock()
	defer c.streamLock.Unlock()

	// Check if closed since lock acquired to prevent adding
	// anything after cleanup completes
	select {
	case <-c.ctx.Done():
		return nil, ErrClosed
	default:
	}

	s := newStream(c.nextStreamID, c.sender)
	c.streams[s.id] = s
	c.nextStreamID = c.nextStreamID + 2

	return s, nil
}

func (c *Client) deleteStream(s *stream) {
	c.streamLock.Lock()
	delete(c.streams, s.id)
	c.streamLock.Unlock()
	s.closeWithError(nil)
}

func (c *Client) getStream(sid streamID) *stream {
	c.streamLock.RLock()
	s := c.streams[sid]
	c.streamLock.RUnlock()
	return s
}

func (c *Client) cleanupStreams(err error) {
	c.streamLock.Lock()
	defer c.streamLock.Unlock()

	for sid, s := range c.streams {
		s.closeWithError(err)
		delete(c.streams, sid)
	}
}

// filterCloseErr rewrites EOF and EPIPE errors to ErrClosed. Use when
// returning from call or handling errors from main read loop.
//
// This purposely ignores errors with a wrapped cause.
func filterCloseErr(err error) error {
	switch {
	case err == nil:
		return nil
	case err == io.EOF:
		return ErrClosed
	case errors.Is(err, io.ErrClosedPipe):
		return ErrClosed
	case errors.Is(err, io.EOF):
		return ErrClosed
	case strings.Contains(err.Error(), "use of closed network connection"):
		return ErrClosed
	default:
		// if we have an epipe on a write or econnreset on a read , we cast to errclosed
		var oerr *net.OpError
		if errors.As(err, &oerr) && (oerr.Op == "write" || oerr.Op == "read") {
			serr, sok := oerr.Err.(*os.SyscallError)
			if sok && ((serr.Err == syscall.EPIPE && oerr.Op == "write") ||
				(serr.Err == syscall.ECONNRESET && oerr.Op == "read")) {

				return ErrClosed
			}
		}
	}

	return err
}
