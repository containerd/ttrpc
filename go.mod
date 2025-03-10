module github.com/containerd/ttrpc

go 1.22
toolchain go1.22.5

require (
	github.com/containerd/log v0.1.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.4
	github.com/prometheus/procfs v0.6.0
	golang.org/x/sys v0.29.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f
	google.golang.org/grpc v1.71.0
	google.golang.org/protobuf v1.36.4
)

require github.com/sirupsen/logrus v1.9.3 // indirect
