/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package ttrpc

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// ErrProtocol is a general error in the handling the protocol.
	ErrProtocol = errors.New("protocol error")

	// ErrClosed is returned by client methods when the underlying connection is
	// closed.
	ErrClosed = errors.New("ttrpc: closed")

	// ErrServerClosed is returned when the Server has closed its connection.
	ErrServerClosed = errors.New("ttrpc: server closed")

	// ErrStreamClosed is when the streaming connection is closed.
	ErrStreamClosed = errors.New("ttrpc: stream closed")
)

// OversizedMessageErr is used to indicate refusal to send an oversized message.
// It wraps a ResourceExhausted grpc Status together with the offending message
// length.
type OversizedMessageErr struct {
	messageLength int
	maxLength     int
	err           error
}

var (
	oversizedMsgFmt     = "message length %d exceeds maximum message size of %d"
	oversizedMsgScanFmt = fmt.Sprintf("%v", status.New(codes.ResourceExhausted, oversizedMsgFmt))
)

// OversizedMessageError returns an OversizedMessageErr error for the given message
// length if it exceeds the allowed maximum. Otherwise a nil error is returned.
func OversizedMessageError(messageLength, maxLength int) error {
	if messageLength <= maxLength {
		return nil
	}

	return &OversizedMessageErr{
		messageLength: messageLength,
		maxLength:     maxLength,
		err:           OversizedMessageStatus(messageLength, maxLength).Err(),
	}
}

// OversizedMessageStatus returns a Status for an oversized message error.
func OversizedMessageStatus(messageLength, maxLength int) *status.Status {
	return status.Newf(codes.ResourceExhausted, oversizedMsgFmt, messageLength, maxLength)
}

// OversizedMessageFromError reconstructs an OversizedMessageErr from a Status.
func OversizedMessageFromError(err error) (*OversizedMessageErr, bool) {
	var (
		messageLength int
		maxLength     int
	)

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		return nil, false
	}

	// TODO(klihub): might be too ugly to recover an error this way... An
	// alternative would be to define our custom status detail proto type,
	// then use status.WithDetails() and status.Details().

	n, _ := fmt.Sscanf(st.Message(), oversizedMsgScanFmt, &messageLength, &maxLength)
	if n != 2 {
		n, _ = fmt.Sscanf(st.Message(), oversizedMsgFmt, &messageLength, &maxLength)
	}
	if n != 2 {
		return nil, false
	}

	return OversizedMessageError(messageLength, maxLength).(*OversizedMessageErr), true
}

// Error returns the error message for the corresponding grpc Status for the error.
func (e *OversizedMessageErr) Error() string {
	return e.err.Error()
}

// Unwrap returns the corresponding error with our grpc status code.
func (e *OversizedMessageErr) Unwrap() error {
	return e.err
}

// RejectedLength retrieves the rejected message length which triggered the error.
func (e *OversizedMessageErr) RejectedLength() int {
	return e.messageLength
}

// MaximumLength retrieves the maximum allowed message length that triggered the error.
func (e *OversizedMessageErr) MaximumLength() int {
	return e.maxLength
}
