// Package agent connects to a portmole edge and forwards tunnel traffic to
// local ports.
package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/hashicorp/yamux"
	"github.com/portmole/portmole/proto"
)

type Agent struct {
	sess *yamux.Session

	mu    sync.Mutex // serializes control round-trips, guards ports
	enc   *json.Encoder
	dec   *json.Decoder
	ports map[string]int // tunnel name -> local port
}

// Dial connects to the edge at server (e.g. "https://portmole.dev").
// shortcut: plain Upgrade over a direct connection, no HTTP proxy support;
// switch to WebSocket framing if corporate proxies turn out to matter.
func Dial(ctx context.Context, server, token string) (*Agent, error) {
	u, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	switch u.Scheme {
	case "https":
		// http/1.1 only: the edge hijacks the connection, which HTTP/2 cannot do.
		d := tls.Dialer{Config: &tls.Config{NextProtos: []string{"http/1.1"}}}
		conn, err = d.DialContext(ctx, "tcp", hostPort(u, "443"))
	case "http":
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", hostPort(u, "80"))
	default:
		return nil, fmt.Errorf("portmole: unsupported server scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+proto.ConnectPath, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", proto.Upgrade)
	req.Header.Set("Authorization", "Bearer "+token)
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		conn.Close()
		return nil, fmt.Errorf("portmole: server refused connection: %s: %s", resp.Status, msg)
	}

	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	sess, err := yamux.Client(&bufConn{Conn: conn, r: br}, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	ctrl, err := sess.Open()
	if err != nil {
		sess.Close()
		return nil, err
	}
	a := &Agent{
		sess:  sess,
		enc:   json.NewEncoder(ctrl),
		dec:   json.NewDecoder(ctrl),
		ports: map[string]int{},
	}
	go a.serve()
	return a, nil
}

// Open exposes localhost:port under name and returns the public URL.
// A non-empty basicAuth ("user:password") makes the edge ask visitors for it.
func (a *Agent) Open(name string, port int, basicAuth string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.ports[name]; ok {
		return "", fmt.Errorf("portmole: tunnel %q is already open in this session", name)
	}
	// Registered before the round-trip: a visitor can arrive the moment the edge accepts.
	a.ports[name] = port
	resp, err := a.roundTrip(proto.Request{Op: proto.OpOpen, Name: name, BasicAuth: basicAuth})
	if err != nil {
		delete(a.ports, name)
		return "", err
	}
	return resp.URL, nil
}

// Requests returns the most recent visitor requests of a tunnel, oldest first.
func (a *Agent) Requests(name string, limit int) ([]proto.RequestLog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp, err := a.roundTrip(proto.Request{Op: proto.OpRequests, Name: name, Limit: limit})
	return resp.Requests, err
}

func (a *Agent) CloseTunnel(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.ports[name]; !ok {
		return fmt.Errorf("portmole: no open tunnel named %q", name)
	}
	_, err := a.roundTrip(proto.Request{Op: proto.OpClose, Name: name})
	delete(a.ports, name)
	return err
}

// Close drops the connection; the edge closes every tunnel of this session.
func (a *Agent) Close() error { return a.sess.Close() }

// Done is closed when the connection to the edge is lost.
func (a *Agent) Done() <-chan struct{} { return a.sess.CloseChan() }

func (a *Agent) roundTrip(req proto.Request) (proto.Response, error) {
	var resp proto.Response
	if err := a.enc.Encode(req); err != nil {
		return resp, err
	}
	if err := a.dec.Decode(&resp); err != nil {
		return resp, err
	}
	if resp.Error != "" {
		return resp, errors.New("portmole: " + resp.Error)
	}
	return resp, nil
}

func (a *Agent) serve() {
	for {
		st, err := a.sess.Accept()
		if err != nil {
			return
		}
		go a.forward(st)
	}
}

func (a *Agent) forward(st net.Conn) {
	defer st.Close()
	name, err := proto.ReadHeader(st)
	if err != nil {
		return
	}
	a.mu.Lock()
	port, ok := a.ports[name]
	a.mu.Unlock()
	if !ok {
		return
	}
	// "localhost", not 127.0.0.1: many dev servers listen on ::1 only.
	local, err := net.Dial("tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintf(st, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nportmole: nothing is listening on localhost:%d\n", port)
		return
	}
	defer local.Close()
	go func() {
		io.Copy(local, st)
		local.Close()
	}()
	io.Copy(st, local)
}

func hostPort(u *url.URL, defaultPort string) string {
	if u.Port() != "" {
		return u.Host
	}
	return net.JoinHostPort(u.Hostname(), defaultPort)
}

// bufConn keeps bytes the handshake reader may have buffered past the 101 response.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }
