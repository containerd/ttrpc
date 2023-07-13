#   Copyright The containerd Authors.

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

GO_CMD   := go
GO_BUILD := $(GO_CMD) build

PROTO_SOURCES := $(shell find . -name '*.proto' | grep -v /vendor/)
PROTO_GOFILES := $(patsubst %.proto,%.pb.go,$(PROTO_SOURCES))
PROTO_INCLUDE := -I$(PWD):/usr/local/include:/usr/include
PROTO_OPTIONS := --proto_path=. $(PROTO_INCLUDE) \
    --go_opt=paths=source_relative --go_out=. \
    --go-ttrpc_opt=paths=source_relative --go-ttrpc_out=.
PROTO_COMPILE := PATH=$(PATH):$(shell go env GOPATH)/bin; protoc $(PROTO_OPTIONS)

INSTALL_PROTOC := https://github.com/containerd/containerd/blob/main/script/setup/install-protobuf
PROTOC_DEPS    := google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.0
TTRPC_PLUGIN   := github.com/containerd/ttrpc/cmd/protoc-gen-go-ttrpc@74421d10189e8c118870d294c9f7f62db2d33ec1
WGET           := wget

BINARIES := example-client example-server

ifneq ($(V),1)
  Q := @
endif


#
# top-level targets
#

all: build

build: $(BINARIES)

clean:
	$(Q)rm -f $(BINARIES)

#
# binary compilation targets
#

example-client: $(wildcard api/*.go config/*.go client/*.go)
	$(Q)echo "Building $@..."; \
	$(GO_BUILD) -o $@ ./client

example-server: $(wildcard api/*.go config/*.go server/*.go)
	$(Q)echo "Building $@..."; \
	$(GO_BUILD) -o $@ ./server

#
# proto generation targets
#

%.pb.go: %.proto
	$(Q)echo "Generating $@..."; \
	$(PROTO_COMPILE) $<

#
# targets for installing dependencies
#

install-protoc install-protobuf:
	$(Q)$(WGET) $(INSTALL_PROTOC) && chmod a+x install-protobuf && ./install-protobuf

install-protoc-dependencies:
	$(Q)$(GO_INSTALL) -mod=mod $(PROTOC_DEPS)

install-ttrpc-plugin:
	$(Q)$(GO_INSTALL) -mod=mod $(TTRPC_PLUGIN)
