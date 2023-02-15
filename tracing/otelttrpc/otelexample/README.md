# ttrpc Tracing Example

Traces client and server calls via interceptors for [ttrpc](https://github.com/containerd/ttrpc), and based on `ttrpc`'s [example](https://github.com/containerd/ttrpc/tree/master/example).

## Build

```
$ cd ttrpc/tracing/otelttrpc/otelexample/cmd
$ go build
```

This will build with default executable file named `cmd`.

## Run server


```
$ export JAEGER_HOST=localhost
$ ./cmd serever
```

## Run client

```
$ export JAEGER_HOST=localhost
$ ./cmd client foo bar
```
