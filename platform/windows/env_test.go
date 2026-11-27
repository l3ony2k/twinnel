package windows

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
)

func ip(s string) netip.Addr    { return netip.MustParseAddr(s) }
func pfx(s string) netip.Prefix { return netip.MustParsePrefix(s) }
func ips(ss ...string) []netip.Addr {
	out := make([]netip.Addr, len(ss))
	for i, s := range ss {
		out[i] = ip(s)
	}
	return out
}

// fakeSys is a synthetic machine: Wi-Fi, a disconnected Ethernet port,
// Tailscale's wintun adapter and mihomo's TUN "Meta".
type fakeSys struct {
	adapters []Adapter
	routes   []RouteRow
	nrpt     []NRPTRule
	sources  map[string]string
}

func (f *fakeSys) Adapters(context.Context) ([]Adapter, error) { return f.adapters, nil }
func (f *fakeSys) Routes(context.Context) ([]RouteRow, error)  { return f.routes, nil }
func (f *fakeSys) NRPT(context.Context) ([]NRPTRule, error)    { return f.nrpt, nil }
func (f *fakeSys) DNSSource(guid string) string                { return f.sources[guid] }

func coexistSys() *fakeSys {
	return &fakeSys{
		adapters: []Adapter{
			{Index: 1, Name: "Loopback Pseudo-Interface 1", IfType: IfTypeLoopback, Up: true, Addrs: ips("127.0.0.1", "::1"), Metric4: 75},
			{Index: 11, GUID: "{00000000-0000-4000-8000-000000000011}", Name: "Wi-Fi", Description: "Example Wireless Adapter", IfType: IfTypeWiFi, Up: true,
				Addrs: ips("192.168.1.20"), DNS: ips("127.0.0.1"), Gateways: ips("192.168.1.1"), Metric4: 35, Metric6: 35},
			{Index: 12, GUID: "{00000000-0000-4000-8000-000000000012}", Name: "Ethernet", Description: "Example Gigabit Adapter", IfType: IfTypeEthernet,
				Up: false, DNS: ips("fec0:0:0:ffff::1"), Metric4: 25},
			{Index: 21, GUID: "{00000000-0000-4000-8000-000000000021}", Name: "Tailscale", Description: "Tailscale Tunnel", IfType: IfTypePropVirtual, Up: true,
				Addrs: ips("100.101.0.6", "fd7a:115c:a1e0::6"), DNS: ips("100.100.100.100"), DNSSuffix: "example.ts.net", Metric4: 5, Metric6: 5},
			{Index: 31, GUID: "{00000000-0000-4000-8000-000000000031}", Name: "Meta", Description: "Meta Tunnel", IfType: IfTypePropVirtual, Up: true,
				Addrs: ips("198.18.0.1"), Metric4: 0, Metric6: 0},
		},
		routes: []RouteRow{
			{Prefix: pfx("0.0.0.0/0"), NextHop: ip("192.168.1.1"), IfIndex: 11, Metric: 0},
			{Prefix: pfx("192.168.1.0/24"), IfIndex: 11, Metric: 256},
			{Prefix: pfx("0.0.0.0/0"), NextHop: ip("192.168.2.1"), IfIndex: 12, Metric: 0}, // down adapter
			{Prefix: pfx("0.0.0.0/1"), NextHop: ip("0.0.0.0"), IfIndex: 31, Metric: 0},
			{Prefix: pfx("128.0.0.0/1"), NextHop: ip("0.0.0.0"), IfIndex: 31, Metric: 0},
			{Prefix: pfx("100.64.0.0/10"), IfIndex: 21, Metric: 0},
			{Prefix: pfx("100.100.100.100/32"), IfIndex: 21, Metric: 0},
			{Prefix: pfx("fd7a:115c:a1e0::/48"), IfIndex: 21, Metric: 0},
			{Prefix: pfx("::/0"), NextHop: ip("fe80::1"), IfIndex: 11, Metric: 0},
		},
		nrpt:    []NRPTRule{{Namespaces: []string{".example.ts.net"}, Servers: []string{"100.100.100.100"}}},
		sources: map[string]string{"{00000000-0000-4000-8000-000000000011}": health.DNSSourceManual},
	}
}

