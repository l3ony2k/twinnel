package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMigratePrefs(t *testing.T) {
	d := t.TempDir()
	old := filepath.Join(d, "duet")
	os.MkdirAll(filepath.Join(old, "backups", "duet-0.1.app"), 0o755)
	os.WriteFile(filepath.Join(old, "app.json"), []byte(`{"sidebar":"collapsed"}`), 0o600)
	os.WriteFile(filepath.Join(old, "window-state.json"), []byte(`{}`), 0o644)
	moved, err := migratePrefs(d)
	if err != nil || !slices.Equal(moved, []string{"app.json", "window-state.json"}) {
		t.Fatalf("%v %v", moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "twinnel", "app.json")); string(b) != `{"sidebar":"collapsed"}` {
		t.Errorf("app.json: %s", b)
	}
	if fi, err := os.Stat(filepath.Join(d, "twinnel", "app.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(old, "backups", "duet-0.1.app")); err != nil {
		t.Errorf("backups moved: %v", err)
	}
	// A second launch (or a newer file already in place) moves nothing.
	os.WriteFile(filepath.Join(old, "app.json"), []byte(`{"old":true}`), 0o600)
	if moved, _ := migratePrefs(d); len(moved) != 0 {
		t.Errorf("moved again: %v", moved)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "twinnel", "app.json")); string(b) != `{"sidebar":"collapsed"}` {
		t.Errorf("new app.json overwritten: %s", b)
	}
	if err := removeLegacyPrefs(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old folder stays: %v", err)
	}
	if err := removeLegacyPrefs(filepath.Join(d, "twinnel")); err == nil {
		t.Error("removed another folder")
	}
}

func TestIsLegacyLink(t *testing.T) {
	d := t.TempDir()
	ours := filepath.Join(d, "duet")
	os.Symlink("/Applications/duet.app/Contents/Resources/bin/duet", ours)
	other := filepath.Join(d, "duet2")
	os.Symlink("/usr/local/Cellar/duet/bin/duet", other)
	plain := filepath.Join(d, "duet3")
	os.WriteFile(plain, nil, 0o755)
	if !isLegacyLink(ours) || isLegacyLink(other) || isLegacyLink(plain) {
		t.Error(isLegacyLink(ours), isLegacyLink(other), isLegacyLink(plain))
	}
}
