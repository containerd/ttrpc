#!/usr/bin/env bash

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

set -euxo pipefail

pkg_dir=$HOME/go/src/github.com/containerd/ttrpc
dest_dir=$HOME/go/src

PATH=$pkg_dir/cmd/protoc-gen-go-ttrpc:$HOME/go/bin:$PATH

(cd $pkg_dir/cmd/protoc-gen-go-ttrpc && go build)

src_dir=$pkg_dir/example

cd $pkg_dir/example
protoc -I=$src_dir --go_out=$dest_dir --go-ttrpc_out=$dest_dir $src_dir/example.proto
go build
