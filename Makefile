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

# Root directory of the project (absolute path).
export ROOTDIR = $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Project binaries.
COMMANDS = protoc-gen-go-ttrpc protoc-gen-gogottrpc

# Forcibly set the default goal to all, in case an include above brought in a rule definition.
.DEFAULT_GOAL := all

all: binaries

check: proto-fmt lint

integration: ## run integration tests
	$(Q)$(call WHALE_TARGET); \
	cd "${ROOTDIR}/integration" && \
	    $(GOTEST) -v ${TESTFLAGS} -parallel ${TESTFLAGS_PARALLEL} .


ci: check binaries check-protos coverage # coverage-integration ## to be used by the CI

AUTHORS: .mailmap .git/HEAD
	git log --format='%aN <%aE>' | sort -fu > $@

protos: $(BINARIES) ## generate protobuf

check-protos: protos ## check if protobufs needs to be generated again
	$(Q)$(call WHALE_TARGET); \
	test -z "$$(git status --short | grep ".pb.go" | tee /dev/stderr)" || \
		((git diff | cat) && \
		(echo "$(ONI) please run 'make protos' when making changes to proto files" && false))

check-api-descriptors: protos ## check that protobuf changes aren't present
	$(Q)$(call WHALE_TARGET); \
	test -z "$$(git status --short | grep ".pb.txt" | tee /dev/stderr)" || \
		((git diff $$(find . -name '*.pb.txt') | cat) && \
		(echo "$(ONI) please run 'make protos' when making changes to proto files and check-in the generated descriptor file changes" && false))

proto-fmt: ## check format of proto files
	$(Q)$(call WHALE_TARGET); \
	test -z "$$(find . -name '*.proto' -type f -exec grep -Hn -e "^ " {} \; | tee /dev/stderr)" || \
		(echo "$(ONI) please indent proto files with tabs only" && false); \
	test -z "$$(find . -name '*.proto' -type f -exec grep -Hn "Meta meta = " {} \; | grep -v '(gogoproto.nullable) = false' | tee /dev/stderr)" || \
		(echo "$(ONI) meta fields in proto files must have option (gogoproto.nullable) = false" && false)

install-protobuf:
	$(Q)$(call WHALE_TARGET); \
	script/install-protobuf

install-protobuild:
	$(Q)$(call WHALE_TARGET); \
	$(GOINSTALL) google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1 && \
	$(GOINSTALL) github.com/containerd/protobuild@14832ccc41429f5c4f81028e5af08aa233a219cf

verify-vendor: vendor ## verify if all the go.mod/go.sum files are up-to-date
	$(Q)test -z "$$(git status --short | grep "go.sum" | tee /dev/stderr)" || \
		((git diff | cat) && \
		(echo "$(ONI) make sure to checkin changes after go mod tidy" && false))

.PHONY: all check integration ci protos check-protos check-api-descriptors proto-fmt \
        install-protobuf install-protobuild verify-vendor

include common.mk
