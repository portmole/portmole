# portmole

Expose a local port to the internet from your coding agent.

portmole is a dev tunnel built for MCP clients such as Claude Code, Codex and
Cursor. You ask the agent to open port 3000, it gives you back an `https://`
address that reaches your machine. The same thing works from a terminal.

This repository holds the client: the tunnel protocol, the agent that forwards
traffic to your local port, and the command line. The hosted service that the
client connects to is not part of this repository.

## Status

Early development. The tunnel and sign-in work; the MCP server and published
binaries are not there yet, and the hosted service is not open.

## Building

Needs Go 1.22 or newer.

```
go build ./cmd/portmole
```

## Usage

```
portmole http <port> [name]   expose localhost:<port>
portmole login                sign this machine in
portmole logout               sign out and revoke the token
```

The first `portmole http` signs you in if needed: it prints a link, you
approve in the browser with your GitHub account, and the token is saved in
your user config directory. Any port works, and each tunnel gets its own
address, so a frontend and an API can be exposed side by side.

`PORTMOLE_SERVER` points the client at another server. `PORTMOLE_TOKEN`
supplies a token directly, for CI.

## How it works

The client opens one outbound connection to the server and multiplexes it with
yamux. Each request arriving at your public address becomes a stream that the
client pipes to `localhost:<port>`. Nothing listens on your machine and no
elevated permissions are needed. See [proto/proto.go](proto/proto.go) for the
wire format.

## License

Apache-2.0
