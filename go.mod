module github.com/containerd/ttrpc

go 1.13

require (
	github.com/gogo/protobuf v1.3.2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/procfs v0.6.0
	github.com/sirupsen/logrus v1.7.0
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013
	google.golang.org/grpc v1.38.0
)

replace (
	// Downgrading genproto is needed to fix "panic: protobuf tag not enough fields in Status.state:"
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200224152610-e50cd9704f63
)
