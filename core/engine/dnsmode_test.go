package engine

import (
	"context"
	"os"
	"testing"
	"time"
)

// N4 (Windows QA round 5): status.get reports the config file's DNS mode,
// which the helper can read where the app cannot, and reads it again
// when the file changes.
func TestStatusDNSMode(t *testing.T) {
	h := newHarness(t)
	st, err := h.e.Store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	path := st.Mihomo.ConfigFile
	write := func(body string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	for i, tc := range []struct{ body, want string }{
		{"mixed-port: 7890\ndns:\n  enable: true\n  enhanced-mode: fake-ip\n", "fake-ip"},
		{"mixed-port: 7890\ndns:\n  enable: true\n  enhanced-mode: Redir-Host\n", "redir-host"},
		{"mixed-port: 7890\ndns:\n  enable: true\n", "normal"},
		{"mixed-port: 7890\n", "off"},
		{"dns: [not, a, map\n", ""},
	} {
		write(tc.body, at.Add(time.Duration(i)*time.Minute))
		s, _ := h.e.Status(context.Background())
		if s.Mihomo.DNSMode != tc.want {
			t.Errorf("%q: dns mode %q, want %q", tc.body, s.Mihomo.DNSMode, tc.want)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.e.Status(context.Background()); s.Mihomo.DNSMode != "" {
		t.Errorf("no file: %q", s.Mihomo.DNSMode)
	}
}
