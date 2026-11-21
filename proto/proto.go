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
)

const (
	ConnectPath = "/_portmole/connect"
	Upgrade     = "portmole/1"

	OpOpen  = "open"
	OpClose = "close"
)

type Request struct {
	Op   string `json:"op"`
	Name string `json:"name"`
}

type Response struct {
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
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