func TestBestRoute(t *testing.T) {
	s := coexistSys()
	cases := []struct{ dest, iface, gw string }{
		{"1.1.1.1", "Meta", ""},                     // mihomo's /1 beats the /0
		{"200.1.2.3", "Meta", ""},                   // 128.0.0.0/1
		{"100.100.100.100", "Tailscale", ""},        // /32
		{"100.101.0.7", "Tailscale", ""},            // /10 beats mihomo's /1
		{"192.168.1.5", "Wi-Fi", ""},                // on-link
		{"fd7a:115c:a1e0::53", "Tailscale", ""},     // /48
		{"2001:db8::1", "Wi-Fi", "fe80::1"},         // v6 default
		{"::ffff:100.100.100.100", "Tailscale", ""}, // mapped
	}
	for _, c := range cases {
		r, a, ok := BestRoute(s.routes, s.adapters, ip(c.dest))
		if !ok || a.Name != c.iface {
			t.Errorf("%s: got %q (%v), want %q", c.dest, a.Name, ok, c.iface)
			continue
		}
		if gw := routeLookup(c.dest, r, a).Gateway; gw != c.gw {
			t.Errorf("%s: gateway %q, want %q", c.dest, gw, c.gw)
		}
	}

	// Metric: same prefix, route metric + interface metric decides.
	ads := []Adapter{{Index: 1, Name: "A", Up: true, Metric4: 50}, {Index: 2, Name: "B", Up: true, Metric4: 10}}
	routes := []RouteRow{{Prefix: pfx("0.0.0.0/0"), IfIndex: 1, Metric: 0}, {Prefix: pfx("0.0.0.0/0"), IfIndex: 2, Metric: 25}}
	if _, a, _ := BestRoute(routes, ads, ip("8.8.8.8")); a.Name != "B" {
		t.Errorf("metric: got %q", a.Name)
	}
	routes[1].Metric = 45
	if _, a, _ := BestRoute(routes, ads, ip("8.8.8.8")); a.Name != "A" {
		t.Errorf("metric: got %q", a.Name)
	}
	// Nothing up: no route.
	if _, _, ok := BestRoute(routes, []Adapter{{Index: 1}}, ip("8.8.8.8")); ok {
		t.Error("route through a down adapter")
	}
	// The tunnel flag.
	if rl := routeLookup("1.1.1.1", RouteRow{}, s.adapters[4]); !reflect.DeepEqual(rl.Flags, []string{health.RouteFlagTunnel}) {
		t.Errorf("flags: %v", rl.Flags)
	}
	if rl := routeLookup("1.1.1.1", RouteRow{}, s.adapters[1]); len(rl.Flags) != 0 {
		t.Errorf("flags: %v", rl.Flags)
	}
}

func TestAdapterTunnel(t *testing.T) {
	for _, c := range []struct {
		a    Adapter
		want bool
	}{
		{Adapter{IfType: IfTypeWiFi, Description: "Example Wireless"}, false},
		{Adapter{IfType: IfTypeEthernet, Description: "Hyper-V Virtual Ethernet Adapter"}, false},
		{Adapter{IfType: IfTypePropVirtual, Description: "Meta Tunnel"}, true},
		{Adapter{IfType: IfTypeTunnel}, true},
		{Adapter{IfType: IfTypePPP, Description: "WAN Miniport (PPTP)"}, true},
		{Adapter{IfType: IfTypeEthernet, Description: "TAP-Windows Adapter V9"}, true},
		{Adapter{IfType: IfTypeEthernet, Description: "WireGuard Tunnel"}, true},
	} {
		if got := c.a.Tunnel(); got != c.want {
			t.Errorf("%+v: %v", c.a, got)
		}
	}
}

