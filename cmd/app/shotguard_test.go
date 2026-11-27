package main

import "testing"

func TestShotGuard(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	ok := map[string]string{"DUET_SHOTS": "/tmp/s", "DUET_SOCKET": "/scratch/duet.sock", "DUET_CONTROLLER": "127.0.0.1:19894"}
	if err := shotGuard(env(ok)); err != nil {
		t.Fatalf("scratch stack refused: %v", err)
	}
	if err := shotGuard(env(map[string]string{})); err != nil {
		t.Fatalf("normal run refused: %v", err)
	}
	for name, m := range map[string]map[string]string{
		"no socket":     {"DUET_SHOTS": "x", "DUET_CONTROLLER": "127.0.0.1:19894"},
		"live socket":   {"DUET_SHOTS": "x", "DUET_SOCKET": "/var/run/duet/helper.sock", "DUET_CONTROLLER": "127.0.0.1:19894"},
		"no controller": {"DUET_SHOTS": "x", "DUET_SOCKET": "/scratch/duet.sock"},
		"live ctl":      {"DUET_SHOTS": "x", "DUET_SOCKET": "/scratch/duet.sock", "DUET_CONTROLLER": "127.0.0.1:9090"},
		"bad ctl":       {"DUET_SHOTS": "x", "DUET_SOCKET": "/scratch/duet.sock", "DUET_CONTROLLER": "9090"},
	} {
		if shotGuard(env(m)) == nil {
			t.Errorf("%s: allowed", name)
		}
	}
}
