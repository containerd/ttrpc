/*
   Copyright The containerd Authors.
   Copyright The OpenTelemetry Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.

   Based on go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc.
*/

package otelttrpc

// ttrpc tracing middleware
// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/semantic_conventions/rpc.md
import (
	"context"
	"net"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
	grpc_codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/contrib/otelttrpc/internal"
)

const (
	// instrumentationName is the name of this instrumentation package.
	instrumentationName = "github.com/containerd/ttrpc/contrib/otelttrpc"
)

type messageType attribute.KeyValue

// Event adds an event of the messageType to the span associated with the
// passed context with size (if message is a proto message).
func (m messageType) Event(ctx context.Context, message interface{}) {
	span := trace.SpanFromContext(ctx)
	if p, ok := message.(proto.Message); ok {
		span.AddEvent("message", trace.WithAttributes(
			attribute.KeyValue(m),
			semconv.MessageUncompressedSizeKey.Int(proto.Size(p)),
		))
	} else {
		span.AddEvent("message", trace.WithAttributes(
			attribute.KeyValue(m),
		))
	}
}

var (
	messageSent     = messageType(semconv.MessageTypeSent)
	messageReceived = messageType(semconv.MessageTypeReceived)
)

// UnaryClientInterceptor returns a ttrpc.UnaryClientInterceptor suitable
// for use in a ttrpc.NewClient call.
func UnaryClientInterceptor(opts ...Option) ttrpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		req *ttrpc.Request,
		reply *ttrpc.Response,
		info *ttrpc.UnaryClientInfo,
		invoker ttrpc.Invoker,
	) error {
		requestMetadata, _ := ttrpc.GetMetadata(ctx)
		metadataCopy := requestMetadata.Copy()

		tracer := newConfig(opts).TracerProvider.Tracer(instrumentationName)

		name, attr := spanInfo(ctx, info.FullMethod)
		var span trace.Span
		ctx, span = tracer.Start(
			ctx,
			name,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attr...),
		)
		defer span.End()

		Inject(ctx, &metadataCopy, opts...)
		ctx = ttrpc.WithMetadata(ctx, metadataCopy)

		messageSent.Event(ctx, req)

		err := invoker(ctx, req, reply)

		messageReceived.Event(ctx, reply)

		if err != nil {
			s, _ := status.FromError(err)
			span.SetStatus(codes.Error, s.Message())
			span.SetAttributes(statusCodeAttr(s.Code()))
		} else {
			span.SetAttributes(statusCodeAttr(grpc_codes.OK))
		}

		return err
	}
}

// UnaryServerInterceptor returns a ttrpc.UnaryServerInterceptor suitable
// for use in a ttrpc.NewServer call.
func UnaryServerInterceptor(opts ...Option) ttrpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req ttrpc.Unmarshaler,
		info *ttrpc.UnaryServerInfo,
		handler ttrpc.Method,
	) (interface{}, error) {
		requestMetadata, _ := ttrpc.GetMetadata(ctx)
		metadataCopy := requestMetadata.Copy()

		bags, spanCtx := Extract(ctx, &metadataCopy, opts...)
		ctx = baggage.ContextWithBaggage(ctx, bags)

		tracer := newConfig(opts).TracerProvider.Tracer(instrumentationName)

		name, attr := spanInfo(ctx, info.FullMethod)
		ctx, span := tracer.Start(
			trace.ContextWithRemoteSpanContext(ctx, spanCtx),
			name,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attr...),
		)
		defer span.End()

		messageReceived.Event(ctx, req)

		resp, err := handler(ctx, req)
		if err != nil {
			s, _ := status.FromError(err)
			span.SetStatus(codes.Error, s.Message())
			span.SetAttributes(statusCodeAttr(s.Code()))
			messageSent.Event(ctx, s.Proto())
		} else {
			span.SetAttributes(statusCodeAttr(grpc_codes.OK))
			messageSent.Event(ctx, resp)
		}

		return resp, err
	}
}

// spanInfo returns a span name and all appropriate attributes from the ttrpc
// method and peer address.
func spanInfo(ctx context.Context, fullMethod string) (string, []attribute.KeyValue) {
	attrs := []attribute.KeyValue{RPCSystemTTRPC}
	name, mAttrs := internal.ParseFullMethod(fullMethod)
	attrs = append(attrs, mAttrs...)
	if p, ok := peer.FromContext(ctx); ok {
		attrs = append(attrs, peerAttr(p)...)
	}
	return name, attrs
}

// peerAttr returns attributes about the peer address.
func peerAttr(p *peer.Peer) []attribute.KeyValue {
	switch p.Addr.Network() {
	case "unix":
		return []attribute.KeyValue{
			semconv.NetTransportUnix,
			semconv.NetPeerNameKey.String(p.Addr.String()),
		}
	case "tcp":
		attrs := []attribute.KeyValue{semconv.NetTransportTCP}
		if addr, ok := p.Addr.(*net.TCPAddr); ok {
			ip := addr.IP.String()
			if addr.Zone != "" {
				ip += "%" + addr.Zone
			}
			attrs = append(attrs, semconv.NetPeerIPKey.String(ip), semconv.NetPeerPortKey.Int(addr.Port))
		}
		return attrs
	default:
		return nil
	}
}

// statusCodeAttr returns status code attribute based on given ttrpc code
func statusCodeAttr(c grpc_codes.Code) attribute.KeyValue {
	return TTRPCStatusCodeKey.Int64(int64(c))
}
