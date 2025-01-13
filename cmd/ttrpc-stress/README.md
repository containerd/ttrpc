# ttrpc-stress

This is a simple client/server utility designed to stress-test a TTRPC connection. It aims to identify potential deadlock cases during repeated, rapid TTRPC requests.

## Overview of the tool

- The **server** listens for connections and exposes a single, straightforward method. It responds immediately to any requests.
- The **client** creates multiple worker goroutines that send a large number of requests to the server as quickly as possible.

This tool represents a simple client-server interaction where the client sends requests to the server, and the server responds with the same data, allowing for testing of concurrent request handling and response verification. By utilizing core TTRPC facilities like `ttrpc.(*Client).Call` and `ttrpc.(*Server).Register` instead of generated client/server code, the test remains straightforward and effective.

## Usage

The `ttrpc-stress` command provides two modes of operation: server and client.

### Run the Server

To start the server, specify a Unix socket or named pipe as the address:
```bash
ttrpc-stress server <ADDR>
```
- `<ADDR>`: The Unix socket or named pipe to listen on.

### Run the Client

To start the client, specify the address, number of iterations, and number of workers:
```bash
ttrpc-stress client <ADDR> <ITERATIONS> <WORKERS>
```
- `<ADDR>`: The Unix socket or named pipe to connect to.
- `<ITERATIONS>`: The total number of iterations to execute.
- `<WORKERS>`: The number of workers handling the iterations.

## Version Compatibility Testing

One of the primary motivations for developing this stress utility was to identify potential deadlock scenarios when using different versions of the server and client. The goal is to test the current version of TTRPC, which is used to build `ttrpc-stress`, against the following older versions in both server and client scenarios:

- `v1.0.2`
- `v1.1.0`
- `v1.2.0`
- `v1.2.4`
- `latest`

### Known Issues in TTRPC Versions

| Version Range       | Description                              |  Comments                                                 |
|---------------------|-------------------------------------------|------------------------------------------------------------------|
| **v1.0.2 and before**   | Original deadlock bug                                   | [#94](https://github.com/containerd/ttrpc/pull/94) for fixing deadlock in `v1.1.0`                                         |
| **v1.1.0 - v1.2.0** | No known deadlock bugs                                  |  |
| **v1.2.0 - v1.2.4** | Streaming with a new deadlock bug                       | [#107](https://github.com/containerd/ttrpc/pull/107) introduced deadlock in `v1.2.0` |
| **After v1.2.4**    | No known deadlock bugs                                  | [#168](https://github.com/containerd/ttrpc/pull/168) for fixing deadlock in `v1.2.4` |
---

Clients before `v1.1.0` and between `v1.2.0`-`v1.2.3` can encounted the deadlock.

However, if the **server** version is `v1.2.0` or later, deadlock issues in the client may be avoided.

Please refer to https://github.com/containerd/ttrpc/issues/72 for more information about the deadlock bug.

---

Happy testing!
