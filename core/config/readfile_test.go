package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfined(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "conf")
	outside := filepath.Join(dir, "secret")
	if err := os.Mkdir(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(outside, []byte("root-only"), 0o600)
	os.WriteFile(filepath.Join(conf, "config.yaml"), []byte("mode: rule\n"), 0o600)
	os.WriteFile(filepath.Join(conf, "real.yaml"), []byte("mode: global\n"), 0o600)
	os.Symlink(outside, filepath.Join(conf, "escape.yaml"))
	os.Symlink("../secret", filepath.Join(conf, "rel-escape.yaml"))
	os.Symlink("real.yaml", filepath.Join(conf, "inside.yaml"))
	os.Mkdir(filepath.Join(conf, "adir"), 0o755)

	if b, err := ReadConfined(filepath.Join(conf, "config.yaml")); err != nil || string(b) != "mode: rule\n" {
		t.Fatalf("plain: %q %v", b, err)
	}
	if b, err := ReadConfined(filepath.Join(conf, "inside.yaml")); err != nil || string(b) != "mode: global\n" {
		t.Fatalf("symlink inside the dir: %q %v", b, err)
	}
	for _, n := range []string{"escape.yaml", "rel-escape.yaml", "adir", "missing.yaml"} {
		if b, err := ReadConfined(filepath.Join(conf, n)); err == nil {
			t.Errorf("%s: read %q, want error", n, b)
		}
	}
}