func TestResolversAndServices(t *testing.T) {
	s := coexistSys()
	env := New(Options{Sys: s, ConfigPath: "x", ReadFile: func(string) ([]byte, error) { return nil, errors.New("none") }})
	ctx := context.Background()
	rs, err := env.Resolvers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var nrpt, tsSuffix, def bool
	for _, r := range rs {
		switch {
		case r.Domain == "example.ts.net" && !r.Scoped && reflect.DeepEqual(r.Nameservers, []string{"100.100.100.100"}):
			nrpt = true
		case r.Domain == "example.ts.net" && r.Scoped && r.IfIndex == "21":
			tsSuffix = true
		case r.Domain == "" && !r.Scoped:
			def = r.IfIndex == "11" && reflect.DeepEqual(r.Nameservers, []string{"127.0.0.1"})
		}
	}
	if !nrpt || !tsSuffix || !def {
		t.Errorf("resolvers (nrpt %v, ts suffix %v, default %v): %+v", nrpt, tsSuffix, def, rs)
	}

	svcs, err := env.NetworkServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 2 || svcs[0].Name != "Wi-Fi" || svcs[0].Order != 1 || !svcs[0].Enabled ||
		svcs[0].DNSSource != health.DNSSourceManual || !reflect.DeepEqual(svcs[0].DNS, []string{"127.0.0.1"}) ||
		svcs[1].Name != "Ethernet" || svcs[1].Enabled || len(svcs[1].DNS) != 0 || svcs[1].DNSSource != health.DNSSourceGlobal { // fec0:0:0:ffff::1 is a placeholder
		t.Errorf("services: %+v", svcs)
	}

	vpns, _ := env.VPNs(ctx)
	if len(vpns) != 0 {
		t.Errorf("vpns: %+v", vpns)
	}
	s.adapters = append(s.adapters, Adapter{Index: 41, Name: "wg-office", Description: "WireGuard Tunnel", IfType: IfTypePropVirtual, Up: true})
	vpns, _ = env.VPNs(ctx)
	if len(vpns) != 1 || vpns[0].Name != "wg-office" || !vpns[0].Connected() || !strings.Contains(vpns[0].Kind, "WireGuard") {
		t.Errorf("vpns: %+v", vpns)
	}
	if name, err := env.TailscaleInterface(ctx); err != nil || name != "Tailscale" {
		t.Errorf("tailscale interface: %q %v", name, err)
	}
}

const testConfig = `mixed-port: 7899
external-controller: 127.0.0.1:9090
secret: synthetic-secret
dns:
  enable: true
  listen: 127.0.0.1:53
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter: ['*', '+.example.ts.net']
  nameserver-policy:
    '+.example.ts.net': ['100.100.100.100#Tailscale']
tun:
  enable: true
  device: Meta
  auto-route: true
  dns-hijack: ['any:53']
  route-exclude-address: [100.64.0.0/10, 'fd7a:115c:a1e0::/48']
rules:
  - PROCESS-NAME,tailscaled.exe,DIRECT
  - DOMAIN-SUFFIX,tailscale.com,DIRECT
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - MATCH,PROXY
`

const testStatus = `{
  "Version": "1.106.0", "TUN": true, "BackendState": "Running",
  "TailscaleIPs": ["100.101.0.6", "fd7a:115c:a1e0::6"],
  "Self": {"HostName": "desk", "DNSName": "desk.example.ts.net.", "OS": "windows",
    "TailscaleIPs": ["100.101.0.6", "fd7a:115c:a1e0::6"], "Online": true},
  "MagicDNSSuffix": "example.ts.net",
  "CurrentTailnet": {"Name": "example", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
  "Peer": {
    "nodekey:1": {"HostName": "server", "DNSName": "server.example.ts.net.", "OS": "linux",
      "TailscaleIPs": ["100.101.0.7"], "Online": true, "Active": true}
  }
}`

