package mgrpc

import (
	"context"
	"net"
	"path"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/typeurl"
	"github.com/pkg/errors"
)

type Server struct {
	handlers map[string]map[string]Handler
}

func NewServer() *Server {
	return &Server{handlers: make(map[string]map[string]Handler)}
}

func (s *Server) Register(name string, methods map[string]Handler) error {
	if _, ok := s.handlers[name]; ok {
		return errors.Errorf("duplicate service %v registered", name)
	}

	s.handlers[name] = methods
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	// TODO(stevvooe): Wait on connection shutdown.
	return nil
}

func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			log.L.WithError(err).Error("failed accept")
		}

		go s.handleConn(conn)
	}

	return nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	var (
		ch          = newChannel(conn)
		req         Request
		ctx, cancel = context.WithCancel(context.Background())
	)

	defer cancel()

	// TODO(stevvooe): Recover here or in dispatch to handle panics in service
	// methods.

	// every connection is just a simple in/out request loop. No complexity for
	// multiplexing streams or dealing with head of line blocking, as this
	// isn't necessary for shim control.
	for {
		if err := ch.recv(ctx, &req); err != nil {
			log.L.WithError(err).Error("failed receiving message on channel")
			return
		}

		resp, err := s.dispatch(ctx, &req)
		if err != nil {
			log.L.WithError(err).Error("failed to dispatch request")
			return
		}

		if err := ch.send(ctx, resp); err != nil {
			log.L.WithError(err).Error("failed sending message on channel")
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, req *Request) (*Response, error) {
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("method", path.Join("/", req.Service, req.Method)))
	handler, err := s.resolve(req.Service, req.Method)
	if err != nil {
		log.L.WithError(err).Error("failed to resolve handler")
		return nil, err
	}

	payload, err := typeurl.UnmarshalAny(req.Payload)
	if err != nil {
		return nil, err
	}

	resp, err := handler.Handle(ctx, payload)
	if err != nil {
		log.L.WithError(err).Error("handler returned an error")
		return nil, err
	}

	apayload, err := typeurl.MarshalAny(resp)
	if err != nil {
		return nil, err
	}

	rresp := &Response{
		// Status:  *st,
		Payload: apayload,
	}

	return rresp, nil
}

func (s *Server) resolve(service, method string) (Handler, error) {
	srv, ok := s.handlers[service]
	if !ok {
		return nil, errors.Wrapf(errdefs.ErrNotFound, "could not resolve service %v", service)
	}

	handler, ok := srv[method]
	if !ok {
		return nil, errors.Wrapf(errdefs.ErrNotFound, "could not resolve method %v", method)
	}

	return handler, nil
}
