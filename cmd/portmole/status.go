package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/internal/auth"
	"github.com/portmole/portmole/mcp"
)

// consoleURL is where `portmole console` listens unless 4040 was taken.
const consoleURL = "http://127.0.0.1:4040"

// status tells whether this machine is signed in, whether the server takes
// the sign-in, and what the console on this machine has open.
func status(ctx context.Context, w io.Writer, server, console string) {
	// The agent lives as long as its context, so the limit covers the whole check.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	fmt.Fprintf(w, "portmole %s\nServer:    %s\n", mcp.Version, server)

	token, who := os.Getenv("PORTMOLE_TOKEN"), "with PORTMOLE_TOKEN"
	if token == "" {
		c, ok := auth.Load(server)
		token, who = c.Token, "as "+c.Handle
		if !ok {
			token = ""
		}
	}
	switch a, err := dialToken(ctx, server, token); {
	case token == "":
		fmt.Fprintln(w, "Signed in: no; run: portmole login")
	case errors.Is(err, agent.ErrUnauthorized):
		fmt.Fprintln(w, "Signed in: no, the server no longer accepts the stored sign-in; run: portmole login")
	case errors.Is(err, agent.ErrClosed):
		fmt.Fprintf(w, "Signed in: %s, but the account is closed; write to hello@portmole.com\n", who)
	case err != nil:
		fmt.Fprintf(w, "Signed in: %s (not checked: the server was not reached: %v)\n", who, err)
	default:
		fmt.Fprintf(w, "Signed in: %s\n", who)
		if used, allowance, err := a.Usage(); err == nil && allowance > 0 {
			fmt.Fprintf(w, "Data used today: %s of %s\n", mcp.Size(used), mcp.Size(allowance))
		}
		a.Close()
	}

	// shortcut: only the console's tunnels are listed, and only when it has
	// port 4040. Tunnels of `portmole http` and of an agent live in their own
	// process; list them from the server when accounts need to see them all.
	var state struct {
		Tunnels []consoleTunnel `json:"tunnels"`
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(console + "/api/state")
	if err == nil {
		defer resp.Body.Close()
		// Some other program can have the port; only our own answer counts.
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&state) != nil || state.Tunnels == nil {
			err = errors.New("not the console")
		}
	}
	if err != nil {
		fmt.Fprintln(w, "Console:   not running; start it with: portmole console")
	} else {
		fmt.Fprintf(w, "Console:   %s, %d tunnel(s) open\n", console, len(state.Tunnels))
		for _, t := range state.Tunnels {
			fmt.Fprintf(w, "  %s -> localhost:%d\n", t.URL, t.Port)
		}
	}
	fmt.Fprintln(w, "\nA tunnel opened with `portmole http` shows in its own terminal, one opened by an agent in its tunnel_list.")
}

// dialToken is agent.Dial, skipped when there is no token to try.
func dialToken(ctx context.Context, server, token string) (*agent.Agent, error) {
	if token == "" {
		return nil, nil
	}
	return agent.Dial(ctx, server, token)
}
