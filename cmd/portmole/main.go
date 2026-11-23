// Command portmole exposes a local port through a portmole server.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/internal/auth"
	"github.com/portmole/portmole/internal/name"
	"github.com/portmole/portmole/mcp"
	"github.com/portmole/portmole/proto"
)

const usage = `portmole gives a port on this machine a public https address.

usage:
  portmole http <port> [name]   expose localhost:<port>; runs until Ctrl+C
      --bots=<list>             automated visitors to admit: search, ai_crawlers,
                                ai_assistants (the default), all or none
  portmole console              open, rename and close tunnels from a page on
                                this machine, and watch their requests
  portmole mcp                  serve the tools to a coding agent over stdio
  portmole setup [agent ...]    add portmole to the coding agents on this
                                machine: claude, claude-desktop, codex, cursor,
                                windsurf, gemini; with no name, the ones found
  portmole doctor               check the program, this machine, the agents
                                and the sign-in, and say what to fix
  portmole update               look for a newer version and install it
  portmole login                sign this machine in
  portmole logout               sign out and revoke the token
  portmole status               show whether this machine is signed in and
                                what the console has open
  portmole version              print the version
  portmole help                 print this list

examples:
  portmole http 3000            https://<folder>-<you>.portmole.dev
  portmole http 3000 demo       https://demo-<you>.portmole.dev

docs: https://portmole.com/docs`

func main() {
	agent.Version = mcp.Version
	server := strings.TrimRight(os.Getenv("PORTMOLE_SERVER"), "/")
	if server == "" {
		server = "https://portmole.dev"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The one flag there is; everything else is positional.
	bots := ""
	args := os.Args[:1]
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "--bots="); ok {
			bots = v
		} else {
			args = append(args, a)
		}
	}
	os.Args = args

	var err error
	switch {
	case len(os.Args) == 1 || os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help":
		fmt.Println(usage)
	case len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version"):
		fmt.Println("portmole " + mcp.Version)
	case len(os.Args) == 2 && os.Args[1] == "login":
		_, err = login(ctx, server)
	case len(os.Args) == 2 && os.Args[1] == "logout":
		if err = auth.Logout(ctx, server); err == nil {
			fmt.Println("Signed out.")
		}
	case os.Args[1] == "setup":
		err = setup(ctx, os.Stdin, os.Stdout, os.Args[2:])
	case len(os.Args) == 2 && os.Args[1] == "doctor":
		if doctor(ctx, os.Stdout, server) > 0 {
			os.Exit(1)
		}
	case os.Args[1] == "update" && (len(os.Args) == 2 || len(os.Args) == 3 && os.Args[2] == "--check"):
		var exit int
		if exit, err = update(ctx, os.Stdout, latestURL, len(os.Args) == 3); err == nil && exit != 0 {
			os.Exit(exit)
		}
	case len(os.Args) == 2 && os.Args[1] == "status":
		status(ctx, os.Stdout, server, consoleURL)
	case len(os.Args) == 2 && os.Args[1] == "console":
		err = runConsole(ctx, server)
	case len(os.Args) == 2 && os.Args[1] == "mcp":
		err = mcp.Serve(ctx, os.Stdin, os.Stdout, server)
	case len(os.Args) >= 3 && len(os.Args) <= 4 && os.Args[1] == "http":
		err = serve(ctx, server, os.Args[2:], bots)
	default:
		fmt.Fprintf(os.Stderr, "portmole: cannot run %q as given\n\n%s\n", strings.Join(os.Args[1:], " "), usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func login(ctx context.Context, server string) (auth.Credentials, error) {
	d, err := auth.Start(ctx, server)
	if err != nil {
		return auth.Credentials{}, err
	}
	fmt.Printf("Open this link to sign in:\n\n  %s\n\nCheck that the page shows the code %s, then approve.\n", d.URL, d.UserCode)
	c, err := auth.Wait(ctx, server, d)
	if err != nil {
		return c, err
	}
	fmt.Printf("Signed in as %s.\n", c.Handle)
	return c, nil
}

// dial connects to the server with this machine's token, signing in first
// when there is none or the stored one was revoked.
func dial(ctx context.Context, server string) (*agent.Agent, error) {
	// PORTMOLE_TOKEN wins so CI and tests need no credentials file.
	token := os.Getenv("PORTMOLE_TOKEN")
	if token == "" {
		c, ok := auth.Load(server)
		if !ok {
			var err error
			if c, err = login(ctx, server); err != nil {
				return nil, err
			}
		}
		token = c.Token
	}
	a, err := agent.Dial(ctx, server, token)
	if errors.Is(err, agent.ErrUnauthorized) && os.Getenv("PORTMOLE_TOKEN") == "" {
		// The stored token was revoked: sign in again and retry once.
		auth.Logout(ctx, server)
		var c auth.Credentials
		if c, err = login(ctx, server); err == nil {
			a, err = agent.Dial(ctx, server, c.Token)
		}
	}
	return a, err
}

func serve(ctx context.Context, server string, args []string, bots string) error {
	port, err := strconv.Atoi(args[0])
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %q", args[0])
	}
	wd, _ := os.Getwd()
	n := name.FromDir(wd)
	if len(args) > 1 {
		if n = name.Clean(args[1]); n == "" {
			return fmt.Errorf("invalid name %q", args[1])
		}
	}

	a, err := dial(ctx, server)
	if err != nil {
		return err
	}
	defer a.Close()
	url, err := a.OpenWith(n, port, agent.Options{Bots: bots})
	if err != nil {
		return err
	}
	fmt.Printf("%s -> localhost:%d\n", url, port)
	if used, allowance, err := a.Usage(); err == nil && allowance > 0 {
		fmt.Printf("Data used today: %s of %s\n", mcp.Size(used), mcp.Size(allowance))
	}
	fmt.Print("\nRequests appear below. Press Ctrl+C to close the tunnel.\n\n")

	// shortcut: asks the server for its request log once a second, so a burst
	// of more than the 20 it keeps is shown in part; stream them if that matters.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var shown time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.Done():
			return fmt.Errorf("the session with %s ended (its time limit was reached or the token was revoked); run the command again", server)
		case <-tick.C:
			logs, err := a.Requests(n, 0)
			if err != nil {
				continue // reconnecting; the next tick tries again
			}
			for _, l := range logs {
				if l.Time.After(shown) {
					shown = l.Time
					fmt.Println(requestLine(l))
				}
			}
		}
	}
}

// requestLine formats one request for the terminal. The path comes from a
// stranger, so anything that could drive the terminal is shown as "?".
func requestLine(l proto.RequestLog) string {
	path := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return '?'
		}
		return r
	}, l.Path)
	return fmt.Sprintf("%s  %-7s %3d  %5d ms  %s", l.Time.Local().Format("15:04:05"), l.Method, l.Status, l.Millis, path)
}
