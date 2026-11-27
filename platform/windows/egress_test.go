package windows

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

// The direct request leaves by Wi-Fi although mihomo's TUN holds 0/1 and
// 128/1: tunnels, the down Ethernet port and loopback are not candidates.
func TestEgressAdapter(t *testing.T) {
	s := coexistSys()
	for _, dest := range []string{"1.1.1.1", "200.1.2.3"} {
		a, ok := EgressAdapter(s.routes, s.adapters, ip(dest))
		if !ok || a.Name != "Wi-Fi" || a.Index != 11 {
			t.Errorf("%s: %+v %v", dest, a, ok)
		}
	}
	// IPv6 needs a global IPv6 address on the adapter.
	if a, ok := EgressAdapter(s.routes, s.adapters, ip("2001:db8::1")); ok {
		t.Errorf("v6 without a v6 address: %+v", a)
	}
	s.adapters[1].Addrs = append(s.adapters[1].Addrs, ip("2001:db8:1::20"))
	if a, ok := EgressAdapter(s.routes, s.adapters, ip("2001:db8::1")); !ok || a.Name != "Wi-Fi" {
		t.Errorf("v6: %+v %v", a, ok)
	}
	// A second up Ethernet port with a cheaper default route wins.
	s.adapters[2].Up, s.adapters[2].Addrs = true, ips("192.168.2.20")
	if a, _ := EgressAdapter(s.routes, s.adapters, ip("1.1.1.1")); a.Name != "Ethernet" {
		t.Errorf("cheaper Ethernet: %s", a.Name)
	}
	// Only tunnels left: none.
	tun := &fakeSys{adapters: coexistSys().adapters[3:], routes: coexistSys().routes}
	if a, ok := EgressAdapter(tun.routes, tun.adapters, ip("1.1.1.1")); ok {
		t.Errorf("tunnel picked: %+v", a)
	}
}

func TestDirectEgressPinsThePhysicalAdapter(t *testing.T) {
	var p egress.Path
	settings := health.DefaultSettings()
	settings.DirectDNS = ips("192.0.2.53")
	var dhcpGUID string
	e := New(Options{Sys: coexistSys(), Settings: &settings,
		DHCPDNS: func(guid string) []netip.Addr { dhcpGUID = guid; return ips("192.168.1.2") },
		Direct: func(_ context.Context, url string, got egress.Path) (netip.Addr, error) {
			p = got
			return ip("203.0.113.9"), nil
		}})
	a, err := e.DirectEgress(context.Background(), "https://echo.example/trace")
	got := p.Iface
	if err != nil || a != ip("203.0.113.9") || got != (egress.Iface{Index: 11, Name: "Wi-Fi"}) {
		t.Fatalf("%v %v %+v", a, err, got)
	}
	// Names resolve over Wi-Fi: the backup, Wi-Fi's DHCP servers, its
	// gateway; never mihomo's 127.0.0.1 the adapter is set to now.
	if dhcpGUID != "{00000000-0000-4000-8000-000000000011}" {
		t.Errorf("DHCP servers read for %q", dhcpGUID)
	}
	want := append(ips("192.0.2.53", "192.168.1.2", "192.168.1.1"), egress.PublicDNS...)
	if c := egress.DNSCandidates(p.DNS); fmt.Sprint(c) != fmt.Sprint(want) {
		t.Errorf("DNS %v, want %v", c, want)
	}
	// Socket option it sets for that adapter on Windows: IP_UNICAST_IF.
	o, err := egress.BindOption(health.Windows, "tcp4", got)
	if err != nil || o.Level != 0 || o.Name != 31 {
		t.Fatalf("%+v %v", o, err)
	}

	none := coexistSys()
	none.adapters = none.adapters[3:]
	e = New(Options{Sys: none, Direct: func(context.Context, string, egress.Path) (netip.Addr, error) {
		t.Fatal("dialed without a physical adapter")
		return netip.Addr{}, nil
	}})
	if _, err := e.DirectEgress(context.Background(), health.DefaultEgressURLs[0]); err == nil {
		t.Fatal("no error without a physical adapter")
	}
}

func TestProxiedEgressNeedsAPort(t *testing.T) {
	ctl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"meta":true,"version":"v1.19.0"}`)
		case "/configs":
			fmt.Fprint(w, `{"mode":"rule","mixed-port":0,"port":0}`)
		case "/proxies":
			fmt.Fprint(w, `{"proxies":{}}`)
		case "/rules":
			fmt.Fprint(w, `{"rules":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ctl.Close()
	e := New(Options{Sys: coexistSys(), Controller: strings.TrimPrefix(ctl.URL, "http://"),
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("no config") }})
	_, _, err := e.ProxiedEgress(context.Background(), health.DefaultEgressURLs[0])
	if err == nil || !strings.Contains(err.Error(), "mixed-port") {
		t.Fatal(err)
	}
}
