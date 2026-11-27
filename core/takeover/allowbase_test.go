package takeover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllowedBase(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "home", "conf")
	os.MkdirAll(filepath.Join(conf, "a", "b", "c"), 0o755)
	live := filepath.Join(conf, "config.yaml")
	cfg := []byte("proxies:\n  - {name: x, type: direct}\n")
	os.WriteFile(live, cfg, 0o644)
	ok := filepath.Join(conf, "a", "sub.yaml")
	os.WriteFile(ok, cfg, 0o644)
	deep := filepath.Join(conf, "a", "b", "c", "deep.yaml")
	os.WriteFile(deep, cfg, 0o644)
	other := filepath.Join(root, "elsewhere.yaml")
	os.WriteFile(other, cfg, 0o644)
	notcfg := filepath.Join(conf, "notes.yaml")
	os.WriteFile(notcfg, []byte("hello: 1\n"), 0o644)
	link := filepath.Join(conf, "link.yaml")
	os.Symlink(other, link)

	if err := AllowedBase(live, ok); err != nil {
		t.Fatalf("subdir: %v", err)
	}
	for _, p := range []string{live, deep, other, notcfg, link, "/etc/passwd"} {
		if err := AllowedBase(live, p); err == nil {
			t.Errorf("%s allowed", p)
		}
	}
}
