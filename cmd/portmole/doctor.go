package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/portmole/portmole/mcp"
)

// latestURL answers with the newest version published to npm.
const latestURL = "https://registry.npmjs.org/portmole/latest"

// exitNewer is how `portmole update --check` says a newer version exists.
const exitNewer = 10

// latest asks npm for the newest version.
func latest(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("npm was not reached")
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v) != nil || v.Version == "" {
		return "", fmt.Errorf("npm answered %s", resp.Status)
	}
	return v.Version, nil
}

// newer reports whether version a comes after b. A version that is not
// three numbers, such as a development build, counts as older than any.
func newer(a, b string) bool {
	pa, pb := numbers(a), numbers(b)
	if pa == nil {
		return false
	}
	if pb == nil {
		return true
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func numbers(v string) []int {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// update tells whether a newer version exists and how to get it. Replacing
// the program is left to npm, which the npm launcher starts for a global
// install: a program cannot overwrite itself while it runs on Windows.
func update(ctx context.Context, w io.Writer, url string, checkOnly bool) (exit int, err error) {
	v, err := latest(ctx, url)
	if err != nil {
		return 1, fmt.Errorf("portmole: could not look up the newest version: %v", err)
	}
	if !newer(v, mcp.Version) {
		fmt.Fprintf(w, "portmole %s is the newest version.\n", mcp.Version)
		return 0, nil
	}
	fmt.Fprintf(w, "portmole %s is available; this is %s.\n", v, mcp.Version)
	if checkOnly {
		return exitNewer, nil
	}
	exe, fromNpx, _ := self()
	switch {
	case fromNpx:
		fmt.Fprintln(w, "npx fetches the newest version by itself the next time it starts portmole.")
	case strings.Contains(filepath.ToSlash(exe), "/node_modules/"):
		fmt.Fprintln(w, "To update, run: npm install -g portmole@latest")
	default:
		fmt.Fprintf(w, "To update, download it from https://github.com/portmole/portmole/releases/latest\nand put it in place of %s\n", exe)
	}
	return exitNewer, nil
}

// pathProblem describes what is wrong with a PATH for starting MCP servers,
// or returns "" when nothing is.
func pathProblem(goos, path string) string {
	if goos == "windows" && strings.HasPrefix(path, ";") {
		return "PATH begins with a semicolon, an empty first entry. Claude Code then cannot find npx or any other program it is told to start by name.\n      Fix: Start > \"environment variables\" > Path (under System variables, then under User variables) > delete the empty first line; then open a new terminal.\n      `portmole setup` avoids the problem for portmole by giving agents its full path."
	}
	return ""
}

// doctor checks everything an agent needs to start portmole and open a
// tunnel, and says what to do about what it finds. It returns the number
// of problems.
func doctor(ctx context.Context, w io.Writer, server string) int {
	problems := 0
	ok := func(format string, a ...any) { fmt.Fprintf(w, "  ok  "+format+"\n", a...) }
	bad := func(format string, a ...any) { problems++; fmt.Fprintf(w, "  !!  "+format+"\n", a...) }
	note := func(format string, a ...any) { fmt.Fprintf(w, "  --  "+format+"\n", a...) }

	fmt.Fprintln(w, "Program")
	exe, fromNpx, err := self()
	if err != nil {
		bad("its own path is unknown: %v", err)
	} else if fromNpx {
		note("%s (started by npx; install it to use `portmole setup`: npm install -g portmole)", exe)
	} else {
		ok("%s", exe)
	}
	if v, err := latest(ctx, latestURL); err != nil {
		note("version %s; the newest could not be looked up: %v", mcp.Version, err)
	} else if newer(v, mcp.Version) {
		bad("version %s, and %s is available; run: portmole update", mcp.Version, v)
	} else {
		ok("version %s, the newest", mcp.Version)
	}
	if err == nil {
		if err := selfTest(ctx, exe); err != nil {
			bad("the MCP server did not start when tried: %v", err)
		} else {
			ok("the MCP server starts and answers")
		}
	}

	fmt.Fprintln(w, "\nThis machine")
	if p := pathProblem(runtime.GOOS, os.Getenv("PATH")); p != "" {
		bad("%s", p)
	} else {
		ok("PATH has no empty first entry")
	}
	npx, npxErr := exec.LookPath("npx")
	if npxErr != nil {
		note("npx is not on the PATH; that matters only for agents that start portmole through npx")
	} else {
		ok("npx: %s", npx)
	}

	fmt.Fprintln(w, "\nCoding agents")
	any := false
	for _, t := range tools() {
		if !t.found() {
			continue
		}
		any = true
		command, args, added := t.entry()
		line := strings.TrimSpace(command + " " + strings.Join(args, " "))
		switch {
		case !added:
			// Not a problem: nobody has to use portmole from every agent.
			note("%s: portmole is not added; to add it: portmole setup %s", t.name, t.key)
		case filepath.IsAbs(command):
			if _, err := os.Stat(command); err != nil {
				bad("%s: starts %s, which is not there; run: portmole setup %s", t.name, command, t.key)
			} else {
				ok("%s: %s", t.name, line)
			}
		case npxErr != nil && (command == "npx" || command == "cmd"):
			bad("%s: starts portmole through npx, which is not on the PATH; run: portmole setup %s", t.name, t.key)
		default:
			// By name: found through the PATH the agent happens to have.
			ok("%s: %s", t.name, line)
			if !fromNpx {
				note("that entry depends on the agent finding %s on its PATH; `portmole setup %s` gives it the full path instead", command, t.key)
			}
		}
	}
	if !any {
		note("none of Claude Code, Claude Desktop, Codex, Cursor, Windsurf or Gemini CLI was found")
	}
	note("an entry added inside one project (for example .cursor/mcp.json) is not looked at here")

	fmt.Fprintln(w, "\nService")
	status(ctx, w, server, consoleURL)

	if problems == 0 {
		fmt.Fprintln(w, "\nNo problems found.")
	} else {
		fmt.Fprintf(w, "\n%d problem(s) found; each line marked !! says what to do.\n", problems)
	}
	return problems
}
