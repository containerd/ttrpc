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
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	grpc_codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/containerd/ttrpc"
	"github.com/containerd/ttrpc/tracing/otelttrpc/internal"
)

const (
	// instrumentationName is the name of this instrumentation package.
	instrumentationName = "github.com/containerd/ttrpc/contrib/otelttrpc"
)

type messageType attribute.KeyValue

// Event adds an event of the messageType to the span associated with the
// passed context with a message id.
func (m messageType) Event(ctx context.Context, _ interface{}) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent("message", trace.WithAttributes(
		attribute.KeyValue(m),
	))
}

var (
	messageSent     = messageType(RPCMessageTypeSent)
	messageReceived = messageType(RPCMessageTypeReceived)
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
		requestMetadata, ok := ttrpc.GetMetadata(ctx)
		if !ok {
			requestMetadata = make(ttrpc.MD)
		}

		tracer := newConfig(opts).TracerProvider.Tracer(instrumentationName)

		name, attr := spanInfo(info.FullMethod, peerFromCtx(ctx))
		var span trace.Span
		ctx, span = tracer.Start(
			ctx,
			name,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attr...),
		)
		defer span.End()

		Inject(ctx, &requestMetadata, opts...)
		ctx = ttrpc.WithMetadata(ctx, requestMetadata)

		messageSent.Event(ctx, req)

		setRequest(req, &requestMetadata)

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
		requestMetadata, ok := ttrpc.GetMetadata(ctx)
		if !ok {
			requestMetadata = make(ttrpc.MD)
		}

		bags, spanCtx := Extract(ctx, &requestMetadata, opts...)
		ctx = baggage.ContextWithBaggage(ctx, bags)

		tracer := newConfig(opts).TracerProvider.Tracer(instrumentationName)

		name, attr := spanInfo(info.FullMethod, peerFromCtx(ctx))
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

func setRequest(req *ttrpc.Request, md *ttrpc.MD) {
	newMD := make([]*ttrpc.KeyValue, 0)
	for _, kv := range req.Metadata {
		// not found in md, means that we can copy old kv
		// otherwise, we will use the values in md to overwrite it
		if _, found := md.Get(kv.Key); !found {
			newMD = append(newMD, kv)
		}
	}

	req.Metadata = newMD

	for k, values := range *md {
		for _, v := range values {
			req.Metadata = append(req.Metadata, &ttrpc.KeyValue{
				Key:   k,
				Value: v,
			})
		}
	}
}

// spanInfo returns a span name and all appropriate attributes from the gRPC
// method and peer address.
func spanInfo(fullMethod, peerAddress string) (string, []attribute.KeyValue) {
	attrs := []attribute.KeyValue{RPCSystemTTRPC}
	name, mAttrs := internal.ParseFullMethod(fullMethod)
	attrs = append(attrs, mAttrs...)
	attrs = append(attrs, peerAttr(peerAddress)...)
	return name, attrs
}

// peerAttr returns attributes about the peer address.
func peerAttr(addr string) []attribute.KeyValue {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return []attribute.KeyValue(nil)
	}

	if host == "" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return []attribute.KeyValue(nil)
	}

	var attr []attribute.KeyValue
	if ip := net.ParseIP(host); ip != nil {
		attr = []attribute.KeyValue{
			semconv.NetSockPeerAddr(host),
			semconv.NetSockPeerPort(port),
		}
	} else {
		attr = []attribute.KeyValue{
			semconv.NetPeerName(host),
			semconv.NetPeerPort(port),
		}
	}

	return attr
}

// peerFromCtx returns a peer address from a context, if one exists.
func peerFromCtx(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	return p.Addr.String()
}

// statusCodeAttr returns status code attribute based on given ttrpc code
func statusCodeAttr(c grpc_codes.Code) attribute.KeyValue {
	return TTRPCStatusCodeKey.Int64(int64(c))
}
