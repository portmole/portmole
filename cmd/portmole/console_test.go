package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/portmole/portmole/agent"
	"github.com/portmole/portmole/proto"
)

type fakeAgent struct {
	open map[string]int
	fail string // a name that cannot be opened
}

func (f *fakeAgent) OpenWith(n string, port int, _ agent.Options) (string, error) {
	if n == f.fail {
		return "", errors.New("name is in use")
	}
	f.open[n] = port
	return "https://" + n + "-ada.example", nil
}
func (f *fakeAgent) CloseTunnel(n string) error { delete(f.open, n); return nil }
func (f *fakeAgent) Requests(string, int) ([]proto.RequestLog, error) {
	return nil, nil
}
func (f *fakeAgent) Usage() (int64, int64, error) { return 0, 0, nil }

func consoleCall(c *console, method, path, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:4040"+path, strings.NewReader(body))
	if method == http.MethodPost {
		r.Header.Set("Origin", "http://127.0.0.1:4040")
		r.Header.Set("Content-Type", "application/json")
	}
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	return w
}

func TestConsoleOpensRenamesAndCloses(t *testing.T) {
	f := &fakeAgent{open: map[string]int{}, fail: "taken"}
	c := &console{a: f, dir: "/work/my-shop", host: "127.0.0.1:4040", tunnels: map[string]consoleTunnel{}}

	if w := consoleCall(c, "POST", "/api/open", `{"port":3000}`, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "https://my-shop-ada.example") {
		t.Fatalf("open: %d %s", w.Code, w.Body)
	}
	if w := consoleCall(c, "POST", "/api/rename", `{"name":"my-shop","to":"Demo Site"}`, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "https://demo-site-ada.example") {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	if f.open["demo-site"] != 3000 || len(f.open) != 1 {
		t.Fatalf("after rename the agent has %v", f.open)
	}
	// A rename that fails leaves the port reachable at its old address.
	if w := consoleCall(c, "POST", "/api/rename", `{"name":"demo-site","to":"taken"}`, nil); w.Code != 400 {
		t.Fatalf("failed rename: %d %s", w.Code, w.Body)
	}
	if f.open["demo-site"] != 3000 || c.tunnels["demo-site"].Port != 3000 {
		t.Fatalf("a failed rename lost the tunnel: %v", f.open)
	}
	if w := consoleCall(c, "POST", "/api/open", `{"port":99999}`, nil); w.Code != 400 {
		t.Fatalf("bad port: %d", w.Code)
	}
	if w := consoleCall(c, "POST", "/api/close", `{"name":"demo-site"}`, nil); w.Code != 200 || len(f.open) != 0 {
		t.Fatalf("close: %d %s, agent has %v", w.Code, w.Body, f.open)
	}
}

// Opening a tunnel publishes a local port, so no other site may ask for it.
func TestConsoleRefusesOtherSites(t *testing.T) {
	f := &fakeAgent{open: map[string]int{}}
	c := &console{a: f, host: "127.0.0.1:4040", tunnels: map[string]consoleTunnel{}}
	for what, edit := range map[string]func(*http.Request){
		"another origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"no origin":      func(r *http.Request) { r.Header.Del("Origin") },
		"a form post":    func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		"a rebound host": func(r *http.Request) {
			r.Host = "evil.example:4040"
			r.Header.Set("Origin", "http://evil.example:4040")
		},
		"the wrong port": func(r *http.Request) { r.Host = "127.0.0.1:9999"; r.Header.Set("Origin", "http://127.0.0.1:9999") },
	} {
		if w := consoleCall(c, "POST", "/api/open", `{"port":3000,"name":"x"}`, edit); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", what, w.Code)
		}
	}
	if w := consoleCall(c, "GET", "/api/state", "", func(r *http.Request) { r.Host = "evil.example" }); w.Code != http.StatusForbidden {
		t.Errorf("state for another host: %d", w.Code)
	}
	if len(f.open) != 0 {
		t.Fatalf("a refused request opened %v", f.open)
	}
	if w := consoleCall(c, "GET", "/", "", func(r *http.Request) { r.Host = "localhost:4040" }); w.Code != 200 {
		t.Errorf("page on localhost: %d", w.Code)
	}
}
