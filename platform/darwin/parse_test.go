//go:build darwin

package darwin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseRouteGet(t *testing.T) {
	r := ParseRouteGet(read(t, "route_get_tailnet.txt"))
	if r.Interface != "utun9" || r.Destination != "100.100.100.100" || r.Gateway != "" || len(r.Flags) != 6 {
		t.Fatalf("tailnet: %+v", r)
	}
	r = ParseRouteGet(read(t, "route_get_default.txt"))
	if r.Interface != "utun5" || r.Gateway != "198.18.0.1" {
		t.Fatalf("default: %+v", r)
	}
}

func TestParseNetstat(t *testing.T) {
	rs := ParseNetstat(read(t, "netstat_rn.txt"))
	var v4, v6 int
	found := false
	for _, r := range rs {
		if r.Family == 4 {
			v4++
		} else {
			v6++
		}
		if r.Destination == "100.64/10" && r.Interface == "utun9" {
			found = true
		}
	}
	if v4 != 9 || v6 != 4 || !found {
		t.Fatalf("v4=%d v6=%d found=%v: %+v", v4, v6, found, rs)
	}
}

func TestParseIfconfig(t *testing.T) {
	ifs := ParseIfconfig(read(t, "ifconfig.txt"))
	by := map[string]int{}
	for i, x := range ifs {
		by[x.Name] = i
	}
	if len(ifs) != 6 {
		t.Fatalf("got %d interfaces", len(ifs))
	}
	en0 := ifs[by["en0"]]
	if !en0.Up || !en0.Active || len(en0.Addrs) != 2 || en0.Addrs[1] != netip.MustParseAddr("192.168.1.20") {
		t.Fatalf("en0: %+v", en0)
	}
	if b := ifs[by["bridge0"]]; b.Active {
		t.Fatalf("bridge0 should be inactive: %+v", b)
	}
	u9 := ifs[by["utun9"]]
	if !u9.Active || u9.Addrs[0] != netip.MustParseAddr("100.101.0.6") || u9.Addrs[2] != netip.MustParseAddr("fd7a:115c:a1e0::aa:bb") {
		t.Fatalf("utun9: %+v", u9)
	}
}

func TestParseScutilDNS(t *testing.T) {
	rs := ParseScutilDNS(read(t, "scutil_dns.txt"))
	if len(rs) != 6 {
		t.Fatalf("got %d resolvers", len(rs))
	}
	if rs[0].Domain != "" || rs[0].Nameservers[0] != "127.0.0.1" || rs[0].SearchDomains[0] != "lan" || rs[0].Scoped {
		t.Fatalf("default: %+v", rs[0])
	}
	if rs[3].Domain != "tailnet-example.ts.net" || !reflect.DeepEqual(rs[3].Nameservers, []string{"100.100.100.100", "fd7a:115c:a1e0::53"}) {
		t.Fatalf("tailnet: %+v", rs[3])
	}
	if !rs[5].Scoped || rs[5].IfIndex != "14 (en0)" {
		t.Fatalf("scoped: %+v", rs[5])
	}
}

func TestParseNCList(t *testing.T) {
	vs := ParseNCList(read(t, "scutil_nc_list.txt"))
	if len(vs) != 3 {
		t.Fatalf("got %+v", vs)
	}
	if vs[0].Name != "Example VPN" || vs[0].BundleID != "com.example.vpn" || vs[0].Connected() || !vs[0].Enabled {
		t.Fatalf("0: %+v", vs[0])
	}
	if vs[1].BundleID != "io.tailscale.ipn.macsys" || !vs[1].Connected() {
		t.Fatalf("1: %+v", vs[1])
	}
	if vs[2].Name != "Office L2TP" || vs[2].Enabled {
		t.Fatalf("2: %+v", vs[2])
	}
}

func TestParseServiceOrder(t *testing.T) {
	ss := ParseServiceOrder(read(t, "service_order.txt"))
	if len(ss) != 4 {
		t.Fatalf("got %+v", ss)
	}
	if ss[1].Name != "Wi-Fi" || ss[1].Device != "en0" || ss[1].Order != 2 || !ss[1].Enabled {
		t.Fatalf("wifi: %+v", ss[1])
	}
	if ss[2].Enabled || ss[2].Device != "en7" {
		t.Fatalf("disabled: %+v", ss[2])
	}
	if ss[3].Device != "" {
		t.Fatalf("vpn: %+v", ss[3])
	}
}

