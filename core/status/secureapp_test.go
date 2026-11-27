package status

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/privfile/privfiletest"
)

// An app.json written before it was protected (or moved in from the old
// app's folder) is private after SecureAppFiles, with its folder.
func TestSecureAppFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "twinnel")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "app.json")
	if err := os.WriteFile(p, []byte(`{"controller":"127.0.0.1:19090"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := SecureAppFiles(p); err != nil {
			t.Fatal(err)
		}
	}
	privfiletest.AssertPrivate(t, dir, true)
	privfiletest.AssertPrivate(t, p, false)
	if err := SecureAppFiles(filepath.Join(t.TempDir(), "none", "app.json")); err != nil {
		t.Errorf("missing: %v", err)
	}
	if err := SecureAppFiles(""); err != nil {
		t.Error(err)
	}
}
