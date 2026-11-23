# portmole

A public `https://` address for an app on localhost, opened by your coding
agent or from the terminal.

portmole is a dev tunnel built for MCP clients such as Claude Code, Codex and
Cursor. You tell the agent "open port 3000", and it hands back an address
that reaches your machine. A tunnel shows unfinished work, so it is private
by default: search engines and crawlers are turned away, and you can put a
password on it in the same sentence.

```
https://my-shop-ada.portmole.dev  ->  localhost:3000
```

The name defaults to the project folder. The last part is your GitHub name.

## Use it from a coding agent

Nothing to install; Node 18 or newer is enough.

Claude Code:

```
claude mcp add portmole -- npx -y portmole mcp
```

Claude Code on Windows, where `npx` has to go through `cmd`:

```
claude mcp add portmole -- cmd /c npx -y portmole mcp
```

Codex:

```
codex mcp add portmole -- npx -y portmole mcp
```

Cursor, Windsurf, Claude Desktop and other tools that take a JSON entry:

```json
{
  "mcpServers": {
    "portmole": { "command": "npx", "args": ["-y", "portmole", "mcp"] }
  }
}
```

Start a new agent session, then ask in plain words:

- "Open port 3000 with portmole."
- "Open port 3000 as `demo` and put a password on it."
- "Open the frontend on 5173 and the API on 8080."
- "Show me the last requests that reached the tunnel." (handy for webhooks)
- "Let search engines in on that tunnel."
- "Close the tunnel."

The first time, the agent shows a sign-in link. You approve with your GitHub
account, confirm the code on portmole.dev, and tell the agent you are done.
There is no password to create and no key to copy.

## Use it from a terminal

```
npm install -g portmole

portmole http 3000            https://<folder>-<you>.portmole.dev
portmole http 3000 demo       https://demo-<you>.portmole.dev
portmole http 8080 api --bots=none
```

The command prints each request as it arrives (time, method, status,
duration, path) and runs until Ctrl+C.

| Command | What it does |
| --- | --- |
| `portmole http <port> [name]` | Open one tunnel until Ctrl+C |
| `portmole console` | Open, rename and close tunnels from a page on this machine |
| `portmole mcp` | Serve the tools to a coding agent over stdio |
| `portmole login` / `logout` | Sign this machine in, or sign out and revoke its token |
| `portmole status` | Show whether this machine is signed in and what the console has open |
| `portmole version` / `help` | Print the version, or the list of commands |

## The MCP tools

| Tool | What it does | Arguments |
| --- | --- | --- |
| `tunnel_create` | Give a local port a public address | `port`; optional `name`, `basic_auth` (`user:password`), `allow_bots` |
| `tunnel_list` | List the tunnels open in this session, with the day's data use | none |
| `tunnel_close` | Close a tunnel | `name` |
| `tunnel_requests` | Show the latest requests that reached a tunnel | `name`; optional `limit` |

Calling `tunnel_create` again for a port that is already open returns the
same address and applies a changed password or bot setting.

## Private by default

A development site should not end up in a search index or a training set
because it was online for an afternoon. So:

**Crawlers are turned away.** Automated visitors are sorted into three kinds,
and you choose which get in with `allow_bots` (or `--bots=` in the terminal):

| Kind | Who | Default |
| --- | --- | --- |
| `ai_assistants` | An AI assistant fetching a page because a person asked it to | let in |
| `ai_crawlers` | Crawlers collecting pages for AI models and AI search: GPTBot, ClaudeBot, PerplexityBot, CCBot and others | turned away |
| `search` | Search engine and SEO crawlers: Googlebot, Bingbot, YandexBot and others | turned away |

Give a comma-separated list, or `all`, or `none`. The default,
`ai_assistants`, means you can paste the address into a chat and ask an
assistant to look at your page, while nothing harvests it. A bot that is
turned away gets a 403 and still shows in the request log, so you can see who
came.

**Nothing is indexed.** Unless you admit `search`, every answer carries
`X-Robots-Tag: noindex, nofollow`, and the tunnel answers `/robots.txt`
itself instead of your app.

**A password takes one sentence.** With `basic_auth`, visitors get the
browser's own password prompt before anything reaches your app. portmole
checks the password and removes the header, so your app never sees it. Ten
wrong answers lock that visitor out of the tunnel for a minute.

**Visitors are told where they are.** A browser sees a notice before its
first visit to a tunnel, saying that the page is served from someone's
computer. `curl`, API clients and webhooks are not shown it.

**The request log leaves the secrets out.** portmole keeps the last 20
requests of each tunnel so an agent can debug with them: time, method, path,
status, duration and headers. `Authorization`, `Cookie` and
`Proxy-Authorization` headers are never recorded, and bodies are not
recorded at all.

**The agent is told not to trust what arrives.** Everything in
`tunnel_requests` was written by strangers on the internet. The result says
so on its first line, so the agent reads it as data and not as instructions.

Bots are recognised by the name they give. That stops the ones that play
fair; against a program that lies about who it is, use a password. Addresses
are also easy to guess, so anything that must stay private needs one.

## Limits of the free plan

- 2 tunnels at a time
- 120 requests a minute per visitor and tunnel
- 5 GB of data a day
- sessions of 120 minutes

A tunnel is for development and demos, not for hosting a production site.
It carries http and WebSocket; it does not carry a database port or SSH.

## About this package

npm picks the program built for your machine: Windows, macOS or Linux, x64
or arm64. No install script runs. The client is open source under
Apache-2.0; the service at portmole.dev is run by the operator.

- Docs: https://portmole.com/docs
- Help: https://portmole.com/help
- Source: https://github.com/portmole/portmole
- Report abuse of a tunnel: abuse@portmole.com
