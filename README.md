# mgrpc

GRPC for low-memory environments.

The existing grpc-go project requires a lot of memory overhead for importing
packages and at runtime. While this is great for many services with low density
requirements, this can be a problem when running a large number of services on
a single machine or on a machine with a small amount of memory.

This project reduces the binary size and protocol overhead when working with
GRPC. We do this by eliding the `net/http` and `net/http2` package used by grpc
with a lightweight framing protocol We do this by eliding the `net/http` and
`net/http2` package used by grpc with a lightweight framing protocol.

Please note that while this project supports generating either end of the
protocol, the generated service definitions will be incompatible with regular
GRPC services, as they do not speak the same protocol.

# Usage

Create a gogo vanity binary (see
[`cmd/protoc-gen-gogomgrpc/main.go`](cmd/protoc-gen-gogomgrpc/main.go) for an
example with the mgrpc plugin enabled.

It's recommended to use [`protobuild`](https://github.com/stevvooe/protobuild)
to build the protobufs for this project, but this will work with protoc
directly, if required.

# Differences from GRPC

- The protocol stack has been replaced with a lighter protocol that doesn't
  require http, http2 and tls.
- The client and server interface are identical whereas in GRPC there is a
  client and server interface that are different.
- The Go stdlib context package is used instead.

# Status

Very new. YMMV.