func TestParseGetDNSServers(t *testing.T) {
	if ns := ParseGetDNSServers("127.0.0.1\n::1\n"); !reflect.DeepEqual(ns, []string{"127.0.0.1", "::1"}) {
		t.Fatal(ns)
	}
	if ns := ParseGetDNSServers("There aren't any DNS Servers set on Wi-Fi.\n"); ns != nil {
		t.Fatal(ns)
	}
}

func TestParseDscacheutil(t *testing.T) {
	a := ParseDscacheutil("peer-a", read(t, "dscacheutil.txt"))
	if a.Rcode != "NOERROR" || len(a.Addrs) != 2 || a.Addrs[0] != netip.MustParseAddr("100.101.0.7") {
		t.Fatalf("%+v", a)
	}
	if a := ParseDscacheutil("x", ""); a.Rcode != "NXDOMAIN" || len(a.Addrs) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestParseTailscaleStatus(t *testing.T) {
	st, err := ParseTailscaleStatus([]byte(read(t, "tailscale_status.json")))
	if err != nil {
		t.Fatal(err)
	}
	if st.BackendState != "Running" || !st.MagicDNS || st.MagicDNSSuffix != "tailnet-example.ts.net" || len(st.TailscaleIPs) != 2 {
		t.Fatalf("%+v", st)
	}
	if len(st.Peers) != 3 || st.Peers[0].ShortName() != "phone" || st.Peers[2].ShortName() != "server" {
		t.Fatalf("peers not sorted: %+v", st.Peers)
	}
	if st.ExitNode == nil || st.ExitNode.HostName != "router" {
		t.Fatalf("exit node: %+v", st.ExitNode)
	}
	if st.Self.ShortName() != "laptop" || len(st.Self.Addrs) != 2 {
		t.Fatalf("self: %+v", st.Self)
	}
}

func TestParseMihomoAPI(t *testing.T) {
	c, err := ParseMihomoConfigs([]byte(read(t, "mihomo_configs.json")))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "rule" || !c.TUN.Enable || c.TUN.Device != "utun5" || len(c.TUN.RouteExcludeAddress) != 3 || len(c.TUN.DNSHijack) != 2 {
		t.Fatalf("%+v", c)
	}
	px, err := ParseMihomoProxies([]byte(read(t, "mihomo_proxies.json")))
	if err != nil {
		t.Fatal(err)
	}
	if g := px["Google"]; !g.IsGroup() || g.Now != "PROXY" || len(g.All) != 3 {
		t.Fatalf("%+v", g)
	}
	if px["DIRECT"].IsGroup() {
		t.Fatal("DIRECT is not a group")
	}
	rs, err := ParseMihomoRules([]byte(read(t, "mihomo_rules.json")))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 6 || rs[0].Type != "PROCESSNAME" || rs[1].Type != "DOMAINSUFFIX" || rs[5].Type != "MATCH" || rs[5].Target != "PROXY" {
		t.Fatalf("%+v", rs)
	}
}

func TestParseMihomoConfigFile(t *testing.T) {
	c, secret, err := ParseMihomoConfigFile("x.yaml", []byte(read(t, "mihomo_config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" || c.SecretSet || c.ExternalController != "127.0.0.1:9090" {
		t.Fatalf("%+v", c)
	}
	if !c.DNS.Enable || c.DNS.Listen != "127.0.0.1:53" || c.DNS.FakeIPRange != "198.18.0.1/16" {
		t.Fatalf("dns: %+v", c.DNS)
	}
	if !reflect.DeepEqual(c.DNS.NameserverPolicy["+.ts.net"], []string{"system"}) ||
		!reflect.DeepEqual(c.DNS.NameserverPolicy["+.lan"], []string{"system", "192.168.1.1"}) {
		t.Fatalf("policy: %+v", c.DNS.NameserverPolicy)
	}
	if len(c.Rules) != 4 || c.Rules[1].Payload != "100.64.0.0/10" || c.Rules[1].Target != "DIRECT" ||
		c.Rules[2].Type != "AND" || c.Rules[2].Target != "REJECT" || c.Rules[3].Type != "MATCH" || c.Rules[3].Target != "PROXY" {
		t.Fatalf("rules: %+v", c.Rules)
	}
	if len(c.ProxyGroups) != 1 || c.ProxyGroups[0].Name != "PROXY" {
		t.Fatalf("groups: %+v", c.ProxyGroups)
	}

	_, secret, _ = ParseMihomoConfigFile("x", []byte("secret: s3cret\n"))
	if secret != "s3cret" {
		t.Fatal("secret not returned")
	}
}

func TestConfigFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"/opt/m/bin/mihomo", "-d", "/opt/m/conf", "-f", "/opt/m/conf/config.yaml"}, "/opt/m/conf/config.yaml"},
		{[]string{"/opt/m/bin/mihomo", "-d", "/opt/m/conf"}, "/opt/m/conf/config.yaml"},
		{[]string{"/opt/m/bin/mihomo", "-d=/c", "-f=x.yaml"}, "/c/x.yaml"},
		{[]string{"/usr/bin/other", "-d", "/c"}, ""},
	}
	for _, c := range cases {
		if got := ConfigFromArgs(c.args); got != c.want {
			t.Errorf("%v: got %q want %q", c.args, got, c.want)
		}
	}
}

