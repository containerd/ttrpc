package ttrpc

import (
	"context"
	"net"

	"github.com/containerd/typeurl"
)

type Client struct {
	channel *channel
}

func NewClient(conn net.Conn) *Client {
	return &Client{
		channel: newChannel(conn),
	}
}

func (c *Client) Call(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	payload, err := typeurl.MarshalAny(req)
	if err != nil {
		return nil, err
	}

	request := Request{
		Service: service,
		Method:  method,
		Payload: payload,
	}

	if err := c.channel.send(ctx, &request); err != nil {
		return nil, err
	}

	var response Response

	if err := c.channel.recv(ctx, &response); err != nil {
		return nil, err
	}

	// TODO(stevvooe): Reliance on the typeurl isn't great for bootstrapping
	// and ease of use. Let's consider a request header frame and body frame as
	// a better solution. This will allow the caller to set the exact type.
	rpayload, err := typeurl.UnmarshalAny(response.Payload)
	if err != nil {
		return nil, err
	}

	return rpayload, nil
}
