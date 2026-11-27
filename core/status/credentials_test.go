package status

import "testing"

func TestWithHelperCredentials(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c := Config{Controller: "127.0.0.1:9090", Secret: "typed", PrimaryGroup: "G"}
	got := c.WithHelperCredentials("127.0.0.1:19190", "", env(nil))
	if got.Controller != "127.0.0.1:19190" || got.Secret != "" || got.PrimaryGroup != "G" {
		t.Fatalf("helper wins (an empty secret too): %+v", got)
	}
	if got = c.WithHelperCredentials("", "x", env(nil)); got.Controller != "127.0.0.1:9090" || got.Secret != "x" {
		t.Fatalf("no address keeps c's: %+v", got)
	}
	if got = c.WithHelperCredentials("127.0.0.1:19190", "x", env(map[string]string{EnvController: "127.0.0.1:59999"})); got != c {
		t.Fatalf("TWINNEL_CONTROLLER pins: %+v", got)
	}
}
