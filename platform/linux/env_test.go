package linux

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseIPRoute(t *testing.T) {
	r, err := ParseIPRouteGet(fixture(t, "ip_route_get_quad100.json"))
	if err != nil || r.Interface != "tailscale0" || r.Gateway != "" || !reflect.DeepEqual(r.Flags, []string{"table 52"}) {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = ParseIPRouteGet(fixture(t, "ip_route_get_default.json"))
	if r.Interface != "Meta" || r.Gateway != "192.0.2.1" {
		t.Fatalf("%+v", r)
	}
	if _, err := ParseIPRouteGet([]byte("[]")); err == nil {
		t.Fatal("empty answer accepted")
	}
	rs, err := ParseIPRoutes(fixture(t, "ip_route_v4.json"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 6 {
		t.Fatalf("local/broadcast not dropped: %+v", rs)
	}
	if rs[1].Destination != "100.64.0.0/10" || rs[1].Interface != "tailscale0" || rs[1].Flags != "table 52" {
		t.Fatalf("%+v", rs[1])
	}
	if rs[3].Flags != "metric 600" || rs[3].Gateway != "192.0.2.1" {
		t.Fatalf("%+v", rs[3])
	}
	rs, _ = ParseIPRoutes(fixture(t, "ip_route_v6.json"), 6)
	if len(rs) != 1 || rs[0].Family != 6 || !strings.HasPrefix(rs[0].Destination, "fd7a:115c:a1e0::/48") {
		t.Fatalf("%+v", rs)
	}
}

func TestParseIPAddr(t *testing.T) {
	ls, err := ParseIPAddr(fixture(t, "ip_addr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 5 {
		t.Fatalf("%+v", ls)
	}
	eth, wlan, ts := ls[1], ls[2], ls[3]
	if !eth.Up || eth.Active || !eth.Physical() {
		t.Errorf("eth0: %+v", eth)
	}
	if !wlan.Up || !wlan.Active || len(wlan.Addrs) != 2 || wlan.Addrs[0].String() != "192.0.2.10" {
		t.Errorf("wlan0: %+v", wlan)
	}
	if ts.Physical() || ls[0].Physical() || ts.Index != 5 {
		t.Errorf("tun/loopback counted as physical: %+v %+v", ts, ls[0])
	}
}

func TestParseResolvectl(t *testing.T) {
	ls := ParseResolvectl(string(fixture(t, "resolvectl_status.txt")))
	if len(ls) != 5 {
		t.Fatalf("%+v", ls)
	}
	g, wlan, ts := ls[0], ls[2], ls[3]
	if g.Index != 0 || !g.DefaultRoute || !reflect.DeepEqual(g.Servers, []string{"127.0.0.1"}) {
		t.Errorf("global: %+v", g)
	}
	if wlan.Name != "wlan0" || !wlan.DefaultRoute || !reflect.DeepEqual(wlan.Servers, []string{"192.0.2.1", "2001:db8::1"}) || !reflect.DeepEqual(wlan.Domains, []string{"lan"}) {
		t.Errorf("wlan0: %+v", wlan)
	}
	if ts.DefaultRoute || !reflect.DeepEqual(ts.Domains, []string{"tailnet-example.ts.net"}) || len(ts.RouteDomains) != 3 || ts.RouteDomains[1] != "100.100.in-addr.arpa" {
		t.Errorf("tailscale0: %+v", ts)
	}
}

func TestResolversFrom(t *testing.T) {
	stub := ParseResolvConf(string(fixture(t, "resolv_stub.conf")))
	if !stub.Stub() || len(stub.Search) != 2 {
		t.Fatalf("%+v", stub)
	}
	rs := ResolversFrom(stub, ParseResolvectl(string(fixture(t, "resolvectl_status.txt"))))
	if rs[0].Domain != "" || rs[0].Scoped || !reflect.DeepEqual(rs[0].Nameservers, []string{"127.0.0.1"}) {
		t.Fatalf("default: %+v", rs[0])
	}
	found := map[string]string{}
	for _, r := range rs {
		found[r.Domain] = strings.Join(r.Nameservers, ",")
	}
	if found["tailnet-example.ts.net"] != "100.100.100.100" || found["101.100.in-addr.arpa"] != "100.100.100.100" || found["lan"] != "192.0.2.1,2001:db8::1" {
		t.Fatalf("%+v", rs)
	}

	plain := ParseResolvConf(string(fixture(t, "resolv_plain.conf")))
	if plain.Stub() || !reflect.DeepEqual(plain.Nameservers, []string{"127.0.0.1", "192.0.2.1"}) {
		t.Fatalf("%+v", plain)
	}
	rs = ResolversFrom(plain, nil)
	if len(rs) != 2 || rs[0].Domain != "" || rs[1].Domain != "lan" {
		t.Fatalf("%+v", rs)
	}
}

func TestNmcli(t *testing.T) {
	cs := ParseNmcliConnections(string(fixture(t, "nmcli_con.txt")))
	if len(cs) != 5 || cs[0].Name != "Home: 5 GHz" || cs[0].Device != "wlan0" || cs[1].State != "" {
		t.Fatalf("%+v", cs)
	}
	vs := VPNsFromNM(cs)
	if len(vs) != 2 || vs[0].Name != "Office VPN" || vs[0].Connected() || !vs[1].Connected() || vs[1].BundleID != "nm.wireguard" {
		t.Fatalf("%+v", vs)
	}
}

func TestParseNmcliDNSSource(t *testing.T) {
	cases := map[string]string{
		"auto\n192.0.2.1\nyes\n\nno\n":          health.DNSSourceManual,
		"manual\n192.0.2.1\nno\n\nno\n":         health.DNSSourceManual,
		"auto\n\nno\n2001\\:db8\\:\\:1\nyes\n":  health.DNSSourceManual,
		"auto\n192.0.2.1,192.0.2.2\nno\n\nno\n": health.DNSSourceMixed,
		"auto\n\nno\n\nno\n":                    health.DNSSourceAuto,
		"manual\n\nno\n\nno\n":                  health.DNSSourceGlobal,
		"auto\n":                                "",
	}
	for in, want := range cases {
		if got := ParseNmcliDNSSource(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseGetent(t *testing.T) {
	a := ParseGetent("server", string(fixture(t, "getent_peer.txt")))
	if a.Rcode != "NOERROR" || len(a.Addrs) != 1 || a.Addrs[0].String() != "100.101.0.7" {
		t.Fatalf("%+v", a)
	}
	if a := ParseGetent("x", ""); a.Rcode != "NXDOMAIN" {
		t.Fatalf("%+v", a)
	}
}

func TestScanUnits(t *testing.T) {
	dirs := []string{"testdata/units/etc", "testdata/units/lib"}
	svcs := ScanUnits(dirs, nil)
	if len(svcs) != 2 {
		t.Fatalf("%+v", svcs)
	}
	m, ts := svcs[0], svcs[1]
	if m.Label != "mihomo" || m.Kind != "systemd" || m.Program[0] != "/usr/local/bin/mihomo" || m.Path != "testdata/units/etc/mihomo.service" {
		t.Errorf("mihomo (the /etc unit must hide the /lib one): %+v", m)
	}
	if ts.Label != "tailscaled" || ts.Program[0] != "/usr/sbin/tailscaled" {
		t.Errorf("tailscaled: %+v", ts)
	}
	if got := DiscoverConfigPath(dirs, nil); got != "/etc/mihomo/live.yaml" {
		t.Errorf("config path: %s", got)
	}
	if kindOf("/home/u/.config/systemd/user") != "systemd-user" {
		t.Error("user unit kind")
	}
}

// fakeRunner answers commands from fixtures.
func fakeRunner(t *testing.T) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		switch cmd {
		case "ip -j route get 1.1.1.1":
			return fixture(t, "ip_route_get_default.json"), nil
		case "ip -j route get 100.100.100.100", "ip -j route get 100.101.0.7", "ip -j route get 100.101.0.9", "ip -j route get fd7a:115c:a1e0::53":
			return fixture(t, "ip_route_get_quad100.json"), nil
		case "ip -j -4 route show table all":
			return fixture(t, "ip_route_v4.json"), nil
		case "ip -j -6 route show table all":
			return fixture(t, "ip_route_v6.json"), nil
		case "ip -j -4 route show default":
			return fixture(t, "ip_route_default.json"), nil
		case "ip -j addr show":
			return fixture(t, "ip_addr.json"), nil
		case "resolvectl status --no-pager":
			return fixture(t, "resolvectl_status.txt"), nil
		case "nmcli -t -f NAME,UUID,TYPE,DEVICE,STATE connection show":
			return fixture(t, "nmcli_con.txt"), nil
		case "nmcli -g " + NMCLIDNSFields + " connection show uuid 7d4c1b8e-0000-4000-8000-000000000001":
			return fixture(t, "nmcli_dns_wlan0.txt"), nil
		case "/usr/bin/tailscale status --json":
			return fixture(t, "tailscale_status.json"), nil
		}
		if name == "getent" {
			if strings.HasPrefix(args[1], "server") {
				return fixture(t, "getent_peer.txt"), nil
			}
			return nil, &ExitError{Name: "getent", Code: 2}
		}
		return nil, errors.New("unexpected command: " + cmd)
	}
}

func TestEnvWithFakes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"meta":true,"version":"v1.19.0"}`))
		case "/configs":
			w.Write(fixture(t, "mihomo_configs.json"))
		case "/proxies":
			w.Write(fixture(t, "mihomo_proxies.json"))
		case "/rules":
			w.Write(fixture(t, "mihomo_rules.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	files := map[string]string{
		"/etc/resolv.conf":      "resolv_stub.conf",
		"/etc/mihomo/live.yaml": "units/mihomo_live.yaml",
	}
	readFile := func(p string) ([]byte, error) {
		if f, ok := files[p]; ok {
			return fixture(t, f), nil
		}
		return os.ReadFile(p)
	}
	lookPath := func(p string) (string, error) {
		switch p {
		case "nmcli", "/usr/bin/tailscale":
			return p, nil
		}
		return "", errors.New("not found")
	}
	env := New(Options{
		Controller: strings.TrimPrefix(srv.URL, "http://"),
		Run:        fakeRunner(t), ReadFile: readFile, LookPath: lookPath,
		UnitDirs:  []string{"testdata/units/etc", "testdata/units/lib"},
		DNSServer: "127.0.0.1:1", // nothing listens: mihomo lookups fail fast
	})
	ctx := context.Background()

	svcs, err := env.NetworkServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 2 || svcs[0].Device != "eth0" || svcs[1].Name != "Home: 5 GHz" || svcs[1].Order != 2 ||
		!reflect.DeepEqual(svcs[1].DNS, []string{"192.0.2.1", "2001:db8::1"}) || len(svcs[0].DNS) != 0 ||
		svcs[1].DNSSource != health.DNSSourceMixed || svcs[0].DNSSource != health.DNSSourceGlobal {
		t.Fatalf("services: %+v", svcs)
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil || cfg.Path != "/etc/mihomo/live.yaml" || cfg.TUN.Device != "Meta" {
		t.Fatalf("config: %+v %v", cfg, err)
	}
	rt, err := env.MihomoRuntime(ctx)
	if err != nil || rt.Version != "v1.19.0" || len(rt.Rules) == 0 {
		t.Fatalf("runtime: %+v %v", rt, err)
	}
	if a, err := env.LookupSystem(ctx, "nope.example"); err != nil || a.Rcode != "NXDOMAIN" {
		t.Fatalf("%+v %v", a, err)
	}
	vpns, _ := env.VPNs(ctx)
	if len(vpns) != 2 {
		t.Fatalf("%+v", vpns)
	}

	// The whole check suite runs against the Linux Env without errors.
	rep := (&health.Runner{Registry: health.DefaultRegistry()}).Run(ctx, env)
	t.Log(rep.Text())
	byID := map[string]health.Result{}
	for _, c := range rep.Checks {
		byID[c.ID] = c.Result
	}
	if r := byID["tailnet-route"]; r.Status != health.StatusOK || !strings.Contains(r.Summary, "tailscale0") {
		t.Errorf("tailnet-route: %+v", r)
	}
	// Default route held by another TUN while mihomo's is "Meta": the
	// fixture's /configs reports device utun5, so this must not pass.
	if r := byID["vpn-conflict"]; r.Status == health.StatusOK {
		t.Errorf("vpn-conflict: %+v", r)
	}
	// wlan0 resolves through 192.0.2.1, not mihomo; dns-hijack any:53 covers it.
	if r := byID["system-dns"]; r.Status != health.StatusWarn {
		t.Errorf("system-dns: %+v", r)
	}
	for id, r := range byID {
		for _, ev := range r.Evidence {
			if strings.Contains(ev, "unexpected command") {
				t.Errorf("%s ran an unexpected command: %s", id, ev)
			}
		}
	}
}

// Finding 13: with the default reader the config is read confined to its dir.
func TestMihomoConfigRefusesEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "conf")
	os.Mkdir(conf, 0o755)
	os.WriteFile(filepath.Join(dir, "secret"), []byte("external-controller: 127.0.0.1:1\n"), 0o600)
	link := filepath.Join(conf, "config.yaml")
	if err := os.Symlink(filepath.Join(dir, "secret"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{ConfigPath: link, UnitDirs: []string{}}).MihomoConfig(context.Background()); err == nil {
		t.Fatal("read a config through a symlink leaving its dir")
	}
}

func TestTailscaleStatusOverride(t *testing.T) {
	ran := false
	e := New(Options{
		TailscaleBin: "/nonexistent/tailscale",
		Run: func(context.Context, string, ...string) ([]byte, error) {
			ran = true
			return nil, errors.New("live read")
		},
		TailscaleStatus: func(context.Context) ([]byte, error) { return fixture(t, "tailscale_status.json"), nil },
	})
	st, err := e.Tailscale(context.Background())
	if err != nil || ran || st.BackendState == "" {
		t.Fatalf("%+v %v ran=%v", st, err, ran)
	}
}

// eth0 is up without carrier (NO-CARRIER), wlan0 is connected: only
// wlan0 counts for the system-dns verdict (sysdns.LinkConnected, the
// rule dns.status shares), so the unplugged port never decides it.
func TestConnectedDevicesSkipsNoCarrier(t *testing.T) {
	env := New(Options{Run: fakeRunner(t)})
	got := health.ConnectedDevices(context.Background(), env)
	if got["eth0"] || !got["wlan0"] {
		t.Fatalf("connected: %v", got)
	}
}
