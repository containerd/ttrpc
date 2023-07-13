module github.com/containerd/ttrpc/otelttrpc/example

go 1.18

require (
	github.com/containerd/ttrpc v1.2.2
	github.com/containerd/ttrpc/otelttrpc v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v1.16.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.16.0
	go.opentelemetry.io/otel/sdk v1.16.0
	go.opentelemetry.io/otel/trace v1.16.0
	google.golang.org/protobuf v1.31.0
)

require (
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	go.opentelemetry.io/otel/metric v1.16.0 // indirect
	golang.org/x/net v0.11.0 // indirect
	golang.org/x/sys v0.9.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230731190214-cbb8c96f2d6d // indirect
	google.golang.org/grpc v1.57.0 // indirect
)

replace (
	github.com/containerd/ttrpc => ../../
	github.com/containerd/ttrpc/otelttrpc => ../
)
