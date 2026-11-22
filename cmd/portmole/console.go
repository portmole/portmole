package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/internal/name"
	"github.com/portmole/portmole/proto"
)

//go:embed console.html
var consolePage []byte

// tunneler is the part of the agent the console uses.
type tunneler interface {
	OpenWith(name string, port int, o agent.Options) (string, error)
	CloseTunnel(name string) error
	Requests(name string, limit int) ([]proto.RequestLog, error)
	Usage() (used, allowance int64, err error)
}

type consoleTunnel struct {
	Name     string             `json:"name"`
	Port     int                `json:"port"`
	URL      string             `json:"url"`
	Requests []proto.RequestLog `json:"requests"`
}

// console is a page on this machine that opens, renames and closes tunnels
// and shows what came through them. It owns its tunnels: they live as long
// as the command runs.
type console struct {
	a    tunneler
	dir  string // names a tunnel opened without a name
	host string // "127.0.0.1:4040"; requests for any other host are refused

	mu      sync.Mutex
	tunnels map[string]consoleTunnel
}

func (c *console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A page on another site can make the browser send requests here, and
	// opening a tunnel puts a local port on the internet. So: only our own
	// host name (stops DNS rebinding), and changes only from our own page.
	_, port, _ := net.SplitHostPort(c.host)
	if r.Host != c.host && r.Host != "localhost:"+port {
		http.Error(w, "wrong host", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && (r.Method != http.MethodPost || r.Header.Get("Origin") != "http://"+r.Host || r.Header.Get("Content-Type") != "application/json") {
		http.Error(w, "not from the console page", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	var in struct {
		Name string `json:"name"`
		To   string `json:"to"`
		Port int    `json:"port"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	var err error
	switch r.Method + " " + r.URL.Path {
	case "GET /":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(consolePage)
		return
	case "GET /api/state":
	case "POST /api/open":
		err = c.open(in.Name, in.Port)
	case "POST /api/close":
		err = c.close(in.Name)
	case "POST /api/rename":
		err = c.rename(in.Name, in.To)
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(c.state())
}

func (c *console) open(n string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	if n == "" {
		n = name.FromDir(c.dir)
	} else if n = name.Clean(n); n == "" {
		return errors.New("invalid name: use letters, digits and dashes")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	url, err := c.a.OpenWith(n, port, agent.Options{})
	if err != nil {
		return err
	}
	c.tunnels[n] = consoleTunnel{Name: n, Port: port, URL: url}
	return nil
}

func (c *console) close(n string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tunnels[n]; !ok {
		return fmt.Errorf("no open tunnel named %q", n)
	}
	delete(c.tunnels, n)
	return c.a.CloseTunnel(n)
}

// rename gives a tunnel a new address. The old one stops working: an address
// is its name, so there is nothing to keep.
func (c *console) rename(from, to string) error {
	if to = name.Clean(to); to == "" {
		return errors.New("invalid name: use letters, digits and dashes")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tunnels[from]
	if !ok {
		return fmt.Errorf("no open tunnel named %q", from)
	}
	if to == from {
		return nil
	}
	if _, taken := c.tunnels[to]; taken {
		return fmt.Errorf("a tunnel named %q is already open", to)
	}
	// Closed first: opening the new one first would fail at the tunnel limit.
	if err := c.a.CloseTunnel(from); err != nil {
		return err
	}
	delete(c.tunnels, from)
	url, err := c.a.OpenWith(to, t.Port, agent.Options{})
	if err != nil {
		// Put the old address back rather than leave the port closed.
		if old, backErr := c.a.OpenWith(from, t.Port, agent.Options{}); backErr == nil {
			t.URL = old
			c.tunnels[from] = t
		}
		return err
	}
	c.tunnels[to] = consoleTunnel{Name: to, Port: t.Port, URL: url}
	return nil
}

func (c *console) state() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := make([]consoleTunnel, 0, len(c.tunnels))
	for _, t := range c.tunnels {
		t.Requests, _ = c.a.Requests(t.Name, 0) // empty while reconnecting
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	used, allowance, _ := c.a.Usage()
	return map[string]any{"tunnels": list, "used": used, "allowance": allowance}
}

// runConsole serves the console until ctx ends or the session does.
func runConsole(ctx context.Context, server string) error {
	a, err := dial(ctx, server)
	if err != nil {
		return err
	}
	defer a.Close()

	// 4040 so the address is the same every time; any free port if it is taken.
	ln, err := net.Listen("tcp", "127.0.0.1:4040")
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	wd, _ := os.Getwd()
	c := &console{a: a, dir: wd, host: ln.Addr().String(), tunnels: map[string]consoleTunnel{}}
	srv := &http.Server{Handler: c, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	fmt.Printf("Console: http://%s\n\nTunnels opened there stay open while this command runs. Press Ctrl+C to close them.\n", c.host)
	select {
	case <-ctx.Done():
		return nil
	case <-a.Done():
		return fmt.Errorf("the session with %s ended (its time limit was reached or the token was revoked); run the command again", server)
	}
}
