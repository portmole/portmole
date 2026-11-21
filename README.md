# portmole

Expose a local port to the internet from your coding agent.

portmole is a dev tunnel built for MCP clients such as Claude Code, Codex and
Cursor. You ask the agent to open port 3000, it gives you back an `https://`
address that reaches your machine. The same thing works from a terminal.

This repository holds the client: the tunnel protocol, the agent that forwards
traffic to your local port, and the command line. The hosted service that the
client connects to is not part of this repository.

## Status

Early development. The tunnel itself works; sign-in, the MCP server and
published binaries are not there yet.

## Building

Needs Go 1.22 or newer.

```
go build ./cmd/portmole
```

## Usage

```
portmole http 3000 [name]
```

`PORTMOLE_SERVER` and `PORTMOLE_TOKEN` select the server and credentials.

## How it works

The client opens one outbound connection to the server and multiplexes it with
yamux. Each request arriving at your public address becomes a stream that the
client pipes to `localhost:<port>`. Nothing listens on your machine and no
elevated permissions are needed. See [proto/proto.go](proto/proto.go) for the
wire format.

## License

Apache-2.0
