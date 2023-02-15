/*
   Copyright The containerd Authors.
   Copyright The OpenTelemetry Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.

   Based on go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc.
*/

package otelttrpc

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// Semantic conventions for attribute keys for gRPC.
const (
	// Name of message transmitted or received.
	RPCNameKey = attribute.Key("name")

	// Type of message transmitted or received.
	RPCMessageTypeKey = attribute.Key("message.type")

	// Identifier of message transmitted or received.
	RPCMessageIDKey = attribute.Key("message.id")

	// The compressed size of the message transmitted or received in bytes.
	RPCMessageCompressedSizeKey = attribute.Key("message.compressed_size")

	// The uncompressed size of the message transmitted or received in
	// bytes.
	RPCMessageUncompressedSizeKey = attribute.Key("message.uncompressed_size")
)

// Semantic conventions for ttrpc attributes.
var (
	// Semantic convention for ttrpc as the remoting system.
	RPCSystemTTRPC = semconv.RPCSystemKey.String("ttrpc")

	// Semantic convention for a message named message.
	RPCNameMessage = RPCNameKey.String("message")

	// TTRPCStatusCodeKey is the ttrpc analogue of gRPC convention for
	// numeric status code of a ttrpc request.
	TTRPCStatusCodeKey = attribute.Key("rpc.ttrpc.status_code")

	// Semantic conventions for RPC message types.
	RPCMessageTypeSent     = RPCMessageTypeKey.String("SENT")
	RPCMessageTypeReceived = RPCMessageTypeKey.String("RECEIVED")
)
