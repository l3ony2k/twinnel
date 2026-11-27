package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/engine"
)

// `twinnel service log helper` prints the helper's log through the helper,
// redacted, and --since drops the older lines.
func TestServiceLogCommand(t *testing.T) {
	sock, e := importHelper(t)
	p := engine.HelperLogFile(e.Store)
	os.MkdirAll(filepath.Dir(p), 0o755)
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	recent := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	os.WriteFile(p, []byte("time="+old+" level=INFO msg=\"helper started\"\n"+
		"time="+recent+" level=WARN msg=fetch err=\"https://sub.example.invalid/s?token=synthetic-cli-token\"\n"), 0o644)

	out := withIO(t, "")
	if err := runService([]string{"log", "helper", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "helper started") || !strings.Contains(out.String(), "msg=fetch") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out.String(), "synthetic-cli-token") {
		t.Fatalf("not redacted:\n%s", out)
	}
	out.Reset()
	if err := runService([]string{"log", "helper", "--since", "10m", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "helper started") || !strings.Contains(out.String(), "msg=fetch") {
		t.Fatalf("--since 10m:\n%s", out)
	}
	if err := runService([]string{"log", "tailscaled", "--socket", sock}); err == nil {
		t.Fatal("tailscaled has no log here")
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got, err := parseSince("90s", now); err != nil || !got.Equal(now.Add(-90*time.Second)) {
		t.Fatal(got, err)
	}
	if got, err := parseSince("2026-01-01T00:00:00Z", now); err != nil || got.Year() != 2026 || got.Day() != 1 {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"yesterday", "-5m", "0s"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
