//go:build darwin

package main

import "testing"

// A dev or shot run (TWINNEL_CONTROLLER set) must never let the in-process
// health checks discover the live LaunchDaemon's config.
func TestDevConfigPath(t *testing.T) {
	t.Setenv("HOME", "/scratch/home")
	t.Setenv("TWINNEL_MIHOMO_CONFIG", "")
	t.Setenv("TWINNEL_CONTROLLER", "")
	if p := devConfigPath(); p != "" {
		t.Fatalf("normal run: got %q, want discovery", p)
	}
	t.Setenv("TWINNEL_CONTROLLER", "127.0.0.1:19894")
	if p := devConfigPath(); p != "/scratch/home/.config/mihomo/config.yaml" {
		t.Fatalf("dev run: got %q", p)
	}
	t.Setenv("TWINNEL_MIHOMO_CONFIG", "/scratch/conf.yaml")
	if p := devConfigPath(); p != "/scratch/conf.yaml" {
		t.Fatalf("explicit: got %q", p)
	}
}
