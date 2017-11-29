package ttrpc

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"google.golang.org/grpc/status"
)

type Client struct {
	codec        codec
	channel      *channel
	requestID    uint32
	sendRequests chan sendRequest
	recvRequests chan recvRequest

	closed    chan struct{}
	closeOnce sync.Once
	done      chan struct{}
	err       error
}

func NewClient(conn net.Conn) *Client {
	c := &Client{
		codec:        codec{},
		requestID:    1,
		channel:      newChannel(conn, conn),
		sendRequests: make(chan sendRequest),
		recvRequests: make(chan recvRequest),
		closed:       make(chan struct{}),
		done:         make(chan struct{}),
	}

	go c.run()
	return c
}

func (c *Client) Call(ctx context.Context, service, method string, req, resp interface{}) error {
	requestID := atomic.AddUint32(&c.requestID, 2)
	if err := c.sendRequest(ctx, requestID, service, method, req); err != nil {
		return err
	}

	return c.recvResponse(ctx, requestID, resp)
}

func (c *Client) sendRequest(ctx context.Context, requestID uint32, service, method string, req interface{}) error {
	payload, err := c.codec.Marshal(req)
	if err != nil {
		return err
	}

	request := Request{
		Service: service,
		Method:  method,
		Payload: payload,
	}

	return c.send(ctx, requestID, &request)
}

func (c *Client) recvResponse(ctx context.Context, requestID uint32, resp interface{}) error {
	var response Response
	if err := c.recv(ctx, requestID, &response); err != nil {
		return err
	}

	if err := c.codec.Unmarshal(response.Payload, resp); err != nil {
		return err
	}

	if response.Status == nil {
		return errors.New("no status provided on response")
	}

	return status.ErrorProto(response.Status)
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})

	return nil
}

type sendRequest struct {
	ctx context.Context
	id  uint32
	msg interface{}
	err chan error
}

func (c *Client) send(ctx context.Context, id uint32, msg interface{}) error {
	errs := make(chan error, 1)
	select {
	case c.sendRequests <- sendRequest{
		ctx: ctx,
		id:  id,
		msg: msg,
		err: errs,
	}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.err
	}

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.err
	}
}

type recvRequest struct {
	id  uint32
	msg interface{}
	err chan error
}

func (c *Client) recv(ctx context.Context, id uint32, msg interface{}) error {
	errs := make(chan error, 1)
	select {
	case c.recvRequests <- recvRequest{
		id:  id,
		msg: msg,
		err: errs,
	}:
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errs:
		return err
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type received struct {
	mh  messageHeader
	p   []byte
	err error
}

func (c *Client) run() {
	defer close(c.done)
	var (
		waiters  = map[uint32]recvRequest{}
		queued   = map[uint32]received{} // messages unmatched by waiter
		incoming = make(chan received)
	)

	go func() {
		// start one more goroutine to recv messages without blocking.
		for {
			var p [messageLengthMax]byte
			// TODO(stevvooe): Something still isn't quite right with error
			// handling on the client-side, causing EOFs to come through. We
			// need other fixes in this changeset, so we'll address this
			// correctly later.
			mh, err := c.channel.recv(context.TODO(), p[:])
			select {
			case incoming <- received{
				mh:  mh,
				p:   p[:mh.Length],
				err: err,
			}:
			case <-c.done:
				return
			}
		}
	}()

	for {
		select {
		case req := <-c.sendRequests:
			if p, err := proto.Marshal(req.msg.(proto.Message)); err != nil {
				req.err <- err
			} else {
				req.err <- c.channel.send(req.ctx, req.id, messageTypeRequest, p)
			}
		case req := <-c.recvRequests:
			if r, ok := queued[req.id]; ok {
				req.err <- proto.Unmarshal(r.p, req.msg.(proto.Message))
			}
			waiters[req.id] = req
		case r := <-incoming:
			if waiter, ok := waiters[r.mh.StreamID]; ok {
				if r.err != nil {
					waiter.err <- r.err
				} else {
					waiter.err <- proto.Unmarshal(r.p, waiter.msg.(proto.Message))
				}
			} else {
				queued[r.mh.StreamID] = r
			}
		case <-c.closed:
			return
		}
	}
}
