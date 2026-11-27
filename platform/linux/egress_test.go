package linux

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

// The fixture machine: eth0 has the cheapest default route but no carrier,
// mihomo's default route is in table 2022 on Meta; the request goes out
// wlan0.
func TestDirectEgressBindsThePhysicalLink(t *testing.T) {
	var got egress.Iface
	var p egress.Path
	up := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(up, []byte("# resolved upstream\nnameserver 192.0.2.53\n"), 0o600)
	settings := health.DefaultSettings()
	settings.DirectDNS = []netip.Addr{netip.MustParseAddr("198.51.100.53")}
	e := New(Options{Run: fakeRunner(t), UpstreamResolvConf: up, Settings: &settings, Direct: func(_ context.Context, _ string, gp egress.Path) (netip.Addr, error) {
		p, got = gp, gp.Iface
		return netip.MustParseAddr("203.0.113.9"), nil
	}})
	a, err := e.DirectEgress(context.Background(), health.DefaultEgressURLs[0])
	if err != nil || a.String() != "203.0.113.9" || got != (egress.Iface{Index: 3, Name: "wlan0"}) {
		t.Fatalf("%v %v %+v", a, err, got)
	}
	// Names resolve over wlan0: the backup, then resolved's upstream and
	// the gateway; never mihomo's 127.0.0.1 the link uses now.
	want := append([]netip.Addr{netip.MustParseAddr("198.51.100.53"), netip.MustParseAddr("192.0.2.53"), netip.MustParseAddr("192.0.2.1")}, egress.PublicDNS...)
	if c := egress.DNSCandidates(p.DNS); fmt.Sprint(c) != fmt.Sprint(want) {
		t.Errorf("DNS %v, want %v", c, want)
	}
	o, err := egress.BindOption(health.Linux, "tcp4", got)
	if err != nil || o.Level != 1 || o.Name != 25 || o.Str != "wlan0" {
		t.Fatalf("SO_BINDTODEVICE: %+v %v", o, err)
	}
}

func TestEgressLink(t *testing.T) {
	ls, err := ParseIPAddr(fixture(t, "ip_addr.json"))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := ParseIPRoutes(fixture(t, "ip_route_v4.json"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := EgressLink(rs, ls, true); !ok || l.Name != "wlan0" {
		t.Fatalf("%+v %v", l, ok)
	}
	// No IPv6 global address on a physical link: no IPv6 candidate.
	if l, ok := EgressLink(rs, ls, false); ok {
		t.Fatalf("v6: %+v", l)
	}
	// Only mihomo's table-2022 default left: none.
	var only []health.Route
	for _, r := range rs {
		if r.Interface == "Meta" {
			only = append(only, r)
		}
	}
	if l, ok := EgressLink(only, ls, true); ok {
		t.Fatalf("TUN picked: %+v", l)
	}
}
