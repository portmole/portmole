# portmole

Expose a local port to the internet from your coding agent.

portmole is a dev tunnel built for MCP clients such as Claude Code, Codex and
Cursor. You ask the agent to open port 3000, it gives you back an `https://`
address that reaches your machine. The same thing works from a terminal.

This repository holds the client: the tunnel protocol, the agent that forwards
traffic to your local port, and the command line. The hosted service that the
client connects to is not part of this repository.

## Status

Early development. The tunnel, sign-in and the MCP server work. The hosted
service is not open yet, and the npm packages and downloads below arrive with
the first tagged release.

## Installing

With Node 18 or newer, nothing needs installing to use it from an agent:

```
npx -y portmole mcp
```

To have the `portmole` command in your terminal:

```
npm install -g portmole
```

npm picks the program built for your machine: Windows, macOS or Linux, x64 or
arm64. No install script runs.

Without Node, download the archive for your platform from the
[releases page](https://github.com/portmole/portmole/releases), check it
against `SHA256SUMS`, and put `portmole` somewhere on your `PATH`.

From source, with Go 1.22 or newer:

```
go build ./cmd/portmole
```

## Usage

```
portmole http <port> [name]   expose localhost:<port>
portmole mcp                  serve the tools to a coding agent over stdio
portmole login                sign this machine in
portmole logout               sign out and revoke the token
portmole version              print the version
```

The first `portmole http` signs you in if needed: it prints a link, you
approve in the browser with your GitHub account, and the token is saved in
your user config directory. Any port works, and each tunnel gets its own
address, so a frontend and an API can be exposed side by side.

While `portmole http` runs it prints each request as it arrives (time, method,
status, duration, path), so you can watch a webhook come in. `tunnel_list`
and the terminal both show how much of the day's data allowance is used.

Without a name, the tunnel is named after the folder you run it in, so
`~/code/My Shop` is served at `my-shop-<you>.portmole.dev` every time.

`PORTMOLE_SERVER` points the client at another server. `PORTMOLE_TOKEN`
supplies a token directly, for CI.

## Using it from a coding agent

`portmole mcp` is an MCP server over stdio. In Claude Code:

```
claude mcp add portmole -- npx -y portmole mcp
```

If you installed the command, `portmole mcp` in place of `npx -y portmole mcp`
starts a little faster.

Other clients take the same command in their MCP settings. The agent then has
four tools:

| Tool | What it does |
|---|---|
| `tunnel_create` | Exposes a port and returns the address. Takes `port`, and optionally `name`, `basic_auth` (`user:password`) and `allow_bots`. |
| `tunnel_list` | Lists the tunnels open in this session. |
| `tunnel_close` | Closes one. |
| `tunnel_requests` | Shows the latest requests that reached a tunnel, which helps when a webhook does not do what you expect. |

By default a tunnel admits AI assistants fetching a page for a person and
turns search and AI crawlers away with a 403, a `noindex` header and its own
`robots.txt`. `allow_bots` (or `--bots=` in the terminal) takes a list of
`search`, `ai_crawlers` and `ai_assistants`, or `all`, or `none`.

If you are not signed in, `tunnel_create` returns the sign-in link for the
agent to show you. A second port of the same project gets the port in its
name (`my-shop-8000-<you>`). Tunnels close when the agent session ends.

## How it works

The client opens one outbound connection to the server and multiplexes it with
yamux. Each request arriving at your public address becomes a stream that the
client pipes to `localhost:<port>`. Nothing listens on your machine and no
elevated permissions are needed. See [proto/proto.go](proto/proto.go) for the
wire format.

## Releases

A release is a tag: pushing `v1.2.3` makes the
[release workflow](.github/workflows/release.yml) build the six programs,
publish them with their checksums on the releases page, and publish the same
version to npm. `portmole version` prints the version a program was built
from, or `dev` for a build from source.

Bugs and ideas go to the [issue tracker](https://github.com/portmole/portmole/issues);
see [CONTRIBUTING.md](CONTRIBUTING.md). Security reports go to
security@portmole.com, as [SECURITY.md](SECURITY.md) describes.

## License

Apache-2.0
