package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeCommand(t *testing.T) {
	td := filepath.Join("..", "..", "core", "config", "testdata")
	out := filepath.Join(t.TempDir(), "config.yaml")
	args := []string{
		"--sub", filepath.Join(td, "subscription.yaml"),
		"--base", filepath.Join(td, "base.yaml"),
		"--layer", filepath.Join(td, "user-direct.yaml"),
		"--tailnet-json", filepath.Join(td, "tailscale-status.json"),
		"--leak-guard", "Google",
		"-o", out,
	}
	if err := runCompose(args); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	want, _ := os.ReadFile(filepath.Join(td, "composed.golden.yaml"))
	if string(got) != string(want) {
		t.Errorf("output differs from golden:\n%s", got)
	}
	// A second run must not overwrite without --force.
	if err := runCompose(args); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("overwrite: err = %v", err)
	}
	if err := runCompose(append(args, "--force")); err != nil {
		t.Errorf("--force: %v", err)
	}
}

func TestComposeCommandVerge(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub.yaml")
	verge := filepath.Join(dir, "merge.yaml")
	out := filepath.Join(dir, "out.yaml")
	os.WriteFile(sub, []byte("rules:\n  - MATCH,DIRECT\n"), 0o644)
	os.WriteFile(verge, []byte("prepend-rules:\n  - DOMAIN,a.example,DIRECT\n"), 0o644)
	if err := runCompose([]string{"--sub", sub, "--verge", verge, "-o", out}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if want := "rules:\n  - DOMAIN,a.example,DIRECT\n  - MATCH,DIRECT\n"; string(got) != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestComposeCommandErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--sub", "x", "extra"},
		{"--sub", "x", "--tailnet-from-status", "--tailnet-json", "y"},
		{"--sub", filepath.Join(t.TempDir(), "missing.yaml")},
	} {
		if err := runCompose(args); err == nil {
			t.Errorf("%q: no error", args)
		}
	}
}
