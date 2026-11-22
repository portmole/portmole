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
	"time"

	"github.com/hashicorp/yamux"
	"github.com/portmole/portmole/proto"
)

// ErrUnauthorized means the server does not accept the token: it was revoked
// or the account is closed.
var ErrUnauthorized = errors.New("portmole: the server rejected the token")

type tunnel struct {
	port      int
	basicAuth string
}

// Agent holds one connection to the edge. When the connection drops it is
// dialed again and the open tunnels are restored under the same addresses.
type Agent struct {
	ctx           context.Context
	server, token string

	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex // serializes control round-trips, guards the fields below
	sess    *yamux.Session
	enc     *json.Encoder
	dec     *json.Decoder
	tunnels map[string]tunnel
}

// link is one established connection.
type link struct {
	sess *yamux.Session
	ctrl net.Conn
	// expires is when the edge will end the session; zero if it sets no limit.
	expires time.Time
}

// Dial connects to the edge at server (e.g. "https://portmole.dev"). The
// agent lives until Close is called or ctx is cancelled.
func Dial(ctx context.Context, server, token string) (*Agent, error) {
	a := &Agent{ctx: ctx, server: server, token: token, done: make(chan struct{}), tunnels: map[string]tunnel{}}
	l, err := a.connect()
	if err != nil {
		return nil, err
	}
	a.use(l)
	go a.run(l)
	return a, nil
}

// connect performs the handshake and opens the control stream.
// shortcut: plain Upgrade over a direct connection, no HTTP proxy support;
// switch to WebSocket framing if corporate proxies turn out to matter.
func (a *Agent) connect() (*link, error) {
	u, err := url.Parse(a.server)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	switch u.Scheme {
	case "https":
		// http/1.1 only: the edge hijacks the connection, which HTTP/2 cannot do.
		d := tls.Dialer{Config: &tls.Config{NextProtos: []string{"http/1.1"}}}
		conn, err = d.DialContext(a.ctx, "tcp", hostPort(u, "443"))
	case "http":
		var d net.Dialer
		conn, err = d.DialContext(a.ctx, "tcp", hostPort(u, "80"))
	default:
		return nil, fmt.Errorf("portmole: unsupported server scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, u.Scheme+"://"+u.Host+proto.ConnectPath, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", proto.Upgrade)
	req.Header.Set("Authorization", "Bearer "+a.token)
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
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
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
	l := &link{sess: sess, ctrl: ctrl}
	if secs, _ := strconv.Atoi(resp.Header.Get(proto.SessionHeader)); secs > 0 {
		l.expires = time.Now().Add(time.Duration(secs) * time.Second)
	}
	return l, nil
}

// use makes l the connection control requests go over. Callers hold a.mu,
// except Dial, which runs before the agent is shared.
func (a *Agent) use(l *link) {
	a.sess, a.enc, a.dec = l.sess, json.NewEncoder(l.ctrl), json.NewDecoder(l.ctrl)
}

// run serves the connection and replaces it when it drops, until the agent
// is closed, the token stops working or the session's time is up.
func (a *Agent) run(l *link) {
	defer a.Close()
	for {
		for {
			st, err := l.sess.Accept()
			if err != nil {
				break
			}
			go a.forward(st)
		}
		// The edge ends sessions at its time limit; coming straight back
		// would defeat the limit. The margin covers the two clocks starting
		// a moment apart.
		if !l.expires.IsZero() && time.Until(l.expires) < 5*time.Second {
			return
		}
		for wait := time.Second; ; wait = min(2*wait, 30*time.Second) {
			select {
			case <-a.done:
				return
			case <-a.ctx.Done():
				return
			case <-time.After(wait):
			}
			var err error
			if l, err = a.connect(); err == nil {
				break
			}
			if errors.Is(err, ErrUnauthorized) {
				return
			}
		}
		a.restore(l)
		select {
		case <-a.done: // closed while the new connection was being made
			l.sess.Close()
			return
		default:
		}
	}
}

// restore switches to a new connection and opens the tunnels again.
func (a *Agent) restore(l *link) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.use(l)
	for name, t := range a.tunnels {
		var resp proto.Response
		if a.enc.Encode(proto.Request{Op: proto.OpOpen, Name: name, BasicAuth: t.basicAuth}) != nil || a.dec.Decode(&resp) != nil {
			return // connection lost again; run comes back with the tunnels intact
		}
		if resp.Error != "" {
			delete(a.tunnels, name) // the edge will not have it back, e.g. the name was taken meanwhile
		}
	}
}

// Open exposes localhost:port under name and returns the public URL.
// A non-empty basicAuth ("user:password") makes the edge ask visitors for it.
func (a *Agent) Open(name string, port int, basicAuth string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.tunnels[name]; ok {
		return "", fmt.Errorf("portmole: tunnel %q is already open in this session", name)
	}
	// Registered before the round-trip: a visitor can arrive the moment the edge accepts.
	a.tunnels[name] = tunnel{port: port, basicAuth: basicAuth}
	resp, err := a.roundTrip(proto.Request{Op: proto.OpOpen, Name: name, BasicAuth: basicAuth})
	if err != nil {
		delete(a.tunnels, name)
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
	if _, ok := a.tunnels[name]; !ok {
		return fmt.Errorf("portmole: no open tunnel named %q", name)
	}
	_, err := a.roundTrip(proto.Request{Op: proto.OpClose, Name: name})
	delete(a.tunnels, name)
	return err
}

// Close drops the connection; the edge closes every tunnel of this session.
func (a *Agent) Close() error {
	a.closeOnce.Do(func() { close(a.done) })
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sess.Close()
}

// Done is closed when the agent has stopped for good: after Close, when the
// token is rejected, or when the session's time limit is reached.
func (a *Agent) Done() <-chan struct{} { return a.done }

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

func (a *Agent) forward(st net.Conn) {
	defer st.Close()
	name, err := proto.ReadHeader(st)
	if err != nil {
		return
	}
	a.mu.Lock()
	t, ok := a.tunnels[name]
	a.mu.Unlock()
	if !ok {
		return
	}
	// "localhost", not 127.0.0.1: many dev servers listen on ::1 only.
	local, err := net.Dial("tcp", net.JoinHostPort("localhost", strconv.Itoa(t.port)))
	if err != nil {
		fmt.Fprintf(st, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nportmole: nothing is listening on localhost:%d\n", t.port)
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