func fakeController(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"meta":true,"version":"v1.19.0"}`))
		case "/configs":
			w.Write([]byte(`{"mixed-port":7899,"tun":{"enable":true,"device":"Meta","stack":"Mixed","dns-hijack":["any:53"],"auto-route":true,"route-exclude-address":["100.64.0.0/10","fd7a:115c:a1e0::/48"]},"allow-lan":false,"bind-address":"127.0.0.1","mode":"Rule"}`))
		case "/proxies":
			w.Write([]byte(`{"proxies":{"DIRECT":{"name":"DIRECT","type":"Direct"},"REJECT":{"name":"REJECT","type":"Reject"},"node-a":{"name":"node-a","type":"Shadowsocks"},"PROXY":{"name":"PROXY","type":"Selector","now":"node-a","all":["node-a","REJECT"]}}}`))
		case "/rules":
			w.Write([]byte(`{"rules":[{"index":0,"type":"ProcessName","payload":"tailscaled.exe","proxy":"DIRECT"},{"index":1,"type":"DomainSuffix","payload":"tailscale.com","proxy":"DIRECT"},{"index":2,"type":"IPCIDR","payload":"100.64.0.0/10","proxy":"DIRECT"},{"index":3,"type":"IPCIDR6","payload":"fd7a:115c:a1e0::/48","proxy":"DIRECT"},{"index":4,"type":"Match","payload":"","proxy":"PROXY"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testEnv(t *testing.T, s *fakeSys) *Env {
	srv := fakeController(t)
	names := map[string][]netip.Addr{
		"desk.example.ts.net":   ips("100.101.0.6"),
		"server.example.ts.net": ips("100.101.0.7"),
		"server":                ips("100.101.0.7"),
	}
	return New(Options{
		Sys:        s,
		Controller: strings.TrimPrefix(srv.URL, "http://"),
		ConfigPath: `C:\ProgramData\Twinnel\mihomo\config.yaml`,
		ReadFile:   func(string) ([]byte, error) { return []byte(testConfig), nil },
		DNSServer:  "127.0.0.1:1", // nothing listens: mihomo lookups fail fast
		TailscaleStatus: func(context.Context) ([]byte, error) {
			return []byte(testStatus), nil
		},
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("no commands in tests")
		},
		LookupHost: func(_ context.Context, n string) ([]netip.Addr, error) {
			if a, ok := names[n]; ok {
				return a, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
		},
	})
}

func runChecks(t *testing.T, env *Env) map[string]health.Result {
	rep := (&health.Runner{Registry: health.DefaultRegistry()}).Run(context.Background(), env)
	t.Log(rep.Text())
	out := map[string]health.Result{}
	for _, c := range rep.Checks {
		out[c.ID] = c.Result
	}
	return out
}

func TestChecksCoexist(t *testing.T) {
	env := testEnv(t, coexistSys())
	if a, err := env.LookupSystem(context.Background(), "nope.example"); err != nil || a.Rcode != "NXDOMAIN" {
		t.Fatalf("%+v %v", a, err)
	}
	if env.Platform() != health.Windows {
		t.Fatal(env.Platform())
	}
	byID := runChecks(t, env)
	for _, id := range []string{"tailnet-route", "tun-route-exclude", "tailscale-direct-rules", "vpn-conflict", "system-dns", "controller-security"} {
		if r := byID[id]; r.Status != health.StatusOK {
			t.Errorf("%s: %s %s %v", id, r.Status, r.Summary, r.Evidence)
		}
	}
	if r := byID["tailnet-route"]; !strings.Contains(r.Summary, "Tailscale") {
		t.Errorf("tailnet-route: %+v", r)
	}
	// The NRPT rule counts as the OS resolver for the MagicDNS suffix:
	// the system side passes (only mihomo's DNS, not listening here, warns).
	if r := byID["magicdns"]; r.Status == health.StatusFail || strings.Contains(r.Summary, "resolver") {
		t.Errorf("magicdns: %s %s %v", r.Status, r.Summary, r.Evidence)
	}
}

func TestChecksOtherVPNHoldsDefault(t *testing.T) {
	s := coexistSys()
	s.adapters = append(s.adapters, Adapter{Index: 41, Name: "wg-office", Description: "WireGuard Tunnel", IfType: IfTypePropVirtual, Up: true, Addrs: ips("10.9.0.2")})
	s.routes = append(s.routes,
		RouteRow{Prefix: pfx("0.0.0.0/1"), IfIndex: 41, Metric: 0},
		RouteRow{Prefix: pfx("128.0.0.0/1"), IfIndex: 41, Metric: 0},
	)
	// The WireGuard adapter's lower interface metric wins over Meta's.
	for i := range s.adapters {
		if s.adapters[i].Name == "Meta" {
			s.adapters[i].Metric4 = 5
		}
	}
	byID := runChecks(t, testEnv(t, s))
	r := byID["vpn-conflict"]
	if r.Status != health.StatusFail || !strings.Contains(r.Summary, "wg-office") {
		t.Errorf("vpn-conflict: %s %s %v", r.Status, r.Summary, r.Evidence)
	}
	if r := byID["tailnet-route"]; r.Status != health.StatusOK {
		t.Errorf("tailnet-route: %+v", r)
	}
}

// A config this process may not read is "access denied", not "not found".
func TestConfigNotRead(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	t.Setenv("USERPROFILE", t.TempDir())
	e := New(Options{ReadFile: func(p string) ([]byte, error) {
		if strings.Contains(p, "ProgramData") || strings.Contains(p, "Twinnel") {
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrPermission}
		}
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}})
	_, err := e.MihomoConfig(context.Background())
	if !errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), "no mihomo config found") {
		t.Fatalf("%v", err)
	}
	e = New(Options{ReadFile: func(p string) ([]byte, error) { return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist} }})
	if _, err := e.MihomoConfig(context.Background()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%v", err)
	}
}
