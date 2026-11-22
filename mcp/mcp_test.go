package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// session drives Serve the way an MCP client does.
type session struct {
	t   *testing.T
	in  io.Writer
	out *bufio.Scanner
	id  int
}

func start(t *testing.T, serverURL string) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		Serve(context.Background(), inR, outW, serverURL)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	return &session{t: t, in: inW, out: bufio.NewScanner(outR)}
}

func (s *session) request(method, params string) map[string]any {
	s.t.Helper()
	s.id++
	fmt.Fprintf(s.in, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", s.id, method, params)
	if !s.out.Scan() {
		s.t.Fatalf("no reply to %s", method)
	}
	var reply map[string]any
	if err := json.Unmarshal(s.out.Bytes(), &reply); err != nil {
		s.t.Fatalf("reply to %s is not one JSON line: %v", method, err)
	}
	if reply["id"] != float64(s.id) {
		s.t.Fatalf("reply id = %v, want %d", reply["id"], s.id)
	}
	return reply
}

func (s *session) call(tool, arguments string) (text string, isError bool) {
	s.t.Helper()
	result := s.request("tools/call", fmt.Sprintf(`{"name":%q,"arguments":%s}`, tool, arguments))["result"].(map[string]any)
	return result["content"].([]any)[0].(map[string]any)["text"].(string), result["isError"].(bool)
}

func TestProtocolAndSignInPrompt(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(k, dir)
	}
	t.Setenv("PORTMOLE_TOKEN", "")
	starts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/device", func(w http.ResponseWriter, _ *http.Request) {
		starts++
		io.WriteString(w, `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri_complete":"http://x/activate?code=ABCD-EFGH","interval":5}`)
	})
	mux.HandleFunc("POST /api/device/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"authorization_pending"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := start(t, srv.URL)

	init := s.request("initialize", `{"protocolVersion":"2025-03-26"}`)["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" || init["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize: %v", init)
	}
	// A notification gets no reply; the next reply must belong to tools/list.
	io.WriteString(s.in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	list := s.request("tools/list", `{}`)["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, tool := range list {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if got := strings.Join(names, " "); got != "tunnel_create tunnel_list tunnel_close tunnel_requests" {
		t.Fatalf("tools = %q", got)
	}
	if s.request("resources/list", `{}`)["error"] == nil {
		t.Fatal("unknown method did not return an error")
	}

	if text, isErr := s.call("tunnel_list", `{}`); isErr || text != "No tunnels are open." {
		t.Fatalf("tunnel_list: %q %v", text, isErr)
	}
	if text, isErr := s.call("tunnel_create", `{"port":0}`); !isErr {
		t.Fatalf("port 0 accepted: %q", text)
	}
	if text, isErr := s.call("tunnel_close", `{"name":"nope"}`); !isErr {
		t.Fatalf("closing an unknown tunnel succeeded: %q", text)
	}

	// Not signed in: the tool hands back the link, and asking again reuses it.
	for i := 0; i < 2; i++ {
		text, isErr := s.call("tunnel_create", `{"port":3000}`)
		if isErr || !strings.Contains(text, "http://x/activate?code=ABCD-EFGH") || !strings.Contains(text, "call tunnel_create again") {
			t.Fatalf("sign-in prompt: %q %v", text, isErr)
		}
	}
	if starts != 1 {
		t.Fatalf("sign-ins started = %d, want 1", starts)
	}
}
