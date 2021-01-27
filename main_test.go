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
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	var ttrpcListener bool
	flag.BoolVar(&ttrpcListener, "listener", false, "Makes the test binary run a ttrpc listener for testing purposes instead of running a test")
	flag.Parse()

	if ttrpcListener {
		handleListenerCmd()
	}

	os.Exit(m.Run())
}

var listenerHandlers = map[string]func(net.Listener) error{
	"TestSendRecvFd": handleTestSendRecvFd,
}

// Starts a ttrpc serverout of process.
//
// The caller is responsible for creating the listener.
// The listener implementation must be able to be converted to an *os.File
// The passed in listtener fd will be closed when this function returns
//   (because the fd is copied and handed off to the other process).
func listenerCmd(ctx context.Context, handler string, l net.Listener) error {
	defer l.Close()

	cmd := exec.CommandContext(ctx, os.Args[0], "-listener=true")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(cmd.Env, "TEST_HANDLER="+handler)

	f, err := l.(fileListener).File()
	if err != nil {
		return err
	}
	defer f.Close()

	cmd.ExtraFiles = []*os.File{f}
	return cmd.Start()
}

type fileListener interface {
	File() (*os.File, error)
}

func handleListenerCmd() {
	h := listenerHandlers[os.Getenv("TEST_HANDLER")]
	l, err := net.FileListener(os.NewFile(3, "TEST_LISTENER"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if err := h(l); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	os.Exit(0)
}
