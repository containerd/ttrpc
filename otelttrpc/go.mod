module github.com/containerd/ttrpc/otelttrpc

go 1.13

require (
	github.com/containerd/ttrpc v1.2.2
	github.com/stretchr/testify v1.8.3
	go.opentelemetry.io/otel v1.16.0
	go.opentelemetry.io/otel/metric v1.16.0
	go.opentelemetry.io/otel/sdk v1.16.0
	go.opentelemetry.io/otel/trace v1.16.0
	google.golang.org/grpc v1.57.0
)

replace github.com/containerd/ttrpc => ../
