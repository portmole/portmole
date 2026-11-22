// Package proto defines the wire protocol between the portmole agent and the edge.
//
// The agent sends an HTTP/1.1 request to ConnectPath with "Upgrade: portmole/1"
// and a bearer token. After the 101 response the connection carries a yamux
// session. The agent opens the first stream as the control stream and exchanges
// Request/Response as JSON lines. Every other stream is opened by the edge, one
// per visitor request: a header line naming the tunnel, then raw HTTP bytes.
package proto

import (
	"errors"
	"io"
	"time"
)

const (
	ConnectPath = "/_portmole/connect"
	Upgrade     = "portmole/1"

	// SessionHeader, on the 101 response, is how many seconds the edge will
	// keep the connection. A client that loses it around then stays away
	// until it is asked for a tunnel again.
	SessionHeader = "Portmole-Session-Seconds"

	OpOpen     = "open"
	OpClose    = "close"
	OpRequests = "requests"

	// NameInUse is the Response.Error for a name another session of the same
	// account holds; the client retries under a different name.
	NameInUse = "tunnel name is in use by another session"
)

type Request struct {
	Op   string `json:"op"`
	Name string `json:"name"`
	// Instance identifies the agent process, the same across reconnects. An
	// open for a name the same instance still holds on an older connection
	// takes the tunnel over instead of failing with NameInUse.
	Instance string `json:"instance,omitempty"`
	// BasicAuth, as "user:password", makes the edge ask visitors for it.
	BasicAuth string `json:"basic_auth,omitempty"`
	// Limit caps the entries returned by OpRequests; 0 means all kept.
	Limit int `json:"limit,omitempty"`
}

type Response struct {
	URL      string       `json:"url,omitempty"`
	Error    string       `json:"error,omitempty"`
	Requests []RequestLog `json:"requests,omitempty"`
}

// RequestLog is one visitor request as the edge saw it, oldest first in a list.
type RequestLog struct {
	Time    time.Time         `json:"time"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Millis  int64             `json:"ms"`
	Headers map[string]string `json:"headers,omitempty"`
}

const maxHeader = 128

func WriteHeader(w io.Writer, name string) error {
	_, err := io.WriteString(w, name+"\n")
	return err
}

// ReadHeader reads byte by byte so nothing past the newline is consumed.
func ReadHeader(r io.Reader) (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for len(buf) <= maxHeader {
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		if b[0] == '\n' {
			return string(buf), nil
		}
		buf = append(buf, b[0])
	}
	return "", errors.New("proto: stream header too long")
}
