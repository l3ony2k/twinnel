package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCLIPathBundle(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"duet", "duet-helper"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := resourcesDir
	resourcesDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { resourcesDir = old })
	t.Setenv("DUET_CLI", "")

	p, err := cliPath()
	if err != nil || p != filepath.Join(bin, "duet") {
		t.Fatalf("cliPath = %q, %v", p, err)
	}
	args := installArgs("--dry-run")
	if i := slices.Index(args, "--helper-bin"); i < 0 || args[i+1] != filepath.Join(bin, "duet-helper") {
		t.Errorf("installArgs = %v", args)
	}

	t.Setenv("DUET_CLI", "/x/duet")
	if p, _ := cliPath(); p != "/x/duet" {
		t.Errorf("DUET_CLI not first: %q", p)
	}
}
