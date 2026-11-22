// Package mcp serves the portmole tools to a coding agent over stdio, as a
// Model Context Protocol server: JSON-RPC 2.0, one message per line.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/internal/auth"
	"github.com/portmole/portmole/internal/name"
	"github.com/portmole/portmole/proto"
)

const tools = `[
{"name":"tunnel_create","description":"Expose a local port to the internet and return its public URL. Call it once per port; a frontend and an API each get their own tunnel. If the user is not signed in, the result is a sign-in link to show them instead.","inputSchema":{"type":"object","properties":{"port":{"type":"integer","description":"Local port the app listens on."},"name":{"type":"string","description":"Name used in the address. Defaults to the project folder name."},"basic_auth":{"type":"string","description":"Optional user:password that visitors must enter."}},"required":["port"]}},
{"name":"tunnel_list","description":"List the tunnels open in this session.","inputSchema":{"type":"object","properties":{}}},
{"name":"tunnel_close","description":"Close a tunnel.","inputSchema":{"type":"object","properties":{"name":{"type":"string","description":"Tunnel name as reported by tunnel_create or tunnel_list."}},"required":["name"]}},
{"name":"tunnel_requests","description":"Show the most recent requests that reached a tunnel: time, method, path, status, duration and headers. Useful for debugging webhooks.","inputSchema":{"type":"object","properties":{"name":{"type":"string","description":"Tunnel name."},"limit":{"type":"integer","description":"How many requests to return, newest last."}},"required":["name"]}}
]`

type open struct {
	port int
	url  string
}

type server struct {
	ctx     context.Context
	url     string
	agent   *agent.Agent
	tunnels map[string]open

	mu      sync.Mutex
	pending *auth.Device // sign-in waiting for the user, polled in the background
}

type args struct {
	Port      int    `json:"port"`
	Name      string `json:"name"`
	BasicAuth string `json:"basic_auth"`
	Limit     int    `json:"limit"`
}

// Serve answers MCP requests from in on out until in is closed. Tunnels live
// as long as the call does.
func Serve(ctx context.Context, in io.Reader, out io.Writer, serverURL string) error {
	s := &server{ctx: ctx, url: serverURL, tunnels: map[string]open{}}
	defer func() {
		if s.agent != nil {
			s.agent.Close()
		}
	}()
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue // notifications and noise get no reply
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if result, ok := s.handle(req.Method, req.Params); ok {
			reply["result"] = result
		} else {
			reply["error"] = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *server) handle(method string, params json.RawMessage) (any, bool) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			// Only the tools basics are used, which every revision shares.
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "portmole", "version": "0.1.0"},
		}, true
	case "ping":
		return map[string]any{}, true
	case "tools/list":
		return map[string]any{"tools": json.RawMessage(tools)}, true
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments args   `json:"arguments"`
		}
		json.Unmarshal(params, &p)
		text, err := s.call(p.Name, p.Arguments)
		if err != nil {
			text = err.Error()
		}
		return map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"isError": err != nil,
		}, true
	}
	return nil, false
}

func (s *server) call(tool string, a args) (string, error) {
	switch tool {
	case "tunnel_create":
		return s.create(a)
	case "tunnel_list":
		if !s.connected() || len(s.tunnels) == 0 {
			return "No tunnels are open.", nil
		}
		var lines []string
		for n, t := range s.tunnels {
			lines = append(lines, fmt.Sprintf("%s: %s -> localhost:%d", n, t.url, t.port))
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n"), nil
	case "tunnel_close":
		if _, ok := s.tunnels[a.Name]; !ok || !s.connected() {
			return "", fmt.Errorf("no open tunnel named %q", a.Name)
		}
		delete(s.tunnels, a.Name)
		return "Closed " + a.Name + ".", s.agent.CloseTunnel(a.Name)
	case "tunnel_requests":
		if _, ok := s.tunnels[a.Name]; !ok || !s.connected() {
			return "", fmt.Errorf("no open tunnel named %q", a.Name)
		}
		logs, err := s.agent.Requests(a.Name, a.Limit)
		if err != nil || len(logs) == 0 {
			return "No requests yet.", err
		}
		b, _ := json.MarshalIndent(logs, "", " ")
		return string(b), nil
	}
	return "", fmt.Errorf("unknown tool %q", tool)
}

func (s *server) create(a args) (string, error) {
	if a.Port < 1 || a.Port > 65535 {
		return "", errors.New("port must be between 1 and 65535")
	}
	if signIn, err := s.connect(); err != nil || signIn != "" {
		return signIn, err
	}

	n := a.Name
	if n == "" {
		wd, _ := os.Getwd()
		n = name.FromDir(wd)
	} else if n = name.Clean(n); n == "" {
		return "", errors.New("name needs at least one letter or digit")
	}
	// A second port of the same project gets the port in its name.
	if t, ok := s.tunnels[n]; ok && t.port != a.Port {
		n += "-" + strconv.Itoa(a.Port)
	}
	if t, ok := s.tunnels[n]; ok {
		if t.port != a.Port {
			return "", fmt.Errorf("the name %q is taken by localhost:%d in this session, pass another name", n, t.port)
		}
		// Same tunnel asked for again: reopen so a changed basic_auth applies.
		s.agent.CloseTunnel(n)
		delete(s.tunnels, n)
	}

	note := ""
	url, err := s.agent.Open(n, a.Port, a.BasicAuth)
	if err != nil && strings.Contains(err.Error(), proto.NameInUse) {
		note = fmt.Sprintf("\nThe name %q is open from another machine on this account, so a suffix was added.", n)
		n += "-" + name.Random(4)
		url, err = s.agent.Open(n, a.Port, a.BasicAuth)
	}
	if err != nil {
		return "", err
	}
	s.tunnels[n] = open{port: a.Port, url: url}
	return fmt.Sprintf("%s -> localhost:%d\nTunnel name: %s%s", url, a.Port, n, note), nil
}

// connected reports whether the agent connection is alive, forgetting the
// tunnels of a connection that was lost.
func (s *server) connected() bool {
	if s.agent == nil {
		return false
	}
	select {
	case <-s.agent.Done():
		s.agent, s.tunnels = nil, map[string]open{}
		return false
	default:
		return true
	}
}

// connect makes sure there is a live agent. When the user has to sign in
// first, it returns the instructions to show them instead.
func (s *server) connect() (signIn string, err error) {
	if s.connected() {
		return "", nil
	}
	// PORTMOLE_TOKEN wins so CI and tests need no credentials file.
	token := os.Getenv("PORTMOLE_TOKEN")
	if token == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := auth.Load(s.url)
		if !ok {
			if s.pending == nil {
				d, err := auth.Start(s.ctx, s.url)
				if err != nil {
					return "", err
				}
				s.pending = &d
				go func() {
					auth.Wait(s.ctx, s.url, d) // stores the token once approved
					s.mu.Lock()
					s.pending = nil
					s.mu.Unlock()
				}()
			}
			return fmt.Sprintf("Sign-in needed. Ask the user to open %s, check that the page shows the code %s, and approve. Then call tunnel_create again.", s.pending.URL, s.pending.UserCode), nil
		}
		token = c.Token
	}
	s.agent, err = agent.Dial(s.ctx, s.url, token)
	return "", err
}
