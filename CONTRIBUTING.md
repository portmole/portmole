# Contributing

This repository is the portmole client. The hosted service it connects to is
developed elsewhere and is not open source, so changes that need the server
to change as well should start as an issue.

## Building and testing

Needs Go 1.22 or newer.

```
go build ./cmd/portmole
go test ./...
```

Before you send a pull request:

```
gofmt -l .        # prints nothing when the formatting is right
go vet ./...
go test ./...
```

CI runs the same three on Linux, Windows and macOS.

## Pull requests

- Keep a pull request to one change, with a test for new behaviour.
- The wire format in `proto/` is shared with the service. Open an issue
  before changing it.
- No new dependencies for something the standard library can do.

## Filing a bug

Use the bug form at https://github.com/portmole/portmole/issues/new/choose.
The output of `portmole version`, your operating system, and whether you used
the MCP server or the terminal command are what we ask about first.

Security problems go to security@portmole.com, not to an issue; see
[SECURITY.md](SECURITY.md). A tunnel being used for abuse is reported at
https://portmole.com/abuse.html.
