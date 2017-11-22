package ttrpc

import (
	"context"
	"net"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
)

type Client struct {
	channel *channel
}

func NewClient(conn net.Conn) *Client {
	return &Client{
		channel: newChannel(conn),
	}
}

func (c *Client) Call(ctx context.Context, service, method string, req, resp interface{}) error {
	var payload []byte
	switch v := req.(type) {
	case proto.Message:
		var err error
		payload, err = proto.Marshal(v)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("ttrpc: unknown request type: %T", req)
	}

	request := Request{
		Service: service,
		Method:  method,
		Payload: payload,
	}

	if err := c.channel.send(ctx, &request); err != nil {
		return err
	}

	var response Response
	if err := c.channel.recv(ctx, &response); err != nil {
		return err
	}
	switch v := resp.(type) {
	case proto.Message:
		if err := proto.Unmarshal(response.Payload, v); err != nil {
			return err
		}
	default:
		return errors.Errorf("ttrpc: unknown response type: %T", resp)
	}

	return nil
}
