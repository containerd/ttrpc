# ttrpc OpenTelemetry Instrumentation

This golang submodule/package implements OpenTelemetry instrumentation
support for ttrpc. This instrumentation code can be used to automatically
generate OpenTelemetry trace spans for RPC methods called on the ttrpc
client side and served on the ttrpc server side.

# Usage

Instrumentation provided by a unary client and a unary server interceptor.
These interceptors can be passed as ttrpc.ClientOpts and ttrpc.ServerOpt
to ttrpc during client and server creation with code like this:

```golang

   import (
       "github.com/containerd/ttrpc"
       "github.com/containerd/ttrpc/otelttrpc"
   )

   // on the client side
   ...
   client := ttrpc.NewClient(
       conn,
       ttrpc.UnaryClientInterceptor(
           otelttrpc.UnaryClientInterceptor(),
       ),
   )

   // and on the server side
   ...
   server, err := ttrpc.NewServer(
       ttrpc.WithUnaryServerInterceptor(
           otelttrpc.UnaryServerInterceptor(),
       ),
   )
```

These interceptors will then automatically handle generating trace Spans
for all called and served unary method calls. If the rest of the code is
properly set up to collect and export tracing data to opentelemetry, these
spans should show up as part of the collected traces.

For a more complete example see the [sample client](example/client/main.go)
and the [sample server](example/server/main.go) code in the [example directory](example).

# Limitations

Currently only unary client and unary server methods can be instrumented.
