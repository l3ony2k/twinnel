package clilink

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) (Tool, string, []string) {
	root := t.TempDir()
	state := filepath.Join(root, "twinnel-state")
	target := filepath.Join(state, "tailscale", "current", "tailscale")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755)
	tool := Tailscale(state)
	tool.Shadows = []string{filepath.Join(root, "brew")}
	tool.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	dirs := []string{filepath.Join(root, "ro"), filepath.Join(root, "bin")}
	os.MkdirAll(dirs[1], 0o755)
	return tool, root, dirs
}

func TestLinkAndRemove(t *testing.T) {
	tool, _, dirs := setup(t)
	// The first dir does not exist (and is not under home): the next.
	p, err := tool.Link(dirs)
	if err != nil || p != filepath.Join(dirs[1], "tailscale") || !tool.IsOurs(p) {
		t.Fatalf("link %q %v", p, err)
	}
	if again, err := tool.Link(dirs); err != nil || again != p {
		t.Fatalf("relink %q %v", again, err)
	}
	if got := tool.Links(dirs); len(got) != 1 {
		t.Fatalf("links %v", got)
	}
	if rm := tool.Remove(dirs); len(rm) != 1 {
		t.Fatalf("removed %v", rm)
	}
	if _, err := os.Lstat(p); err == nil {
		t.Fatal("still there")
	}
}

func TestNeverReplacesForeign(t *testing.T) {
	tool, root, dirs := setup(t)
	// A Homebrew tailscale elsewhere on PATH.
	brew := filepath.Join(root, "brew")
	os.MkdirAll(brew, 0o755)
	os.WriteFile(filepath.Join(brew, "tailscale"), []byte("x"), 0o755)
	var op *OnPathError
	if _, err := tool.Link(dirs); !errors.As(err, &op) || op.Path != filepath.Join(brew, "tailscale") {
		t.Fatalf("err = %v", err)
	}
	os.Remove(filepath.Join(brew, "tailscale"))
	// A file of the same name in the target dir.
	own := filepath.Join(dirs[1], "tailscale")
	os.WriteFile(own, []byte("mine"), 0o755)
	if _, err := tool.Link(dirs); !errors.As(err, &op) {
		t.Fatalf("err = %v", err)
	}
	if tool.Remove(dirs) != nil {
		t.Fatal("removed a foreign file")
	}
	if b, _ := os.ReadFile(own); string(b) != "mine" {
		t.Fatal("foreign file changed")
	}
	// Found on the process's PATH.
	os.Remove(own)
	tool.LookPath = func(string) (string, error) { return "/somewhere/tailscale", nil }
	if _, err := tool.Link(dirs); !errors.As(err, &op) {
		t.Fatalf("err = %v", err)
	}
}

func TestLinkNeedsTarget(t *testing.T) {
	tool, _, dirs := setup(t)
	os.Remove(tool.Target)
	if _, err := tool.Link(dirs); err == nil {
		t.Fatal("linked a missing target")
	}
}
