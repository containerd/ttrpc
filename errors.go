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

	// ErrStreamFull is returned when a stream's receive buffer is full
	// and the message cannot be delivered without blocking the
	// connection's receive loop. This prevents a single unconsumed
	// stream from deadlocking all other streams on the same connection.
	ErrStreamFull = errors.New("ttrpc: stream buffer full")
)

// closeConnectionErr wraps an error whose delivery closes the connection.
// Unexported because its zero value wraps a nil error, which encodes as a
// response with no status: CloseConnectionError is the only way to obtain
// one, and it never wraps nil.
type closeConnectionErr struct {
	err error
}

// CloseConnectionError returns err wrapped so that the server delivers err
// and then closes the connection, discarding any other response queued for
// it. Returns nil if err is nil, and err itself if it is already wrapped.
func CloseConnectionError(err error) error {
	if err == nil {
		return nil
	}

	// Wrapping twice would bury err deeper than the server unwraps it.
	if _, ok := err.(*closeConnectionErr); ok {
		return err
	}

	return &closeConnectionErr{err: err}
}

// Error returns the message of the wrapped error.
func (e *closeConnectionErr) Error() string {
	return e.err.Error()
}

// Unwrap returns the wrapped error.
func (e *closeConnectionErr) Unwrap() error {
	return e.err
}

// OversizedMessageErr is used to indicate refusal to send an oversized message.
// It wraps a ResourceExhausted grpc Status together with the offending message
// length.
type OversizedMessageErr struct {
	messageLength int
	err           error
}

// OversizedMessageError returns an OversizedMessageErr error for the given message
// length if it exceeds the allowed maximum. Otherwise a nil error is returned.
func OversizedMessageError(messageLength int) error {
	if messageLength <= messageLengthMax {
		return nil
	}

	return &OversizedMessageErr{
		messageLength: messageLength,
		err:           status.Errorf(codes.ResourceExhausted, "message length %v exceed maximum message size of %v", messageLength, messageLengthMax),
	}
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
func (*OversizedMessageErr) MaximumLength() int {
	return messageLengthMax
}
