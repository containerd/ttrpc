module github.com/containerd/ttrpc

go 1.19

require (
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.3
	github.com/prometheus/procfs v0.6.0
	github.com/sirupsen/logrus v1.8.1
	golang.org/x/sys v0.18.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230731190214-cbb8c96f2d6d
	google.golang.org/grpc v1.57.1
	google.golang.org/protobuf v1.33.0
)

require golang.org/x/net v0.23.0 // indirect
