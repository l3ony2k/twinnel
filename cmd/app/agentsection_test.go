package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestAgentSnippets(t *testing.T) {
	cmd := "/Applications/Twinnel.app/Contents/Resources/bin/twinnel"
	if got := agentSnippet(0, "/usr/local/bin/twinnel"); got != "claude mcp add twinnel -- /usr/local/bin/twinnel mcp" {
		t.Errorf("claude = %q", got)
	}
	if got := agentSnippet(1, "/Users/x y/bin/twinnel"); got != "codex mcp add twinnel -- '/Users/x y/bin/twinnel' mcp" {
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
	if s := cfg.MCPServers["twinnel"]; s.Command != cmd || len(s.Args) != 1 || s.Args[0] != "mcp" {
		t.Errorf("json = %+v", cfg)
	}
}

func TestTwinnelLinkOnlyOurs(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HOME", d)
	bundle := filepath.Join(d, "Twinnel.app", "Contents", "Resources", "bin")
	os.MkdirAll(bundle, 0o755)
	os.WriteFile(filepath.Join(bundle, "twinnel"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("TWINNEL_CLI", filepath.Join(bundle, "twinnel"))

	bin := filepath.Join(d, ".local", "bin")
	old := cliTargets
	cliTargets = func() []string { return []string{filepath.Join(d, "usr-local-bin"), bin} }
	t.Cleanup(func() { cliTargets = old })
	dst, err := installCLI(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !isTwinnelLink(dst) {
		t.Fatal("installed link not recognized")
	}
	// Installing again replaces our own link.
	if _, err := installCLI(bin); err != nil {
		t.Fatal(err)
	}
	// A file or a foreign link is never ours: not replaced, not removed.
	other := filepath.Join(d, "other")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "twinnel"), []byte("someone else's"), 0o755)
	if isTwinnelLink(filepath.Join(other, "twinnel")) {
		t.Fatal("a plain file counted as Twinnel's link")
	}
	if _, err := installCLI(other); err == nil || !strings.Contains(err.Error(), "twinnel") {
		t.Fatalf("installCLI over a foreign file = %v", err)
	}
	os.Symlink("/bin/ls", filepath.Join(other, "twinnel2"))
	if isTwinnelLink(filepath.Join(other, "twinnel2")) {
		t.Fatal("a foreign symlink counted as Twinnel's")
	}
	removeCLILinks()
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("removeCLILinks left the link")
	}
	if _, err := os.Stat(filepath.Join(other, "twinnel")); err != nil {
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
