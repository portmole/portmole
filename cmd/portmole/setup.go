package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A tool is a coding agent that portmole can add itself to.
type tool struct {
	key  string // what the user types: portmole setup <key>
	name string
	cli  string // the agent's own command, when it has one that adds MCP servers
	file string // otherwise the JSON settings file that lists them
}

// tools lists the agents setup knows, with their settings under home and
// the user config directory.
func tools() []tool {
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir()
	return []tool{
		{key: "claude", name: "Claude Code", cli: "claude", file: filepath.Join(home, ".claude.json")},
		{key: "claude-desktop", name: "Claude Desktop", file: filepath.Join(config, "Claude", "claude_desktop_config.json")},
		{key: "codex", name: "Codex", cli: "codex", file: filepath.Join(home, ".codex", "config.toml")},
		{key: "cursor", name: "Cursor", file: filepath.Join(home, ".cursor", "mcp.json")},
		{key: "windsurf", name: "Windsurf", file: filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")},
		{key: "gemini", name: "Gemini CLI", file: filepath.Join(home, ".gemini", "settings.json")},
	}
}

// found reports whether the agent is on this machine: its command is on the
// PATH, or the folder that holds its settings exists.
func (t tool) found() bool {
	if t.cli != "" {
		_, err := exec.LookPath(t.cli)
		return err == nil
	}
	_, err := os.Stat(filepath.Dir(t.file))
	return err == nil
}

// add makes the agent start exe as its portmole MCP server, replacing an
// entry that is already there.
func (t tool) add(ctx context.Context, exe string) error {
	if t.cli == "" {
		return addToJSON(t.file, exe)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	remove, add := []string{"mcp", "remove", "portmole"}, []string{"mcp", "add", "portmole", "--", exe, "mcp"}
	if t.key == "claude" {
		// User scope: every project, not only the folder setup was run in.
		remove = append(remove, "-s", "user")
		add = []string{"mcp", "add", "-s", "user", "portmole", "--", exe, "mcp"}
	}
	exec.CommandContext(ctx, t.cli, remove...).Run() // fails when there is nothing to remove
	if out, err := exec.CommandContext(ctx, t.cli, add...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s said: %s", t.cli, firstLine(out, err))
	}
	return nil
}

// entry is how the agent starts portmole now: the command and its
// arguments, or ok false when portmole is not in its settings.
func (t tool) entry() (command string, args []string, ok bool) {
	b, err := os.ReadFile(t.file)
	if err != nil {
		return "", nil, false
	}
	if strings.HasSuffix(t.file, ".toml") {
		// Enough TOML for the one table Codex writes.
		_, table, ok := strings.Cut(string(b), "[mcp_servers.portmole]")
		if !ok {
			return "", nil, false
		}
		for _, line := range strings.Split(table, "\n")[1:] {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				break
			}
			if k, v, found := strings.Cut(line, "="); found && strings.TrimSpace(k) == "command" {
				command = strings.Trim(strings.TrimSpace(v), `"'`)
				command = strings.ReplaceAll(command, `\\`, `\`)
			}
		}
		return command, nil, true
	}
	var settings struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b, &settings) != nil {
		return "", nil, false
	}
	e, ok := settings.Servers["portmole"]
	return e.Command, e.Args, ok
}

// addToJSON sets mcpServers.portmole in a JSON settings file and leaves the
// rest of the file's values as they were.
// shortcut: the file is written back with its top-level keys sorted and
// two-space indents; keep the order if someone minds the reshuffle.
func addToJSON(file, exe string) error {
	root := map[string]json.RawMessage{}
	b, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(bytes.TrimSpace(b)) > 0 && json.Unmarshal(b, &root) != nil {
		return fmt.Errorf("%s is not plain JSON (it may hold comments), so it was left alone; add portmole to it by hand", file)
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && json.Unmarshal(raw, &servers) != nil {
		return fmt.Errorf("mcpServers in %s is not a list of servers, so it was left alone", file)
	}
	servers["portmole"], _ = json.Marshal(map[string]any{"command": exe, "args": []string{"mcp"}})
	root["mcpServers"], _ = json.Marshal(servers)
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	// Written beside the file and renamed, so a crash cannot leave half of it.
	tmp := file + ".portmole-tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// self is the path of the running program, and whether npx started it from
// its cache, where it does not stay.
func self() (exe string, fromNpx bool, err error) {
	if exe, err = os.Executable(); err != nil {
		return "", false, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, strings.Contains(filepath.ToSlash(exe), "/_npx/"), nil
}

// choose picks the agents to add portmole to: the ones named, or the ones
// found, which the user can narrow down when there is someone to ask.
func choose(all []tool, names []string, in io.Reader, w io.Writer, ask bool) ([]tool, error) {
	if len(names) > 0 {
		var picked []tool
		for _, n := range names {
			i := 0
			for i < len(all) && all[i].key != n {
				i++
			}
			if i == len(all) {
				keys := make([]string, len(all))
				for j, t := range all {
					keys[j] = t.key
				}
				return nil, fmt.Errorf("portmole does not know the agent %q; it knows: %s", n, strings.Join(keys, ", "))
			}
			picked = append(picked, all[i])
		}
		return picked, nil
	}
	var found []tool
	for _, t := range all {
		if t.found() {
			found = append(found, t)
		}
	}
	if len(found) == 0 || !ask {
		return found, nil
	}
	fmt.Fprintln(w, "Coding agents found on this machine:")
	for i, t := range found {
		fmt.Fprintf(w, "  %d. %s\n", i+1, t.name)
	}
	fmt.Fprint(w, "Add portmole to all of them? Press Enter for yes, or type the numbers to pick (for example 1 3): ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
	if len(fields) == 0 || strings.EqualFold(fields[0], "y") || strings.EqualFold(fields[0], "yes") {
		return found, nil
	}
	var picked []tool
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(found) {
			return nil, fmt.Errorf("%q is not a number from the list", f)
		}
		picked = append(picked, found[n-1])
	}
	return picked, nil
}

// setup adds portmole to the coding agents on this machine, as the full
// path of this program, so the agent needs neither npx nor the PATH to
// start it.
func setup(ctx context.Context, in io.Reader, w io.Writer, names []string) error {
	exe, fromNpx, err := self()
	if err != nil {
		return err
	}
	if fromNpx {
		return errors.New("portmole was started by npx, which keeps the program in a cache that gets cleared.\nInstall it first, then run setup again:\n\n  npm install -g portmole\n  portmole setup")
	}
	fi, _ := os.Stdin.Stat()
	ask := in == os.Stdin && fi != nil && fi.Mode()&os.ModeCharDevice != 0
	picked, err := choose(tools(), names, in, w, ask)
	if err != nil {
		return err
	}
	if len(picked) == 0 {
		fmt.Fprintln(w, "No coding agent that portmole knows was found on this machine.\nName one to add portmole anyway: portmole setup claude | claude-desktop | codex | cursor | windsurf | gemini\nFor any other tool, the command is the line below and the only argument is mcp:\n\n  "+exe)
		return nil
	}
	failed := 0
	for _, t := range picked {
		if err := t.add(ctx, exe); err != nil {
			failed++
			fmt.Fprintf(w, "  %-15s not added: %v\n", t.name, err)
		} else {
			fmt.Fprintf(w, "  %-15s added\n", t.name)
		}
	}
	if failed == len(picked) {
		return errors.New("portmole was not added to any agent")
	}
	if err := selfTest(ctx, exe); err != nil {
		fmt.Fprintf(w, "\nThe MCP server did not start when tried: %v\nRun: portmole doctor\n", err)
		return nil
	}
	fmt.Fprintln(w, "\nStart a new session in the agent, then ask in plain words: open port 3000 with portmole\nThe first tunnel asks you to sign in with GitHub. To check the setup later: portmole doctor")
	return nil
}

// selfTest starts this program as an MCP server and waits for it to answer
// the first message an agent sends.
func selfTest(ctx context.Context, exe string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "mcp")
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"portmole doctor","version":"0"}}}` + "\n")
	out, err := cmd.Output()
	if bytes.Contains(out, []byte(`"serverInfo"`)) {
		return nil
	}
	if err == nil {
		err = errors.New("it gave no answer")
	}
	return err
}

func firstLine(out []byte, err error) string {
	if s := strings.TrimSpace(string(out)); s != "" {
		line, _, _ := strings.Cut(s, "\n")
		return strings.TrimSpace(line)
	}
	return err.Error()
}
