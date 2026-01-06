//go:build windows
// +build windows

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

package main

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// listenConnection listens for incoming named pipe connections at the specified address.
func listenConnection(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, &winio.PipeConfig{
		// 0 buffer sizes for pipe is important to help deadlock to occur.
		// It can still occur if there is buffering, but it takes more IO volume to hit it.
		InputBufferSize:  0,
		OutputBufferSize: 0,
	})
}

// dialConnection dials a named pipe connection to the specified address.
func dialConnection(addr string) (net.Conn, error) {
	return winio.DialPipe(addr, nil)
}
