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
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	ttrpc "github.com/containerd/ttrpc"
	payload "github.com/containerd/ttrpc/cmd/ttrpc-stress/payload"

	"golang.org/x/sync/errgroup"
)

// main is the entry point of the stress utility.
func main() {
	// Define a flag for displaying usage information.
	flagHelp := flag.Bool("help", false, "Display usage")
	flag.Parse()

	// Check if help flag is set or if there are insufficient arguments.
	if *flagHelp || flag.NArg() < 2 {
		usage()
	}

	// Switch based on the first argument to determine mode (server or client).
	switch flag.Arg(0) {
	case "server":
		// Ensure correct number of arguments for server mode.
		if flag.NArg() != 2 {
			usage()
		}

		addr := flag.Arg(1)

		// Run the server and handle any errors.
		err := runServer(context.Background(), addr)
		if err != nil {
			log.Fatalf("error: %s", err)
		}

	case "client":
		// Ensure correct number of arguments for client mode.
		if flag.NArg() != 4 {
			usage()
		}

		addr := flag.Arg(1)

		// Parse iterations and workers arguments.
		iters, err := strconv.Atoi(flag.Arg(2))
		if err != nil {
			log.Fatalf("failed parsing iters: %s", err)
		}

		workers, err := strconv.Atoi(flag.Arg(3))
		if err != nil {
			log.Fatalf("failed parsing workers: %s", err)
		}

		// Run the client and handle any errors.
		err = runClient(context.Background(), addr, iters, workers)
		if err != nil {
			log.Fatalf("runtime error: %s", err)
		}

	default:
		// Display usage information if the mode is unrecognized.
		usage()
	}
}

// usage prints the usage information and exits the program.
// usage prints the usage information for the program and exits.
func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
	stress server <ADDR>
		Run the server with the specified unix socket or named pipe.
	stress client <ADDR> <ITERATIONS> <WORKERS>
		Run the client with the specified unix socket or named pipe, number of ITERATIONS, and number of WORKERS.
`)
	os.Exit(1)
}

// runServer sets up and runs the server.
func runServer(ctx context.Context, addr string) error {
	log.Printf("Starting server on %s", addr)

	// Listen for connections on the specified address.
	l, err := listenConnection(addr)
	if err != nil {
		return fmt.Errorf("failed listening on %s: %w", addr, err)
	}

	// Create a new ttrpc server.
	server, err := ttrpc.NewServer()
	if err != nil {
		return fmt.Errorf("failed creating ttrpc server: %w", err)
	}

	// Register a service and method with the server.
	server.Register("ttrpc.stress.test.v1", map[string]ttrpc.Method{
		"TEST": func(_ context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			req := &payload.Payload{}
			// Unmarshal the request payload.
			if err := unmarshal(req); err != nil {
				log.Fatalf("failed unmarshalling request: %s", err)
			}
			id := req.Value
			log.Printf("got request: %d", id)
			// Return the same payload as the response.
			return &payload.Payload{Value: id}, nil
		},
	})

	// Serve the server and handle any errors.
	if err := server.Serve(ctx, l); err != nil {
		return fmt.Errorf("failed serving server: %w", err)
	}
	return nil
}

// runClient sets up and runs the client.
func runClient(ctx context.Context, addr string, iters int, workers int) error {
	log.Printf("Starting client on %s", addr)

	// Dial a connection to the specified pipe.
	c, err := dialConnection(addr)
	if err != nil {
		return fmt.Errorf("failed dialing connection to %s: %w", addr, err)
	}

	// Create a new ttrpc client.
	client := ttrpc.NewClient(c)
	ch := make(chan int)
	var eg errgroup.Group

	// Start worker goroutines to send requests.
	for i := 0; i < workers; i++ {
		eg.Go(func() error {
			for {
				i, ok := <-ch
				if !ok {
					return nil
				}
				// Send the request and handle any errors.
				if err := send(ctx, client, uint32(i)); err != nil {
					return fmt.Errorf("failed sending request: %w", err)
				}
			}
		})
	}

	// Send iterations to the channel.
	for i := 0; i < iters; i++ {
		ch <- i
	}
	close(ch)

	// Wait for all goroutines to finish.
	if err := eg.Wait(); err != nil {
		return fmt.Errorf("failed waiting for goroutines: %w", err)
	}
	return nil
}

// send sends a request to the server and verifies the response.
func send(ctx context.Context, client *ttrpc.Client, id uint32) error {
	req := &payload.Payload{Value: id}
	resp := &payload.Payload{}

	log.Printf("sending request: %d", id)
	// Call the server method and handle any errors.
	if err := client.Call(ctx, "ttrpc.stress.test.v1", "TEST", req, resp); err != nil {
		return err
	}

	ret := resp.Value
	log.Printf("got response: %d", ret)
	// Verify the response matches the request.
	if ret != id {
		return fmt.Errorf("expected return value %d but got %d", id, ret)
	}
	return nil
}
