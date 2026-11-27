package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Each path tries the endpoints in order until one answers, and the
// evidence names the one that did. In mainland China the direct path to
// 1.1.1.1 times out; the next endpoint stands in.
func TestLeakEgressEndpointFallback(t *testing.T) {
	env := healthyEnv()
	first, second := DefaultEgressURLs[0], DefaultEgressURLs[1]
	env.egressDown = map[string]error{"direct " + first: errors.New("dial tcp 1.1.1.1:443: i/o timeout")}
	r := want(t, LeakEgress{}, env, StatusOK, "Google, Meta")
	if got := strings.Join(env.egressAsked, " | "); got != "direct "+first+" | direct "+second+" | proxied "+first {
		t.Errorf("asked %s", got)
	}
	ev := strings.Join(r.Evidence, "\n")
	if !strings.Contains(ev, "直连出口：198.51.x.x（www.cloudflare.com 回答）") || !strings.Contains(ev, "1.1.1.1 回答）") {
		t.Errorf("evidence does not say who answered:\n%s", ev)
	}
	if r.Egress == nil || r.Egress.Direct != a("198.51.100.7") {
		t.Errorf("direct address %+v", r.Egress)
	}

	// Configured endpoints replace the built-in list; every failure is
	// listed by host.
	env = healthyEnv()
	env.ts.Self.Addrs = nil
	env.settings.EgressURLs = []string{"https://echo-a.example/ip", "https://echo-b.example/ip"}
	env.egressErr = errors.New("unreachable")
	r = want(t, LeakEgress{}, env, StatusSkip, "echo-a.example: unreachable; echo-b.example: unreachable")
	if len(env.egressAsked) != 2 {
		t.Errorf("asked %v", env.egressAsked)
	}

	// Unsupported ends the tries at once.
	env = healthyEnv()
	env.ts.Self.Addrs = nil
	env.egressErr = fmt.Errorf("bind: %w", ErrNotSupported)
	LeakEgress{}.Run(context.Background(), env)
	if len(env.egressAsked) != 1 {
		t.Errorf("tried on after unsupported: %v", env.egressAsked)
	}
}

func TestCheckEgressURLs(t *testing.T) {
	if err := CheckEgressURLs(DefaultEgressURLs); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"http://1.1.1.1/"}, {"https:///x"}, {"https://u:p@echo.example/"}, {"1.1.1.1"},
		make([]string, MaxEgressURLs+1)} {
		if CheckEgressURLs(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseEgressBody(t *testing.T) {
	for body, want := range map[string]string{
		"Current IP Address: 203.0.113.7\n":                        "203.0.113.7",
		"当前 IP：203.0.113.7  来自于：中国 广东 深圳  联通\n":                    "203.0.113.7",
		"IP\t: 203.0.113.7\n地址\t: 示例\nURL\t: http://x/203.0.113.8": "203.0.113.7",
		"your address is 2001:db8::7 (v6)":                         "2001:db8::7",
	} {
		if ip, err := ParseEgressBody(body); err != nil || ip.String() != want {
			t.Errorf("%q: %v %v", body, ip, err)
		}
	}
	for _, body := range []string{"cafe :: dead", "Address: none", strings.Repeat("x", 5000) + " 203.0.113.7"} {
		if ip, err := ParseEgressBody(body); err == nil {
			t.Errorf("%q: %v", body, ip)
		}
	}
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

// The egress test keeps what it saw, unmasked, on the result for the app,
// but never in the result's JSON (reports, the CLI, MCP).
func TestLeakEgressAddresses(t *testing.T) {
	r := want(t, LeakEgress{}, healthyEnv(), StatusOK, "Google, Meta")
	ips := r.Egress
	if ips == nil || ips.Proxied != a("203.0.113.50") || ips.Direct != a("198.51.100.7") || ips.Node != "node-a" {
		t.Fatalf("egress addresses %+v", ips)
	}
	b, err := json.Marshal(CheckReport{ID: "leak-guard-egress", Result: r})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, "203.0.113.50") || strings.Contains(s, "198.51.100.7") {
		t.Errorf("unmasked address in JSON: %s", s)
	}

	// The proxied request failed: the direct address alone.
	env := healthyEnv()
	env.egressProxied, env.egressChain = netip.Addr{}, nil
	env.egressErr = nil
	r = LeakEgress{}.Run(context.Background(), env)
	if r.Egress == nil || r.Egress.Proxied.IsValid() || r.Egress.Direct != a("198.51.100.7") {
		t.Errorf("direct only: %+v", r.Egress)
	}
}
