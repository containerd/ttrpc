package mgrpc

import "context"

type Handler interface {
	Handle(ctx context.Context, req interface{}) (interface{}, error)
}

type HandlerFunc func(ctx context.Context, req interface{}) (interface{}, error)

func (fn HandlerFunc) Handle(ctx context.Context, req interface{}) (interface{}, error) {
	return fn(ctx, req)
}