// fakeRunner serves canned command output.
func fakeRunner(t *testing.T, outputs map[string]string) CommandRunner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := filepath.Base(name) + " " + strings.Join(args, " ")
		out, ok := outputs[key]
		if !ok {
			return nil, errors.New("unexpected command: " + key)
		}
		return []byte(out), nil
	}
}

func TestEnvWithFakes(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request %s", r.Method)
		}
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"meta":true,"version":"v1.19.0"}`))
		case "/configs":
			w.Write([]byte(read(t, "mihomo_configs.json")))
		case "/proxies":
			w.Write([]byte(read(t, "mihomo_proxies.json")))
		case "/rules":
			w.Write([]byte(read(t, "mihomo_rules.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(cfgPath, []byte("secret: abc\ndns:\n  enable: true\n  listen: 0.0.0.0:1053\n"), 0o600)

	e := New(Options{
		Controller:   strings.TrimPrefix(srv.URL, "http://"),
		ConfigPath:   cfgPath,
		TailscaleBin: "/fake/tailscale",
		Run: fakeRunner(t, map[string]string{
			"route -n get 100.100.100.100":          read(t, "route_get_tailnet.txt"),
			"netstat -rn":                           read(t, "netstat_rn.txt"),
			"ifconfig ":                             read(t, "ifconfig.txt"),
			"scutil --dns":                          read(t, "scutil_dns.txt"),
			"scutil --nc list":                      read(t, "scutil_nc_list.txt"),
			"networksetup -listnetworkserviceorder": read(t, "service_order.txt"),
			"networksetup -getdnsservers Thunderbolt Bridge": "There aren't any DNS Servers set on Thunderbolt Bridge.\n",
			"networksetup -getdnsservers Wi-Fi":              "127.0.0.1\n",
			"networksetup -getdnsservers USB LAN":            "There aren't any DNS Servers set on USB LAN.\n",
			"tailscale status --json":                        read(t, "tailscale_status.json"),
			"dscacheutil -q host -a name peer-a":             read(t, "dscacheutil.txt"),
		}),
	})
	ctx := context.Background()
	if r, err := e.RouteGet(ctx, "100.100.100.100"); err != nil || r.Interface != "utun9" {
		t.Fatal(r, err)
	}
	svcs, err := e.NetworkServices(ctx)
	if err != nil || len(svcs) != 4 || svcs[1].DNS[0] != "127.0.0.1" || svcs[0].DNS != nil {
		t.Fatal(svcs, err)
	}
	if st, err := e.Tailscale(ctx); err != nil || len(st.Peers) != 3 {
		t.Fatal(st, err)
	}
	if a, err := e.LookupSystem(ctx, "peer-a"); err != nil || len(a.Addrs) != 2 {
		t.Fatal(a, err)
	}
	rt, err := e.MihomoRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rt.AuthUsed || rt.Version != "v1.19.0" || len(rt.Rules) != 6 || len(rt.Proxies) != 6 {
		t.Fatalf("%+v", rt)
	}
	for _, a := range auth {
		if a != "Bearer abc" {
			t.Fatalf("auth header %q", a)
		}
	}
	if s := e.dnsServer(ctx); s != "127.0.0.1:1053" {
		t.Fatalf("dns server %s", s)
	}
}
