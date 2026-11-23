package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/portmole/portmole/mcp"
)

func TestAddToJSONKeepsWhatIsThereAndRefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "mcp.json")
	exe := `C:\Program Files\portmole\portmole.exe`

	// No file yet: it is created.
	if err := addToJSON(file, exe); err != nil {
		t.Fatal(err)
	}
	tl := tool{file: file}
	if command, args, ok := tl.entry(); !ok || command != exe || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("entry after the first add: %q %q %v", command, args, ok)
	}

	// Other servers and other settings stay; an old portmole entry is replaced.
	os.WriteFile(file, []byte(`{"theme":"dark","mcpServers":{"other":{"command":"x"},"portmole":{"command":"npx","args":["-y","portmole","mcp"]}}}`), 0o600)
	if err := addToJSON(file, exe); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Theme   string                     `json:"theme"`
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	b, _ := os.ReadFile(file)
	if err := json.Unmarshal(b, &got); err != nil || got.Theme != "dark" || len(got.Servers) != 2 || !strings.Contains(string(got.Servers["other"]), `"x"`) {
		t.Fatalf("the rest of the file changed: %s", b)
	}
	if command, _, _ := tl.entry(); command != exe {
		t.Fatalf("the old entry was not replaced: %q", command)
	}

	// A file with comments is left exactly as it was.
	commented := []byte("{\n  // my servers\n  \"mcpServers\": {}\n}\n")
	os.WriteFile(file, commented, 0o600)
	if err := addToJSON(file, exe); err == nil {
		t.Fatal("a file that is not plain JSON was rewritten")
	}
	if b, _ := os.ReadFile(file); !bytes.Equal(b, commented) {
		t.Fatalf("the file was touched: %s", b)
	}
}

func TestEntryReadsCodexSettings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(file, []byte("model = \"x\"\n\n[mcp_servers.portmole]\ncommand = \"C:\\\\tools\\\\portmole.exe\"\nargs = [\"mcp\"]\n\n[other]\ncommand = \"no\"\n"), 0o600)
	if command, _, ok := (tool{file: file}).entry(); !ok || command != `C:\tools\portmole.exe` {
		t.Fatalf("got %q %v", command, ok)
	}
	os.WriteFile(file, []byte("model = \"x\"\n"), 0o600)
	if _, _, ok := (tool{file: file}).entry(); ok {
		t.Fatal("found an entry that is not there")
	}
}

func TestChoose(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o700)
	os.MkdirAll(filepath.Join(dir, "c"), 0o700)
	all := []tool{
		{key: "a", name: "A", file: filepath.Join(dir, "a", "mcp.json")},
		{key: "b", name: "B", file: filepath.Join(dir, "b", "mcp.json")}, // not installed
		{key: "c", name: "C", file: filepath.Join(dir, "c", "mcp.json")},
	}
	keys := func(ts []tool) string {
		var s []string
		for _, t := range ts {
			s = append(s, t.key)
		}
		return strings.Join(s, ",")
	}
	for _, c := range []struct {
		names []string
		typed string
		ask   bool
		want  string
		fails bool
	}{
		{want: "a,c"},                          // nobody to ask: the ones found
		{ask: true, typed: "\n", want: "a,c"},  // Enter: all found
		{ask: true, typed: "2\n", want: "c"},   // a number from the list
		{ask: true, typed: "5\n", fails: true}, // not in the list
		{names: []string{"b"}, want: "b"},      // named: added even when not found
		{names: []string{"nope"}, fails: true}, // unknown name
	} {
		var out bytes.Buffer
		got, err := choose(all, c.names, strings.NewReader(c.typed), &out, c.ask)
		if (err != nil) != c.fails || keys(got) != c.want {
			t.Errorf("names=%v typed=%q ask=%v: got %q, %v; want %q", c.names, c.typed, c.ask, keys(got), err, c.want)
		}
	}
}

func TestNewerAndPathProblem(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true}, {"0.1.10", "0.1.9", true}, {"0.1.2", "0.1.2", false},
		{"0.1.1", "0.1.2", false}, {"0.2.0", "dev", true}, {"dev", "0.1.0", false},
	} {
		if newer(c.a, c.b) != c.want {
			t.Errorf("newer(%q, %q) is not %v", c.a, c.b, c.want)
		}
	}
	if pathProblem("windows", `;C:\Windows`) == "" || pathProblem("windows", `C:\Windows;`) != "" || pathProblem("linux", ";/usr/bin") != "" {
		t.Error("pathProblem is wrong about a leading semicolon")
	}
}

func TestUpdateCheck(t *testing.T) {
	version := "9.9.9"
	npm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": version})
	}))
	defer npm.Close()
	var out bytes.Buffer
	if exit, err := update(context.Background(), &out, npm.URL, true); err != nil || exit != exitNewer || !strings.Contains(out.String(), "9.9.9 is available") {
		t.Fatalf("newer version: exit %d, %v, %q", exit, err, out.String())
	}
	version = mcp.Version
	out.Reset()
	if exit, err := update(context.Background(), &out, npm.URL, true); err != nil || exit != 0 || !strings.Contains(out.String(), "newest version") {
		t.Fatalf("same version: exit %d, %v, %q", exit, err, out.String())
	}
	npm.Close()
	if _, err := update(context.Background(), &out, npm.URL, true); err == nil {
		t.Fatal("no error when npm cannot be reached")
	}
}
