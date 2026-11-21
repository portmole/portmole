// Command portmole exposes a local port through a portmole edge.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/portmole/portmole/agent"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "http" {
		fmt.Fprintln(os.Stderr, "usage: portmole http <port> [name]")
		os.Exit(2)
	}
	port, err := strconv.Atoi(os.Args[2])
	if err != nil || port < 1 || port > 65535 {
		fatal(fmt.Errorf("invalid port %q", os.Args[2]))
	}
	name := "app"
	if len(os.Args) > 3 {
		name = os.Args[3]
	}
	server := os.Getenv("PORTMOLE_SERVER")
	if server == "" {
		server = "https://portmole.dev"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a, err := agent.Dial(ctx, server, os.Getenv("PORTMOLE_TOKEN"))
	if err != nil {
		fatal(err)
	}
	defer a.Close()
	url, err := a.Open(name, port)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s -> localhost:%d\n", url, port)

	select {
	case <-ctx.Done():
	case <-a.Done():
		fatal(fmt.Errorf("connection to %s lost", server))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
