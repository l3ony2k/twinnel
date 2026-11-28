package main

import (
	"strings"
	"testing"
)

// A capture that returns a stale frame saves one picture under several
// names; the run must fail on it (ui-review 8.1).
func TestShotDigestsCatchRepeats(t *testing.T) {
	var d shotDigests
	if first := d.add("main-overview", []byte("A")); first != "" {
		t.Fatalf("first shot reported as a repeat of %s", first)
	}
	if first := d.add("main-health", []byte("B")); first != "" {
		t.Fatalf("different shot reported as a repeat of %s", first)
	}
	if err := d.err(); err != nil {
		t.Fatalf("distinct shots: %v", err)
	}
	if first := d.add("main-logs", []byte("A")); first != "main-overview" {
		t.Fatalf("repeat of main-overview reported as %q", first)
	}
	err := d.err()
	if err == nil || !strings.Contains(err.Error(), "main-logs = main-overview") {
		t.Fatalf("err = %v", err)
	}
	// Saving the same name again (a retake) is not a repeat, and a retake
	// that now differs clears the earlier finding.
	if first := d.add("main-health", []byte("B")); first != "" {
		t.Fatalf("retake reported as a repeat of %s", first)
	}
	d.add("main-logs", []byte("C"))
	if err := d.err(); err != nil {
		t.Fatalf("after the retake: %v", err)
	}
	// A retaken name frees its old picture for another name.
	d.add("main-health", []byte("D"))
	if first := d.add("main-proxies", []byte("B")); first != "" {
		t.Fatalf("B belonged to main-health's old take, got repeat of %s", first)
	}
}

// Only Windows shows shot windows (off screen, inactive) to capture them;
// macOS and Linux capture hidden windows and must stay as they are.
func TestShotShowOffscreen(t *testing.T) {
	for goos, want := range map[string]bool{"windows": true, "darwin": false, "linux": false} {
		if got := shotShowOffscreen(goos); got != want {
			t.Errorf("%s: %v, want %v", goos, got, want)
		}
	}
	if shotOffscreen > -10000 {
		t.Errorf("shot windows at %d may be on a display", shotOffscreen)
	}
}
