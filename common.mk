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

# Variables, rules and targets common to the main and subpackages.

# Default commands and binaries used for builds, testing, installation, etc.
GO        ?= go
GOTEST    ?= $(GO) test
GOBUILD   ?= $(GO) build ${DEBUG_GO_GCFLAGS} ${GO_GCFLAGS} ${GO_BUILD_FLAGS} ${EXTRA_FLAGS}
GOINSTALL ?= $(GO) install
INSTALL   ?= install

# Go build tags.
ifdef BUILDTAGS
    GO_BUILDTAGS = ${BUILDTAGS}
endif

GO_BUILDTAGS ?=
GO_TAGS       = $(if $(GO_BUILDTAGS),-tags "$(strip $(GO_BUILDTAGS))",)


# Go build and test flags.
GO_BUILD_FLAGS      =
TESTFLAGS_RACE      =
TESTFLAGS          ?= $(TESTFLAGS_RACE) $(EXTRA_TESTFLAGS)
TESTFLAGS_PARALLEL ?= 8

# See Golang issue re: '-trimpath': https://github.com/golang/go/issues/13809
GOPATHS   = $(shell echo ${GOPATH} | tr ":" "\n" | tr ";" "\n")
GO_GCFLAGS= $(shell				\
	set -- ${GOPATHS};			\
	echo "-gcflags=-trimpath=$${1}/src";)

# Project packages.
PACKAGES ?= $(shell \
    $(GO) list ${GO_TAGS} ./... | \
        grep -v /example)

# Packages to $(GOTEST).
TESTPACKAGES ?= $(shell \
    $(GO) list ${GO_TAGS} ./... | \
        grep -v /cmd | grep -v /integration | grep -v /example)

# Packages to $(GOBUILD) binaries from.
BINPACKAGES ?= $(if $(COMMANDS),$(addprefix ./cmd/,$(COMMANDS)),)
BINARIES    ?= $(if $(COMMANDS),$(addprefix bin/,$(COMMANDS)),)

define BUILD_BINARY
$(call WHALE_TARGET); \
$(GOBUILD) -o $@ ${GO_TAGS} ./$<
endef

SUBPACKAGES ?= $(shell \
    find . -name go.mod | tr -s ' ' '\n' | \
        grep -v '\./go.mod' | grep -v /example | \
        sed 's:/go.mod::g')

define HANDLE_SUBPACKAGES
for d in $(SUBPACKAGES); do \
    $(MAKE) -s -C $$d SUBPKG=$${d#./} $@; \
done
endef

define WHALE_TARGET
$(if $(SUBPKG),echo "$(WHALE) $@ $(SUBPKG)",echo "$(WHALE) $@")
endef

WHALE := "🇩"
ONI   := "👹"

# Do quiet builds by default. Override with V=1 or Q=
ifeq ($(V),1)
Q =
else
Q = @
endif

lint: ## run all linters
	$(Q)$(call WHALE_TARGET); \
	GOGC=75 golangci-lint run; \
	$(call HANDLE_SUBPACKAGES)

generate: protos
	$(Q)$(call WHALE_TARGET); \
	(@PATH="${ROOTDIR}/bin:${PATH}" $(GO) generate -x ${PACKAGES})

protos:
	$(Q)$(call WHALE_TARGET); \
	(PATH="${ROOTDIR}/bin:${PATH}" protobuild --quiet ${PACKAGES})

build: ## build the go packages
	$(Q)$(call WHALE_TARGET); \
	$(GOBUILD) ${PACKAGES}; \
	$(call HANDLE_SUBPACKAGES)

test: ## run tests, except integration tests and tests that require root
	$(Q)$(call WHALE_TARGET); \
	$(GOTEST) ${TESTFLAGS} ${TESTPACKAGES}; \
	$(call HANDLE_SUBPACKAGES)

benchmark: ## run benchmark tests
	$(Q)$(call WHALE_TARGET); \
	$(GOTEST) ${TESTFLAGS} -bench . -run Benchmark; \
	$(call HANDLE_SUBPACKAGES)

bin/%: cmd/% FORCE
	$(Q)$(call BUILD_BINARY)

binaries: $(BINARIES) ## build binaries
	$(Q)$(call WHALE_TARGET); \
	$(call HANDLE_SUBPACKAGES)

clean: ## clean up binaries
	$(Q)$(call WHALE_TARGET); \
	rm -f $(BINARIES); \
	$(call HANDLE_SUBPACKAGES)

install: ## install binaries
	$(Q)$(call WHALE_TARGET) "$(BINPACKAGES)"; \
	$(GOINSTALL) $(BINPACKAGES); \
	$(call HANDLE_SUBPACKAGES)

coverage: ## generate coverprofiles from the unit tests, except tests that require root
	$(Q)$(call WHALE_TARGET); \
	rm -f coverage.txt; \
	$(GOTEST) ${TESTFLAGS} ${TESTPACKAGES} 2> /dev/null; \
	for pkg in ${PACKAGES}; do \
	    $(GOTEST) ${TESTFLAGS} \
	        -cover \
	        -coverprofile=profile.out \
	        -covermode=atomic $$pkg || exit; \
	    if [ -f profile.out ]; then \
	        cat profile.out >> coverage.txt.raw; \
	        rm profile.out; \
	    fi; \
	done; \
        sort -u coverage.txt.raw > coverage.txt; \
        rm coverage.txt.raw; \
	$(call HANDLE_SUBPACKAGES)

vendor: mod-tidy mod-verify ## ensure that all the go.mod/go.sum files are up-to-date

mod-tidy:
	$(Q)$(call WHALE_TARGET); \
	$(GO) mod tidy; \
	$(call HANDLE_SUBPACKAGES)

mod-verify:
	$(Q)$(call WHALE_TARGET); \
	$(GO) mod verify; \
	$(call HANDLE_SUBPACKAGES)

help: ## this help
	$(Q)awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

FORCE:

.PHONY: lint generate protos build test benchmark binaries clean install coverage \
        vendor mod-tidy mod-verify help

.DEFAULT: default
