package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusWithoutASignInAndWithAConsole(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"AppData", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(k, dir)
	}
	t.Setenv("PORTMOLE_TOKEN", "")
	answer := `{"tunnels":[{"name":"shop","port":3000,"url":"https://shop-ada.portmole.test"}],"used":0,"allowance":0}`
	console := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, answer) }))
	defer console.Close()

	var out bytes.Buffer
	status(context.Background(), &out, "https://portmole.test", console.URL)
	for _, want := range []string{"Signed in: no; run: portmole login", "1 tunnel(s) open", "https://shop-ada.portmole.test -> localhost:3000"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}

	// Another program on the console's port is not taken for the console.
	answer = `<html>something else</html>`
	out.Reset()
	status(context.Background(), &out, "https://portmole.test", console.URL)
	if !strings.Contains(out.String(), "Console:   not running") {
		t.Errorf("another program was taken for the console:\n%s", out.String())
	}
}
