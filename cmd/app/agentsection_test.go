package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
)

func TestAgentSnippets(t *testing.T) {
	cmd := "/Applications/duet.app/Contents/Resources/bin/duet"
	if got := agentSnippet(0, "/usr/local/bin/duet"); got != "claude mcp add duet -- /usr/local/bin/duet mcp" {
		t.Errorf("claude = %q", got)
	}
	if got := agentSnippet(1, "/Users/x y/bin/duet"); got != "codex mcp add duet -- '/Users/x y/bin/duet' mcp" {
		t.Errorf("codex = %q", got)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(agentSnippet(2, cmd)), &cfg); err != nil {
		t.Fatal(err)
	}
	if s := cfg.MCPServers["duet"]; s.Command != cmd || len(s.Args) != 1 || s.Args[0] != "mcp" {
		t.Errorf("json = %+v", cfg)
	}
}

func TestDuetLinkOnlyOurs(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HOME", d)
	bundle := filepath.Join(d, "duet.app", "Contents", "Resources", "bin")
	os.MkdirAll(bundle, 0o755)
	os.WriteFile(filepath.Join(bundle, "duet"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("DUET_CLI", filepath.Join(bundle, "duet"))

	bin := filepath.Join(d, ".local", "bin")
	old := cliTargets
	cliTargets = func() []string { return []string{filepath.Join(d, "usr-local-bin"), bin} }
	t.Cleanup(func() { cliTargets = old })
	dst, err := installCLI(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !isDuetLink(dst) {
		t.Fatal("installed link not recognized")
	}
	// Installing again replaces our own link.
	if _, err := installCLI(bin); err != nil {
		t.Fatal(err)
	}
	// A file or a foreign link is never ours: not replaced, not removed.
	other := filepath.Join(d, "other")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "duet"), []byte("someone else's"), 0o755)
	if isDuetLink(filepath.Join(other, "duet")) {
		t.Fatal("a plain file counted as duet's link")
	}
	if _, err := installCLI(other); err == nil || !strings.Contains(err.Error(), "duet") {
		t.Fatalf("installCLI over a foreign file = %v", err)
	}
	os.Symlink("/bin/ls", filepath.Join(other, "duet2"))
	if isDuetLink(filepath.Join(other, "duet2")) {
		t.Fatal("a foreign symlink counted as duet's")
	}
	removeCLILinks()
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("removeCLILinks left the link")
	}
	if _, err := os.Stat(filepath.Join(other, "duet")); err != nil {
		t.Fatal("removeCLILinks touched a foreign file")
	}
}

func TestAgentEventRow(t *testing.T) {
	data, _ := json.Marshal(rpc.AgentAction{Client: "claude-code 2.1", Tool: "set_mode", Args: "mode=direct", OK: true})
	r, ok := describeEvent(rpc.Event{Type: rpc.EventAgentAction, Data: data}, nil)
	if !ok || !strings.Contains(r.text, "claude-code 2.1") || !strings.Contains(r.text, "set_mode mode=direct") || r.failed {
		t.Fatalf("row = %+v", r)
	}
	data, _ = json.Marshal(rpc.AgentAction{Client: "x", Tool: "apply", OK: false, Error: "boom"})
	r, _ = describeEvent(rpc.Event{Type: rpc.EventAgentAction, Data: data}, nil)
	if !r.failed || r.raw != "boom" {
		t.Fatalf("failed row = %+v", r)
	}
}
