# portmole

Expose a local port to the internet from your coding agent.

portmole is a dev tunnel built for MCP clients such as Claude Code, Codex and
Cursor. You ask the agent to open port 3000, it gives you back an `https://`
address that reaches your machine. The same thing works from a terminal.

This repository holds the client: the tunnel protocol, the agent that forwards
traffic to your local port, and the command line. The hosted service that the
client connects to is not part of this repository.

## Status

Early development. The tunnel, sign-in and the MCP server work; published
binaries are not there yet, and the hosted service is not open.

## Building

Needs Go 1.22 or newer.

```
go build ./cmd/portmole
```

## Usage

```
portmole http <port> [name]   expose localhost:<port>
portmole mcp                  serve the tools to a coding agent over stdio
portmole login                sign this machine in
portmole logout               sign out and revoke the token
```

The first `portmole http` signs you in if needed: it prints a link, you
approve in the browser with your GitHub account, and the token is saved in
your user config directory. Any port works, and each tunnel gets its own
address, so a frontend and an API can be exposed side by side.

Without a name, the tunnel is named after the folder you run it in, so
`~/code/My Shop` is served at `my-shop-<you>.portmole.dev` every time.

`PORTMOLE_SERVER` points the client at another server. `PORTMOLE_TOKEN`
supplies a token directly, for CI.

## Using it from a coding agent

`portmole mcp` is an MCP server over stdio. In Claude Code:

```
claude mcp add portmole -- portmole mcp
```

Other clients take the same command in their MCP settings. The agent then has
four tools:

| Tool | What it does |
|---|---|
| `tunnel_create` | Exposes a port and returns the address. Takes `port`, and optionally `name` and `basic_auth` (`user:password`). |
| `tunnel_list` | Lists the tunnels open in this session. |
| `tunnel_close` | Closes one. |
| `tunnel_requests` | Shows the latest requests that reached a tunnel, which helps when a webhook does not do what you expect. |

If you are not signed in, `tunnel_create` returns the sign-in link for the
agent to show you. A second port of the same project gets the port in its
name (`my-shop-8000-<you>`). Tunnels close when the agent session ends.

## How it works

The client opens one outbound connection to the server and multiplexes it with
yamux. Each request arriving at your public address becomes a stream that the
client pipes to `localhost:<port>`. Nothing listens on your machine and no
elevated permissions are needed. See [proto/proto.go](proto/proto.go) for the
wire format.

## License

Apache-2.0
