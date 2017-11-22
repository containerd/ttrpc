package ttrpc

import (
	"context"
	"net"

	"github.com/containerd/containerd/log"
)

type Server struct {
	services *serviceSet
}

func NewServer() *Server {
	return &Server{
		services: newServiceSet(),
	}
}

func (s *Server) Register(name string, methods map[string]Method) {
	s.services.register(name, methods)
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
			continue
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

		p, status := s.services.call(ctx, req.Service, req.Method, req.Payload)

		resp := &Response{
			Status:  status.Proto(),
			Payload: p,
		}

		if err := ch.send(ctx, resp); err != nil {
			log.L.WithError(err).Error("failed sending message on channel")
			return
		}
	}
}
