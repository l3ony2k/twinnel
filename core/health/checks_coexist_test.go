package health

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func bindEnv() *fakeEnv {
	env := healthyEnv()
	env.cfg.TUN.AutoDetectInterface = true
	env.cfg.DNS.NameserverPolicy = map[string][]string{
		"+.tn.ts.net":           {"100.100.100.100"},
		"*":                     {"100.100.100.100"},
		"+.64.100.in-addr.arpa": {"100.100.100.100"},
		"+.lan":                 {"system"},
	}
	return env
}

func TestResolverBinding(t *testing.T) {
	c := ResolverBinding{}
	want(t, c, healthyEnv(), StatusOK, "") // no binding

	env := bindEnv()
	want(t, c, env, StatusWarn, "没带 #接口")

	env = bindEnv()
	env.cfg.HostNames = []string{"server", "server.tn.ts.net", "router.lan"}
	want(t, c, env, StatusOK, "有设备 hosts")

	env = bindEnv()
	for k := range env.cfg.DNS.NameserverPolicy {
		if k != "+.lan" {
			env.cfg.DNS.NameserverPolicy[k] = []string{"100.100.100.100#utun9"}
		}
	}
	want(t, c, env, StatusOK, "#utun9")

	env.routes["100.100.100.100"] = RouteLookup{Interface: "utun11"}
	want(t, c, env, StatusWarn, "utun9 已不是 Tailscale 当前的接口 utun11")

	env = bindEnv()
	env.cfg.TUN.AutoDetectInterface = false
	env.cfg.InterfaceName = "en0"
	want(t, c, env, StatusWarn, "interface-name: en0")

	env = bindEnv()
	env.cfg.DNS.NameserverPolicy = map[string][]string{"+.ts.net": {"system"}}
	want(t, c, env, StatusSkip, "")
}

func TestSplitResolver(t *testing.T) {
	cases := map[string][2]string{
		"100.100.100.100#utun9":                   {"100.100.100.100", "utun9"},
		"udp://100.100.100.100:53#utun9":          {"100.100.100.100", "utun9"},
		"[fd7a:115c:a1e0::53]:53":                 {"fd7a:115c:a1e0::53", ""},
		"https://doh.pub/dns-query#PROXY&h3=true": {"doh.pub/dns-query", "PROXY"},
		"system": {"system", ""},
	}
	for in, w := range cases {
		h, i := splitResolver(in)
		if h != w[0] || i != w[1] {
			t.Errorf("%q → %q %q", in, h, i)
		}
	}
}

func TestFakeIPMode(t *testing.T) {
	c := FakeIPMode{}
	env := healthyEnv()
	want(t, c, env, StatusOK, "没有 '*'")
	env.cfg.DNS.FakeIPFilter = []string{"+.lan", "*"}
	want(t, c, env, StatusOK, "blacklist")
	env.cfg.DNS.FakeIPFilterMode = "whitelist"
	want(t, c, env, StatusWarn, "whitelist")
	env.cfg.DNS.EnhancedMode = "redir-host"
	want(t, c, env, StatusSkip, "")
}

func TestLeakEgress(t *testing.T) {
	c := LeakEgress{}
	r := want(t, c, healthyEnv(), StatusOK, "Google, Meta")
	for _, e := range r.Evidence {
		if strings.Contains(e, "198.51.100.7") || strings.Contains(e, "203.0.113.50") {
			t.Errorf("unmasked IP in %q", e)
		}
	}

	env := healthyEnv()
	env.settings.Egress = false
	if r := want(t, c, env, StatusSkip, "未开启"); r.Fix == nil || r.Fix.AutoFix != FixEgressProbe {
		t.Errorf("egress off: fix %+v, want %s", r.Fix, FixEgressProbe)
	}

	env = healthyEnv()
	env.egressProxied = env.egressDirect
	want(t, c, env, StatusFail, "相同")

	env = healthyEnv()
	env.egressChain = []string{"node-b", "Final"}
	want(t, c, env, StatusSkip, "node-b")

	env = healthyEnv()
	env.egressChain = []string{"DIRECT"}
	want(t, c, env, StatusSkip, "DIRECT")

	// Direct request blocked: tailscaled's public endpoint is the baseline.
	env = healthyEnv()
	env.ts.Self.Addrs = []string{"192.168.1.20:41641", "198.18.0.1:5000", "203.0.113.50:41641"}
	env.egressDirect = netip.Addr{}
	env.egressErr = errors.New("blocked")
	want(t, c, env, StatusWarn, "blocked") // proxied also fails with the shared fake error

	env = healthyEnv()
	env.ts.Self.Addrs = []string{"192.168.1.20:41641", "203.0.113.50:41641"}
	r = want(t, c, env, StatusFail, "相同")
	if !strings.Contains(strings.Join(r.Evidence, "\n"), "STUN") {
		t.Errorf("want STUN evidence: %v", r.Evidence)
	}

	env = healthyEnv()
	env.ts.Self.Addrs = nil
	env.egressErr = errors.New("no network")
	want(t, c, env, StatusSkip, "no network") // network-only: not a warning
}

func TestParseEgressBody(t *testing.T) {
	ip, err := ParseEgressBody("fl=1\nh=1.1.1.1\nip=203.0.113.7\nts=1\n")
	if err != nil || ip != netip.MustParseAddr("203.0.113.7") {
		t.Fatal(ip, err)
	}
	ip, err = ParseEgressBody("2001:db8::1\n")
	if err != nil || ip.String() != "2001:db8::1" {
		t.Fatal(ip, err)
	}
	if _, err := ParseEgressBody("<html>"); err == nil {
		t.Fatal("want error")
	}
	if MaskIP(netip.MustParseAddr("203.0.113.7")) != "203.0.x.x" || MaskIP(netip.MustParseAddr("2001:db8:1::5")) != "2001:db8::x" {
		t.Fatal("mask")
	}
}
