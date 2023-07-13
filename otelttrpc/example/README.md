# ttRPC Tracing Example

Traces unary client and server calls via interceptors.

### Compile Example Client and Server

```sh
make
```

### Run server

```sh
./example-server
```

### Run client

```sh
./example-client
```

### Generate Protobuf Go and ttRPC bindings

If you modify the example protobuf definitions (`api/hello-service.proto`),
you need to regenerate the corresponding golang/ttRPC bindings.

```sh
# Install protoc and its dependencies if you don't have them yet.
make install-protoc install-protoc-dependencies install-ttrpc-plugin
# Regenerate golang/ttRPC bindings, recompile client and server.
make
```
