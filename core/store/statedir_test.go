package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDirChecksAsRoot(t *testing.T) {
	old := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = old })
	// The test's own dir is not root-owned: a root helper refuses it.
	if _, err := Open(filepath.Join(t.TempDir(), "state")); err == nil {
		t.Fatal("user-owned state dir accepted as root")
	}
	geteuid = old
	// Subdirs are tightened on every open.
	dir := filepath.Join(t.TempDir(), "state")
	os.MkdirAll(filepath.Join(dir, "layers"), 0o755)
	os.Chmod(filepath.Join(dir, "layers"), 0o755)
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "layers")); fi.Mode().Perm() != 0o700 {
		t.Errorf("layers mode %v", fi.Mode().Perm())
	}
}
