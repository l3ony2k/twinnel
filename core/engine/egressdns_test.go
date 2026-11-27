package engine

import (
	"fmt"
	"os"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// The egress test's direct request resolves over the servers recorded
// before Twinnel pointed the system DNS at mihomo; automatic (DHCP)
// services record none.
func TestEgressDirectDNS(t *testing.T) {
	h := newHarness(t)
	if got := h.e.EgressDirectDNS(); got != nil {
		t.Fatalf("no backup: %v", got)
	}
	backup := `{"at":"2026-11-28T00:00:00Z","services":[{"name":"Wi-Fi","servers":["192.0.2.53","2001:db8::53"]},{"name":"Ethernet","servers":[]},{"name":"x","servers":["bogus"]}]}`
	if err := os.WriteFile(h.e.Store.Path("dns-backup.json"), []byte(backup), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(h.e.EgressDirectDNS()); got != "[192.0.2.53 2001:db8::53]" {
		t.Fatalf("got %s", got)
	}
}

// settings.update keeps a valid endpoint list, refuses a bad one, and an
// empty list puts the built-in one back.
func TestSettingsEgressURLs(t *testing.T) {
	h := newHarness(t)
	urls := []string{"https://echo-a.example/ip", "https://echo-b.example/ip"}
	s, err := h.e.UpdateSettings(rpc.SettingsUpdate{HealthEgressURLs: &urls})
	if err != nil || fmt.Sprint(s.HealthEgressURLs) != fmt.Sprint(urls) {
		t.Fatal(s.HealthEgressURLs, err)
	}
	bad := []string{"http://echo.example/"}
	if _, err := h.e.UpdateSettings(rpc.SettingsUpdate{HealthEgressURLs: &bad}); err == nil {
		t.Fatal("http URL accepted")
	}
	if st, _ := h.e.Store.Settings(); len(st.HealthEgressURLs) != 2 {
		t.Fatalf("a refused update changed the list: %v", st.HealthEgressURLs)
	}
	none := []string{}
	if s, err := h.e.UpdateSettings(rpc.SettingsUpdate{HealthEgressURLs: &none}); err != nil || s.HealthEgressURLs != nil {
		t.Fatal(s.HealthEgressURLs, err)
	}
}
