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

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/internal/auth"
	"github.com/portmole/portmole/internal/name"
	"github.com/portmole/portmole/mcp"
)

const usage = `usage:
  portmole http <port> [name]   expose localhost:<port>
  portmole mcp                  serve the tools to a coding agent over stdio
  portmole login                sign this machine in
  portmole logout               sign out and revoke the token
  portmole version              print the version`

func main() {
	server := strings.TrimRight(os.Getenv("PORTMOLE_SERVER"), "/")
	if server == "" {
		server = "https://portmole.dev"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch {
	case len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version"):
		fmt.Println("portmole " + mcp.Version)
	case len(os.Args) == 2 && os.Args[1] == "login":
		_, err = login(ctx, server)
	case len(os.Args) == 2 && os.Args[1] == "logout":
		if err = auth.Logout(ctx, server); err == nil {
			fmt.Println("Signed out.")
		}
	case len(os.Args) == 2 && os.Args[1] == "mcp":
		err = mcp.Serve(ctx, os.Stdin, os.Stdout, server)
	case len(os.Args) >= 3 && len(os.Args) <= 4 && os.Args[1] == "http":
		err = serve(ctx, server, os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, usage)
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

func serve(ctx context.Context, server string, args []string) error {
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

	// PORTMOLE_TOKEN wins so CI and tests need no credentials file.
	token := os.Getenv("PORTMOLE_TOKEN")
	if token == "" {
		c, ok := auth.Load(server)
		if !ok {
			if c, err = login(ctx, server); err != nil {
				return err
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
	if err != nil {
		return err
	}
	defer a.Close()
	url, err := a.Open(n, port, "")
	if err != nil {
		return err
	}
	fmt.Printf("%s -> localhost:%d\n", url, port)

	select {
	case <-ctx.Done():
		return nil
	case <-a.Done():
		return fmt.Errorf("the session with %s ended (its time limit was reached or the token was revoked); run the command again", server)
	}
}
