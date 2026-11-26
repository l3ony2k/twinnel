package health

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEnv is a configurable Env. All data is synthetic.
type fakeEnv struct {
	platform string // "" means Darwin
	settings Settings
	routes   map[string]RouteLookup
	table    []Route
	ifs      []Interface
	res      []Resolver
	svcs     []NetworkService
	vpns     []VPN
	ts       *TailscaleStatus
	tsErr    error
	rt       *MihomoRuntime
	rtErr    error
	cfg      *MihomoConfig
	cfgErr   error
	mihomo   map[string]DNSAnswer
	system   map[string]DNSAnswer
	calls    atomic.Int32

	egressDirect, egressProxied netip.Addr
	egressChain                 []string
	egressErr                   error
}

func (f *fakeEnv) DirectEgress(context.Context, string) (netip.Addr, error) {
	return f.egressDirect, f.egressErr
}
func (f *fakeEnv) ProxiedEgress(context.Context, string) (netip.Addr, []string, error) {
	return f.egressProxied, f.egressChain, f.egressErr
}

func a(s string) netip.Addr { return netip.MustParseAddr(s) }

func healthyEnv() *fakeEnv {
	tun := TUNConfig{Enable: true, Device: "utun5", DNSHijack: []string{"any:53"},
		RouteExcludeAddress: []string{"192.168.0.0/16", "100.64.0.0/10", "fd7a:115c:a1e0::/48"},
		Inet4Address:        []string{"198.18.0.1/30"}}
	peers := []TailscalePeer{
		{HostName: "server", DNSName: "server.tn.ts.net.", TailscaleIPs: []netip.Addr{a("100.101.0.7")}, Online: true, Active: true, CurAddr: "203.0.113.9:41641", Relay: "hkg"},
		{HostName: "nas", DNSName: "nas.tn.ts.net.", TailscaleIPs: []netip.Addr{a("100.101.0.8")}, Online: true, Relay: "sea"},
		{HostName: "phone", DNSName: "phone.tn.ts.net.", TailscaleIPs: []netip.Addr{a("100.101.0.9")}, Online: false, Relay: "sea"},
	}
	rules := []MihomoRule{
		{0, "PROCESSNAME", "tailscaled", "DIRECT"},
		{1, "DOMAINSUFFIX", "tailscale.com", "DIRECT"},
		{2, "IPCIDR", "100.64.0.0/10", "DIRECT"},
		{3, "IPCIDR6", "fd7a:115c:a1e0::/48", "DIRECT"},
		{4, "DOMAIN", "laptop", "DIRECT"},
		{5, "DOMAIN", "server", "DIRECT"},
		{6, "DOMAIN", "nas", "DIRECT"},
		{7, "DOMAIN", "phone", "DIRECT"},
		{8, "GEOSITE", "google", "Google"},
		{9, "MATCH", "", "PROXY"},
	}
	settings := DefaultSettings()
	settings.Egress = true
	return &fakeEnv{
		settings:      settings,
		egressDirect:  a("198.51.100.7"),
		egressProxied: a("203.0.113.50"),
		egressChain:   []string{"node-a", "PROXY", "Final"},
		routes: map[string]RouteLookup{
			"100.100.100.100":    {Interface: "utun9"},
			"100.101.0.7":        {Interface: "utun9"},
			"100.101.0.8":        {Interface: "utun9"},
			"fd7a:115c:a1e0::53": {Interface: "utun9"},
			"1.1.1.1":            {Interface: "utun5", Gateway: "198.18.0.1"},
		},
		table: []Route{{Family: 4, Destination: "100.64/10", Interface: "utun9"}},
		ifs: []Interface{
			{Name: "en0", Up: true, Active: true, Addrs: []netip.Addr{a("192.168.1.20")}},
			{Name: "bridge0", Up: true, Active: false},
			{Name: "utun9", Up: true, Active: true, Addrs: []netip.Addr{a("100.101.0.6"), a("fd7a:115c:a1e0::6")}},
			{Name: "utun5", Up: true, Active: true, Addrs: []netip.Addr{a("198.18.0.1")}},
		},
		res: []Resolver{
			{Nameservers: []string{"127.0.0.1"}},
			{Domain: "tn.ts.net", Nameservers: []string{"100.100.100.100"}},
			{Domain: "64.100.in-addr.arpa", Nameservers: []string{"100.100.100.100"}},
		},
		svcs: []NetworkService{
			{Name: "Thunderbolt Bridge", Device: "bridge0", Enabled: true, Order: 1},
			{Name: "Wi-Fi", Device: "en0", Enabled: true, Order: 2, DNS: []string{"127.0.0.1"}},
			{Name: "Some VPN", Enabled: true, Order: 3},
		},
		vpns: []VPN{{Name: "Some VPN", BundleID: "com.example.vpn", Status: "Disconnected", Enabled: true}},
		ts: &TailscaleStatus{
			BackendState: "Running", TUN: true, MagicDNS: true, MagicDNSSuffix: "tn.ts.net",
			TailscaleIPs: []netip.Addr{a("100.101.0.6"), a("fd7a:115c:a1e0::6")},
			Self:         TailscalePeer{HostName: "laptop", DNSName: "laptop.tn.ts.net.", Addrs: []string{"203.0.113.5:41641"}},
			Peers:        peers,
		},
		rt: &MihomoRuntime{
			Version: "v1.19.0", Meta: true,
			Configs: MihomoRuntimeConfig{Mode: "rule", TUN: tun},
			Proxies: map[string]MihomoProxy{
				"DIRECT": {Name: "DIRECT", Type: "Direct"},
				"REJECT": {Name: "REJECT", Type: "Reject"},
				"node-a": {Name: "node-a", Type: "Shadowsocks"},
				"PROXY":  {Name: "PROXY", Type: "Selector", Now: "node-a", All: []string{"node-a", "REJECT"}},
				"Google": {Name: "Google", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "REJECT"}},
				"Meta":   {Name: "Meta", Type: "Selector", Now: "node-a", All: []string{"node-a", "REJECT"}},
			},
			Rules:    rules,
			AuthUsed: true,
		},
		cfg: &MihomoConfig{
			Path: "/tmp/config.yaml", ExternalController: "127.0.0.1:9090", SecretSet: true,
			DNS: MihomoDNSConfig{Enable: true, Listen: "127.0.0.1:53", EnhancedMode: "fake-ip", FakeIPRange: "198.18.0.1/16",
				NameserverPolicy: map[string][]string{"+.ts.net": {"100.100.100.100"}}},
			TUN:   tun,
			Rules: rules,
		},
		mihomo: map[string]DNSAnswer{
			"laptop.tn.ts.net": {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.6")}},
			"server.tn.ts.net": {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.7")}},
			"server":           {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.7")}},
			"nas.tn.ts.net":    {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.8")}},
			"nas":              {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.8")}},
		},
		system: map[string]DNSAnswer{
			"laptop.tn.ts.net": {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.6")}},
			"server.tn.ts.net": {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.7")}},
			"server":           {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.7")}},
			"nas.tn.ts.net":    {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.8")}},
			"nas":              {Rcode: "NOERROR", Addrs: []netip.Addr{a("100.101.0.8")}},
		},
	}
}

func (f *fakeEnv) Platform() string {
	if f.platform == "" {
		return Darwin
	}
	return f.platform
}
func (f *fakeEnv) Settings() Settings { return f.settings }
func (f *fakeEnv) RouteGet(_ context.Context, d string) (RouteLookup, error) {
	f.calls.Add(1)
	r, ok := f.routes[d]
	if !ok {
		return r, errors.New("no route")
	}
	return r, nil
}
func (f *fakeEnv) Routes(context.Context) ([]Route, error)         { return f.table, nil }
func (f *fakeEnv) Interfaces(context.Context) ([]Interface, error) { return f.ifs, nil }
func (f *fakeEnv) Resolvers(context.Context) ([]Resolver, error)   { return f.res, nil }
func (f *fakeEnv) NetworkServices(context.Context) ([]NetworkService, error) {
	return f.svcs, nil
}
func (f *fakeEnv) VPNs(context.Context) ([]VPN, error) { return f.vpns, nil }
func (f *fakeEnv) Tailscale(context.Context) (*TailscaleStatus, error) {
	f.calls.Add(1)
	return f.ts, f.tsErr
}
func (f *fakeEnv) MihomoRuntime(context.Context) (*MihomoRuntime, error) {
	f.calls.Add(1)
	if f.rtErr != nil {
		return nil, f.rtErr
	}
	return f.rt, nil
}
func (f *fakeEnv) MihomoConfig(context.Context) (*MihomoConfig, error) {
	if f.cfgErr != nil {
		return nil, f.cfgErr
	}
	return f.cfg, nil
}
func (f *fakeEnv) LookupMihomo(_ context.Context, n string) (DNSAnswer, error) {
	if r, ok := f.mihomo[n]; ok {
		return r, nil
	}
	return DNSAnswer{}, ErrDNSTimeout
}
func (f *fakeEnv) LookupSystem(_ context.Context, n string) (DNSAnswer, error) {
	if r, ok := f.system[n]; ok {
		return r, nil
	}
	return DNSAnswer{Rcode: "NXDOMAIN"}, nil
}

func run(t *testing.T, c Check, env Env) Result {
	t.Helper()
	r := c.Run(context.Background(), env)
	t.Logf("%s: %s %s\n  %s", c.ID(), r.Status, r.Summary, strings.Join(r.Evidence, "\n  "))
	return r
}

func TestHealthyAllOK(t *testing.T) {
	env := healthyEnv()
	for _, c := range DefaultRegistry().Checks() {
		if r := run(t, c, env); r.Status != StatusOK {
			t.Errorf("%s: want ok, got %s: %s", c.ID(), r.Status, r.Summary)
		}
	}
}

func want(t *testing.T, c Check, env Env, st Status, contains string) Result {
	t.Helper()
	r := run(t, c, env)
	if r.Status != st {
		t.Fatalf("%s: want %s, got %s: %s", c.ID(), st, r.Status, r.Summary)
	}
	all := r.Summary + "\n" + strings.Join(r.Evidence, "\n")
	if contains != "" && !strings.Contains(all, contains) {
		t.Fatalf("%s: %q not in %q", c.ID(), contains, all)
	}
	return r
}

func TestTailnetRoute(t *testing.T) {
	e := healthyEnv()
	e.routes["100.101.0.7"] = RouteLookup{Interface: "utun5", Gateway: "198.18.0.1"}
	r := want(t, TailnetRoute{}, e, StatusFail, "100.101.0.7")
	if r.Fix == nil {
		t.Fatal("no fix hint")
	}
	e = healthyEnv()
	e.ts.BackendState = "Stopped"
	want(t, TailnetRoute{}, e, StatusSkip, "Stopped")
	e = healthyEnv()
	e.tsErr = errors.New("no cli")
	want(t, TailnetRoute{}, e, StatusSkip, "no cli")
}

func TestTUNExclude(t *testing.T) {
	e := healthyEnv()
	e.rt.Configs.TUN.RouteExcludeAddress = []string{"100.64.0.0/10"}
	want(t, TUNExclude{}, e, StatusFail, "fd7a:115c:a1e0::/48")
	e = healthyEnv()
	e.rt.Configs.TUN.RouteExcludeAddress = []string{"100.0.0.0/8", "fc00::/7"}
	want(t, TUNExclude{}, e, StatusOK, "fc00::/7")
	e = healthyEnv()
	e.rt.Configs.TUN.Enable = false
	want(t, TUNExclude{}, e, StatusSkip, "")
	// Falls back to the config file when the controller is down.
	e = healthyEnv()
	e.rtErr = errors.New("refused")
	e.cfg.TUN.RouteExcludeAddress = nil
	want(t, TUNExclude{}, e, StatusFail, srcFile)
}

func TestTailscaleRules(t *testing.T) {
	e := healthyEnv()
	e.rt.Rules = append([]MihomoRule{{0, "DOMAINKEYWORD", "tailscale", "PROXY"}}, e.rt.Rules...)
	want(t, TailscaleRules{}, e, StatusFail, "controlplane.tailscale.com")

	e = healthyEnv()
	e.rt.Rules = append([]MihomoRule{{0, "RULESET", "proxy-list", "PROXY"}}, e.rt.Rules...)
	want(t, TailscaleRules{}, e, StatusWarn, "RULESET")

	e = healthyEnv()
	e.rt.Rules = []MihomoRule{{0, "MATCH", "", "PROXY"}}
	want(t, TailscaleRules{}, e, StatusFail, "tailscaled")
}

func TestMagicDNS(t *testing.T) {
	e := healthyEnv()
	e.mihomo["server"] = DNSAnswer{Rcode: "NOERROR", Addrs: []netip.Addr{a("198.18.6.32")}}
	delete(e.mihomo, "nas.tn.ts.net")
	e.cfg.DNS.NameserverPolicy["+.ts.net"] = []string{"system"}
	r := want(t, MagicDNS{}, e, StatusWarn, "fake-ip")
	if !strings.Contains(r.Summary, "nas.tn.ts.net") || !strings.Contains(r.Fix.Text, "system") {
		t.Fatalf("%+v", r)
	}

	e = healthyEnv()
	e.system["nas"] = DNSAnswer{Rcode: "NOERROR", Addrs: []netip.Addr{a("198.18.0.9")}}
	want(t, MagicDNS{}, e, StatusFail, "nas")

	e = healthyEnv()
	e.res = e.res[:1]
	want(t, MagicDNS{}, e, StatusFail, "resolver")

	e = healthyEnv()
	e.ts.MagicDNS = false
	want(t, MagicDNS{}, e, StatusSkip, "")
}

func TestPeerNames(t *testing.T) {
	e := healthyEnv()
	e.rt.Rules = append(e.rt.Rules[:6], MihomoRule{6, "DOMAIN", "oldbox", "DIRECT"})
	r := want(t, PeerNames{}, e, StatusWarn, "oldbox")
	if !strings.Contains(r.Summary, "nas") || !strings.Contains(r.Summary, "phone") {
		t.Fatal(r.Summary)
	}
	e = healthyEnv()
	e.rt.Rules = e.rt.Rules[:4]
	want(t, PeerNames{}, e, StatusWarn, msgNamesNone)
}

func TestSystemDNS(t *testing.T) {
	e := healthyEnv()
	e.svcs[1].DNS = nil
	e.res[0].Nameservers = []string{"8.8.8.8"}
	want(t, SystemDNS{}, e, StatusWarn, "dns-hijack")

	// DHCP DNS inside route-exclude-address is not hijacked.
	e = healthyEnv()
	e.svcs[1].DNS = nil
	e.res[0].Nameservers = []string{"192.168.1.1"}
	want(t, SystemDNS{}, e, StatusFail, "Wi-Fi")

	e = healthyEnv()
	e.svcs[1].DNS = []string{"1.1.1.1"}
	e.rt.Configs.TUN.DNSHijack = nil
	want(t, SystemDNS{}, e, StatusFail, "Wi-Fi")

	e = healthyEnv()
	e.ifs[0].Active = false
	want(t, SystemDNS{}, e, StatusWarn, msgDNSNoActive)
}

func TestVPNConflict(t *testing.T) {
	e := healthyEnv()
	e.vpns[0].Status = "Connected"
	want(t, VPNConflict{}, e, StatusFail, "Some VPN")

	e = healthyEnv()
	e.vpns = append(e.vpns, VPN{Name: "Tailscale", BundleID: "io.tailscale.ipn.macsys", Status: "Connected"})
	want(t, VPNConflict{}, e, StatusWarn, "NetworkExtension")

	e = healthyEnv()
	e.ts.ExitNode = &e.ts.Peers[0]
	want(t, VPNConflict{}, e, StatusFail, "exit node")
	e.settings.AllowExitNode = true
	want(t, VPNConflict{}, e, StatusWarn, "exit node")

	e = healthyEnv()
	e.routes["1.1.1.1"] = RouteLookup{Interface: "utun3"}
	want(t, VPNConflict{}, e, StatusFail, "utun3")
}

func TestDirectPeers(t *testing.T) {
	e := healthyEnv()
	e.ts.Peers[1].Active = true
	want(t, DirectPeers{}, e, StatusWarn, "nas")

	e = healthyEnv()
	e.settings.PreferredPeers = []string{"phone"}
	want(t, DirectPeers{}, e, StatusWarn, "phone")

	e = healthyEnv()
	e.ts.Self.Addrs = append(e.ts.Self.Addrs, "198.18.0.1:41641")
	want(t, DirectPeers{}, e, StatusOK, "198.18.0.1:41641")
}

func TestLeakGuard(t *testing.T) {
	e := healthyEnv()
	g := e.rt.Proxies["Google"]
	g.Now = "DIRECT"
	g.All = append(g.All, "DIRECT")
	e.rt.Proxies["Google"] = g
	want(t, LeakGuard{}, e, StatusFail, "Google → DIRECT")

	e = healthyEnv()
	e.rt.Proxies["Meta"] = MihomoProxy{Name: "Meta", Type: "Selector", Now: "node-a", All: []string{"node-a", "DIRECT"}}
	want(t, LeakGuard{}, e, StatusWarn, "Meta")

	e = healthyEnv()
	e.rt.Proxies["Auto"] = MihomoProxy{Name: "Auto", Type: "Fallback", Now: "node-a", All: []string{"node-a", "DIRECT"}}
	e.rt.Proxies["Meta"] = MihomoProxy{Name: "Meta", Type: "URLTest", Now: "Auto", All: []string{"Auto"}}
	want(t, LeakGuard{}, e, StatusFail, "Meta → Auto → DIRECT")

	e = healthyEnv()
	e.settings.SensitiveGroups = []string{"Nope"}
	want(t, LeakGuard{}, e, StatusWarn, "Nope")

	e = healthyEnv()
	e.rt.Configs.Mode = "global"
	want(t, LeakGuard{}, e, StatusFail, "global")
}

func TestController(t *testing.T) {
	e := healthyEnv()
	e.cfg.SecretSet = false
	e.rt.AuthUsed = false
	want(t, Controller{}, e, StatusWarn, "secret")

	e = healthyEnv()
	e.cfg.ExternalController = "0.0.0.0:9090"
	want(t, Controller{}, e, StatusFail, "0.0.0.0")

	e = healthyEnv()
	e.cfg.ExternalController = ":9090"
	want(t, Controller{}, e, StatusFail, "")

	e = healthyEnv()
	e.cfg.ExternalController = ""
	want(t, Controller{}, e, StatusOK, "")
}

// ---- runner / registry / export -------------------------------------------

type slowCheck struct{ d time.Duration }

func (slowCheck) ID() string          { return "slow" }
func (slowCheck) Title() string       { return "slow" }
func (slowCheck) Platforms() []string { return AllPlatforms }
func (s slowCheck) Run(ctx context.Context, _ Env) Result {
	select {
	case <-time.After(s.d):
	case <-ctx.Done():
	}
	return Result{Status: StatusOK}
}

type panicCheck struct{}

func (panicCheck) ID() string                      { return "panic" }
func (panicCheck) Title() string                   { return "panic" }
func (panicCheck) Platforms() []string             { return AllPlatforms }
func (panicCheck) Run(context.Context, Env) Result { panic("boom") }

type linuxOnly struct{}

func (linuxOnly) ID() string                      { return "linux-only" }
func (linuxOnly) Title() string                   { return "linux" }
func (linuxOnly) Platforms() []string             { return []string{Linux} }
func (linuxOnly) Run(context.Context, Env) Result { return Result{Status: StatusFail} }

func TestRunner(t *testing.T) {
	reg := DefaultRegistry()
	reg.Register(slowCheck{time.Second})
	reg.Register(panicCheck{})
	reg.Register(linuxOnly{})
	env := healthyEnv()
	rep := (&Runner{Registry: reg, Timeout: 50 * time.Millisecond, Concurrency: 3}).Run(context.Background(), env)
	if len(rep.Checks) != 16 {
		t.Fatalf("got %d checks", len(rep.Checks))
	}
	by := map[string]CheckReport{}
	for _, c := range rep.Checks {
		by[c.ID] = c
	}
	if by["slow"].Status != StatusWarn || by["panic"].Status != StatusFail || by["linux-only"].Status != StatusSkip {
		t.Fatalf("slow=%s panic=%s linux=%s", by["slow"].Status, by["panic"].Status, by["linux-only"].Status)
	}
	if rep.Overall != StatusFail {
		t.Fatal(rep.Overall)
	}
	if rep.Checks[0].ID != "tailnet-route" {
		t.Fatal("order not kept")
	}
	// Memo: Tailscale status and the runtime were fetched once despite
	// several checks needing them.
	if n := env.calls.Load(); n > 8 { // 5 route lookups + 1 tailscale + 1 runtime (+ slack)
		t.Fatalf("too many env calls: %d", n)
	}

	only := (&Runner{Registry: reg, Only: []string{"leak-guard"}}).Run(context.Background(), healthyEnv())
	if len(only.Checks) != 1 || only.Overall != StatusOK {
		t.Fatalf("%+v", only)
	}
}

func TestRegistryDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	r := NewRegistry()
	r.Register(Controller{})
	r.Register(Controller{})
}

func TestExport(t *testing.T) {
	e := healthyEnv()
	e.cfg.SecretSet = false
	rep := (&Runner{Registry: DefaultRegistry()}).Run(context.Background(), e)
	md := rep.Markdown()
	for _, s := range []string{"# " + titleReport, "## ⚠️ " + titleController, "```", labelFix} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown missing %q", s)
		}
	}
	txt := rep.Text()
	if !strings.Contains(txt, "[WARN] "+titleController) || !strings.Contains(txt, "[OK  ] "+titleTailnetRoute) {
		t.Error(txt)
	}
	b, err := rep.JSON()
	if err != nil || !strings.Contains(string(b), `"id": "controller-security"`) || !strings.Contains(string(b), `"overall": "warn"`) {
		t.Error(string(b), err)
	}
}

func TestHelpers(t *testing.T) {
	if NormRuleType("DOMAIN-SUFFIX") != "DOMAINSUFFIX" || NormRuleType("DomainSuffix") != "DOMAINSUFFIX" || NormRuleType("IP-CIDR6") != "IPCIDR6" {
		t.Fatal("NormRuleType")
	}
	if p := fakeIPRange(&MihomoConfig{DNS: MihomoDNSConfig{FakeIPRange: "198.18.0.1/16"}}); p.String() != "198.18.0.0/16" {
		t.Fatal(p)
	}
	if !hijacksAll53(TUNConfig{Enable: true, DNSHijack: []string{"tcp://any:53"}}) || hijacksAll53(TUNConfig{DNSHijack: []string{"any:53"}}) {
		t.Fatal("hijack")
	}
	if !isLoopbackHost("127.0.0.1") || !isLoopbackHost("::1") || isLoopbackHost("") || isLoopbackHost("0.0.0.0") {
		t.Fatal("loopback")
	}
	if m, _ := matchRule(MihomoRule{Type: "DOMAINSUFFIX", Payload: "tailscale.com"}, probe{domain: "tailscale.com.evil.org"}); m {
		t.Fatal("suffix must not match across labels")
	}
	if m, _ := matchRule(MihomoRule{Type: "DOMAINSUFFIX", Payload: "scale.com"}, probe{domain: "tailscale.com"}); m {
		t.Fatal("suffix must match whole labels")
	}
}
