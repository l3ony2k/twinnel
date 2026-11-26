package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLayoutAndItems(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"subscriptions", "layers", "history", "kernels"} {
		fi, err := os.Stat(s.Path(d))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", d, fi.Mode(), err)
		}
	}
	if err := s.WriteLayer("my-rules", []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(s.Path("layers", "my-rules.yaml"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("layer mode %v", fi.Mode().Perm())
	}
	if b, err := s.ReadLayer("my-rules"); err != nil || string(b) != "a: 1\n" {
		t.Errorf("read: %q %v", b, err)
	}
	for _, bad := range []string{"../x", "a/b", "", "UP", ".hidden", strings.Repeat("a", 65)} {
		if err := s.WriteLayer(bad, nil); err == nil {
			t.Errorf("id %q accepted", bad)
		}
		if _, err := s.ReadSubscription(bad); err == nil {
			t.Errorf("id %q accepted", bad)
		}
	}
	if err := s.DeleteLayer("my-rules"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLayer("my-rules"); err != nil {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.ReadLayer("my-rules"); !errors.Is(err, ErrNotExist) {
		t.Errorf("after delete: %v", err)
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	st, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mihomo.ConfigFile != filepath.Join(dir, "mihomo", "config.yaml") || st.HistoryKeep != 30 || !*st.AutoApply || st.KernelChannel != "stable" {
		t.Errorf("defaults: %+v", st)
	}
	_, err = s.UpdateSettings(func(st *Settings) error {
		st.Layers = append(st.Layers, Layer{ID: "a", Name: "A", Enabled: true})
		st.Subscriptions = append(st.Subscriptions, Subscription{ID: "s1", Active: true})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ = s.Settings()
	if len(st.Layers) != 1 || st.Layers[0].Stage != StageUser || st.Layers[0].Format != FormatMerge {
		t.Errorf("layers: %+v", st.Layers)
	}
	fi, _ := os.Stat(s.Path("settings.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("settings mode %v", fi.Mode().Perm())
	}

	bad := []func(st *Settings){
		func(st *Settings) { st.Layers = append(st.Layers, Layer{ID: "a"}) },
		func(st *Settings) { st.Layers = append(st.Layers, Layer{ID: "../b"}) },
		func(st *Settings) { st.Subscriptions = append(st.Subscriptions, Subscription{ID: "s2", Active: true}) },
		func(st *Settings) { st.Mihomo.Binary = "bin/mihomo" },
		func(st *Settings) { st.Layers[0].Stage = "guard" },
		func(st *Settings) { st.KernelChannel = "nightly" },
	}
	for i, f := range bad {
		if _, err := s.UpdateSettings(func(st *Settings) error { f(st); return nil }); err == nil {
			t.Errorf("bad settings %d accepted", i)
		}
	}
	st, _ = s.Settings()
	if len(st.Layers) != 1 || len(st.Subscriptions) != 1 {
		t.Errorf("a rejected update was saved: %+v", st)
	}
}

func TestHistory(t *testing.T) {
	s, _ := Open(t.TempDir())
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	var ids []string
	for i := range 5 {
		e, err := s.AddHistory(HistoryApplied, "test", []byte{byte('a' + i)}, t0.Add(time.Duration(i)*time.Minute), 3)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	idx, _ := s.History()
	if len(idx) != 3 || idx[0].ID != ids[4] || idx[2].ID != ids[2] {
		t.Fatalf("index: %+v", idx)
	}
	if _, err := os.Stat(s.Path("history", ids[0]+".yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pruned file still there")
	}
	b, e, err := s.ReadHistory(ids[3])
	if err != nil || string(b) != "d" || e.Hash != Hash([]byte("d")) || e.Size != 1 {
		t.Errorf("read: %q %+v %v", b, e, err)
	}
	if !strings.HasPrefix(ids[0], "20261126T100000Z-") {
		t.Errorf("id format: %s", ids[0])
	}
	// Same second, same content: unique id.
	e1, _ := s.AddHistory(HistoryBackup, "", []byte("z"), t0, 0)
	e2, _ := s.AddHistory(HistoryBackup, "", []byte("z"), t0, 0)
	if e1.ID == e2.ID || !strings.HasSuffix(e2.ID, "-2") {
		t.Errorf("ids %s %s", e1.ID, e2.ID)
	}
	if _, _, err := s.ReadHistory("../../etc/passwd"); err == nil {
		t.Error("traversal id accepted")
	}
	if _, _, err := s.ReadHistory(ids[0]); !errors.Is(err, ErrNotExist) {
		t.Errorf("pruned read: %v", err)
	}
}

func TestFileSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.json")
	var sec Secrets = &FileSecrets{Path: p}
	if _, err := sec.Get("x"); !errors.Is(err, ErrNoSecret) {
		t.Errorf("missing: %v", err)
	}
	if err := sec.Set(SubscriptionURLKey("s1"), "https://example.invalid/sub?token=1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := sec.Get("subscription-url/s1"); v != "https://example.invalid/sub?token=1" {
		t.Errorf("got %q", v)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	sec.Delete("subscription-url/s1")
	if _, err := sec.Get("subscription-url/s1"); !errors.Is(err, ErrNoSecret) {
		t.Errorf("after delete: %v", err)
	}
}
