package main

import (
	"os"
	"path/filepath"
	"testing"
)

// scratchShotEnv is a complete shot environment on temp paths.
func scratchShotEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{
		"TWINNEL_SHOTS":                 filepath.Join(dir, "shots"),
		"TWINNEL_SOCKET":                filepath.Join(dir, "twinnel.sock"),
		"TWINNEL_CONTROLLER":            "127.0.0.1:19894",
		"TWINNEL_TAILSCALE_STATUS_FILE": "testdata/shots-tailscale-status.json",
		"HOME":                          filepath.Join(dir, "home"),
	}
}

func envFunc(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestShotGuard(t *testing.T) {
	old := realHome
	realHome = func() string { return "/Users/owner" }
	t.Cleanup(func() { realHome = old })

	if err := shotGuard(envFunc(scratchShotEnv(t))); err != nil {
		t.Fatalf("scratch stack refused: %v", err)
	}
	if err := shotGuard(envFunc(map[string]string{"HOME": "/Users/owner"})); err != nil {
		t.Fatalf("normal run refused: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	for name, change := range map[string]map[string]string{
		"no socket":      {"TWINNEL_SOCKET": ""},
		"live socket":    {"TWINNEL_SOCKET": "/var/run/twinnel/helper.sock"},
		"live socket 2":  {"TWINNEL_SOCKET": "/var/run/twinnel//helper.sock"},
		"no controller":  {"TWINNEL_CONTROLLER": ""},
		"live ctl":       {"TWINNEL_CONTROLLER": "127.0.0.1:9090"},
		"bad ctl":        {"TWINNEL_CONTROLLER": "9090"},
		"no tailnet":     {"TWINNEL_TAILSCALE_STATUS_FILE": ""},
		"missing tailnt": {"TWINNEL_TAILSCALE_STATUS_FILE": missing},
		"missing fixtr":  {"TWINNEL_HEALTH_FIXTURE": missing},
		"no home":        {"HOME": ""},
		"relative home":  {"HOME": "scratch"},
		"real home":      {"HOME": "/Users/owner/"},
	} {
		m := scratchShotEnv(t)
		for k, v := range change {
			m[k] = v
		}
		if shotGuard(envFunc(m)) == nil {
			t.Errorf("%s: allowed", name)
		}
	}
}

// The real home comes from the user database, not $HOME.
func TestRealHomeIgnoresHOME(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if h := realHome(); h == os.Getenv("HOME") {
		t.Fatalf("realHome followed $HOME: %q", h)
	}
}
