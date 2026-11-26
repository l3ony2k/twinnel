//go:build darwin

package darwin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Finding 13: the health env may run as root; a config path that is a
// symlink out of its dir must not be read.
func TestMihomoConfigRefusesEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "conf")
	os.Mkdir(conf, 0o755)
	os.WriteFile(filepath.Join(dir, "secret"), []byte("external-controller: 127.0.0.1:1\n"), 0o600)
	link := filepath.Join(conf, "config.yaml")
	if err := os.Symlink(filepath.Join(dir, "secret"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{ConfigPath: link}).MihomoConfig(context.Background()); err == nil {
		t.Fatal("read a config through a symlink leaving its dir")
	}
	real := filepath.Join(conf, "real.yaml")
	os.WriteFile(real, []byte("external-controller: 127.0.0.1:9090\n"), 0o600)
	if c, err := New(Options{ConfigPath: real}).MihomoConfig(context.Background()); err != nil || c.ExternalController != "127.0.0.1:9090" {
		t.Fatalf("plain config: %+v %v", c, err)
	}
}
