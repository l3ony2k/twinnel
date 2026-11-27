package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTailscaleCoexist(t *testing.T) {
	got, err := TailscaleCoexist(TailnetInfo{
		MagicDNSSuffix: "Example-Tailnet.ts.net.",
		PeerShortNames: []string{"nas", "Laptop", "nas", " phone "},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `# Managed by twinnel: Tailscale coexistence. Regenerated when the tailnet changes; do not edit.
tun:
  route-exclude-address+:
    - 100.64.0.0/10
    - fd7a:115c:a1e0::/48
dns:
  fake-ip-filter+:
    - '+.tailscale.com'
    - '+.tailscale.io'
    - '+.ts.net'
    - '+.beta.tailscale.net'
    - '*'
  nameserver-policy:
    <+.example-tailnet.ts.net>: [100.100.100.100]
    '*': [100.100.100.100]
` + revPolicy("100.100.100.100") + `'+rules':
  - PROCESS-NAME,tailscaled,DIRECT
  - DOMAIN-SUFFIX,tailscale.com,DIRECT
  - DOMAIN-SUFFIX,tailscale.io,DIRECT
  - DOMAIN-SUFFIX,ts.net,DIRECT
  - DOMAIN-SUFFIX,beta.tailscale.net,DIRECT
  - DOMAIN,laptop,DIRECT
  - DOMAIN,nas,DIRECT
  - DOMAIN,phone,DIRECT
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTailscaleCoexistOptions(t *testing.T) {
	got, err := TailscaleCoexist(TailnetInfo{
		IPv4CIDR:       "100.100.0.0/16",
		IPv6CIDR:       "fd7a:115c:a1e0:ab12::/64",
		MagicDNSSuffix: "corp.example.com", // custom suffix, not under ts.net
		ProcessNames:   []string{"tailscaled", "tailscaled.exe"},
		Resolver:       "100.100.100.100",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"- 100.100.0.0/16", "- fd7a:115c:a1e0:ab12::/64",
		"'+.corp.example.com'", "<+.corp.example.com>: [100.100.100.100]",
		"<+.100.100.in-addr.arpa>: [100.100.100.100]",
		"<+.2.1.b.a.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa>: [100.100.100.100]",
		"PROCESS-NAME,tailscaled.exe,DIRECT", "DOMAIN-SUFFIX,corp.example.com,DIRECT",
	} {
		if !strings.Contains(string(got), s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
	if strings.Contains(string(got), "in-addr.arpa>") && strings.Count(string(got), "in-addr.arpa>") != 1 {
		t.Errorf("want exactly one v4 reverse zone for a /16:\n%s", got)
	}
	if strings.Contains(string(got), "hosts") || strings.Contains(string(got), "<+.ts.net>") {
		t.Errorf("no addresses / custom suffix, but hosts or +.ts.net policy present:\n%s", got)
	}
	if strings.Contains(string(got), "DOMAIN,") {
		t.Errorf("no peers given, but DOMAIN rules present:\n%s", got)
	}
}

func TestTailscaleCoexistErrors(t *testing.T) {
	for _, info := range []TailnetInfo{
		{IPv4CIDR: "100.64.0.0"},
		{IPv6CIDR: "nope"},
		{PeerShortNames: []string{"bad name"}},
		{PeerShortNames: []string{"-x"}},
		{PeerAddrs: map[string][]string{"nas": {"not-an-ip"}}},
		{PeerAddrs: map[string][]string{"bad name": {"100.64.0.1"}}},
		{IPv4CIDR: "0.0.0.0/0"},
		{IPv6CIDR: "::/0"},
	} {
		if _, err := TailscaleCoexist(info); err == nil {
			t.Errorf("%+v: no error", info)
		}
	}
}

// The generated layer must merge onto a config that already has the same
// entries without duplicating them, and keep "+.ts.net" a literal key.
func TestTailscaleCoexistMerge(t *testing.T) {
	l, err := TailscaleCoexistLayer(TailnetInfo{PeerShortNames: []string{"nas"}})
	if err != nil {
		t.Fatal(err)
	}
	sub := "dns:\n  fake-ip-filter: ['+.ts.net']\n  nameserver-policy:\n    '+.ts.net': [system]\nrules: ['MATCH,PROXY']\n"
	got, err := Compose([]byte(sub), []Layer{l})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), "'+.ts.net'"); n != 2 { // one filter entry, one policy key
		t.Errorf("'+.ts.net' appears %d times:\n%s", n, got)
	}
	root, _ := Parse(got)
	rules := lookup(root, "rules").Content
	if rules[len(rules)-1].Value != "MATCH,PROXY" || rules[0].Value != "PROCESS-NAME,tailscaled,DIRECT" {
		t.Errorf("subscription rules not last:\n%s", got)
	}
}

func TestTailnetInfoFromStatus(t *testing.T) {
	info, err := TailnetInfoFromStatus(readTestdata(t, "tailscale-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := TailnetInfo{
		MagicDNSSuffix: "example-tailnet.ts.net",
		PeerShortNames: []string{"laptop", "nas", "phone"},
		PeerAddrs:      map[string][]string{"laptop": {"100.64.0.1"}},
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("got %+v, want %+v", info, want)
	}
	if _, err := TailnetInfoFromStatus([]byte("{")); err == nil {
		t.Error("bad json: no error")
	}
}

// revPolicy is the expected nameserver-policy block for the default ranges.
func revPolicy(ns string) string {
	var b strings.Builder
	for i := 64; i <= 127; i++ {
		fmt.Fprintf(&b, "    <+.%d.100.in-addr.arpa>: [%s]\n", i, ns)
	}
	fmt.Fprintf(&b, "    <+.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa>: [%s]\n", ns)
	return b.String()
}

func TestTailscaleCoexistHostsAndInterface(t *testing.T) {
	got, err := TailscaleCoexist(TailnetInfo{
		MagicDNSSuffix: "example-tailnet.ts.net",
		PeerShortNames: []string{"nas"},
		PeerAddrs: map[string][]string{
			"NAS":    {"fd7a:115c:a1e0::1", "100.64.0.2", "100.64.0.2"},
			"laptop": {"100.64.0.1"},
		},
		Interface: "utun9",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"hosts:\n  laptop:\n    - 100.64.0.1\n  laptop.example-tailnet.ts.net:\n    - 100.64.0.1\n" +
			"  nas:\n    - 100.64.0.2\n    - fd7a:115c:a1e0::1\n  nas.example-tailnet.ts.net:\n",
		"dns:\n  use-hosts: true\n",
		"<+.example-tailnet.ts.net>: [100.100.100.100#utun9]",
		"'*': [100.100.100.100#utun9]",
	} {
		if !strings.Contains(string(got), s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
	// A custom resolver is used as is.
	got, _ = TailscaleCoexist(TailnetInfo{Resolver: "10.0.0.53", Interface: "utun9"})
	if strings.Contains(string(got), "#utun9") || !strings.Contains(string(got), "'*': [10.0.0.53]") {
		t.Errorf("custom resolver changed:\n%s", got)
	}
}

func TestReverseZones(t *testing.T) {
	for _, c := range []struct {
		v4, v6 string
		want   []string
	}{
		{"10.0.0.0/8", "fd00::/8", []string{"10.in-addr.arpa", "d.f.ip6.arpa"}},
		{"192.168.4.0/23", "fd7a:115c:a1e0:10::/62", []string{
			"4.168.192.in-addr.arpa", "5.168.192.in-addr.arpa",
			"0.1.0.0.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa", "1.1.0.0.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa",
			"2.1.0.0.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa", "3.1.0.0.0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa",
		}},
		{"10.255.0.0/15", "fd00::/7", []string{
			"254.10.in-addr.arpa", "255.10.in-addr.arpa", // masked to 10.254/15
			"c.f.ip6.arpa", "d.f.ip6.arpa",
		}},
	} {
		got, err := reverseZones(c.v4, c.v6)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s: got %v, want %v", c.v4, c.v6, got, c.want)
		}
	}
}

func TestTailscaleCoexistFakeIPFilterMode(t *testing.T) {
	l, err := TailscaleCoexistLayer(TailnetInfo{MagicDNSSuffix: "tn-1.ts.net"})
	if err != nil {
		t.Fatal(err)
	}
	compose := func(sub string) ([]string, []string) {
		t.Helper()
		r, err := ComposeReport([]byte(sub), []Layer{l})
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			DNS struct {
				Filter []string `yaml:"fake-ip-filter"`
			} `yaml:"dns"`
		}
		if err := yaml.Unmarshal(r.Config, &c); err != nil {
			t.Fatal(err)
		}
		return c.DNS.Filter, r.Warnings
	}

	// Default (blacklist): plain entries.
	f, w := compose("dns:\n  enhanced-mode: fake-ip\n  fake-ip-filter: ['+.lan']\n")
	if strings.Join(f, " ") != "+.lan +.tailscale.com +.tailscale.io +.ts.net +.beta.tailscale.net *" || len(w) != 0 {
		t.Fatalf("blacklist: %q %q", f, w)
	}

	// Rule mode: every entry must be a rule (a bare "+.ts.net" makes mihomo
	// refuse the config), and the real-ip rules must come before the user's
	// catch-all.
	f, w = compose("dns:\n  enhanced-mode: fake-ip\n  fake-ip-filter-mode: rule\n  fake-ip-filter:\n    - DOMAIN-SUFFIX,lan,real-ip\n    - MATCH,fake-ip\n")
	want := []string{
		"DOMAIN-SUFFIX,tailscale.com,real-ip", "DOMAIN-SUFFIX,tailscale.io,real-ip", "DOMAIN-SUFFIX,ts.net,real-ip",
		"DOMAIN-SUFFIX,beta.tailscale.net,real-ip", "DOMAIN-REGEX,^[^.]+$,real-ip",
		"DOMAIN-SUFFIX,lan,real-ip", "MATCH,fake-ip",
	}
	if !reflect.DeepEqual(f, want) || len(w) != 0 {
		t.Fatalf("rule:\n got %q\nwant %q\n%q", f, want, w)
	}
	for _, e := range f {
		if r := ParseRule(e); r.Target != "real-ip" && r.Target != "fake-ip" {
			t.Errorf("rule-mode entry %q has no fake-ip/real-ip action", e)
		}
	}

	// Whitelist mode: listed names get fake-ips; the entries are dropped.
	f, w = compose("dns:\n  enhanced-mode: fake-ip\n  fake-ip-filter-mode: Whitelist\n  fake-ip-filter: ['+.google.com']\n")
	if strings.Join(f, " ") != "+.google.com" || len(w) != 1 {
		t.Fatalf("whitelist: %q %q", f, w)
	}
}

func TestTailnetInfoFromStatusOddNames(t *testing.T) {
	st := `{"MagicDNSSuffix":"tn.ts.net","Self":{"DNSName":"me.tn.ts.net.","TailscaleIPs":["100.64.0.1"]},
"Peer":{"a":{"DNSName":"localhost.tn.ts.net.","TailscaleIPs":["100.64.0.2"]},
"b":{"DNSName":"bad_name.tn.ts.net.","TailscaleIPs":["100.64.0.3"]},
"c":{"DNSName":"xn--caf-dma.tn.ts.net.","TailscaleIPs":["100.64.0.4"]}}}`
	info, err := TailnetInfoFromStatus([]byte(st))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.PeerShortNames, ",") != "me,xn--caf-dma" {
		t.Fatalf("peers %v", info.PeerShortNames)
	}
	if _, err := TailscaleCoexist(info); err != nil {
		t.Fatal(err)
	}
}

func TestTailnetInfoFromStatusMagicDNSOff(t *testing.T) {
	cases := []struct {
		name, st string
		off      bool
	}{
		{"enabled", `{"MagicDNSSuffix":"tn.ts.net","CurrentTailnet":{"MagicDNSSuffix":"tn.ts.net","MagicDNSEnabled":true}}`, false},
		{"disabled", `{"MagicDNSSuffix":"tn.ts.net","CurrentTailnet":{"MagicDNSSuffix":"tn.ts.net","MagicDNSEnabled":false}}`, true},
		{"no suffix", `{"CurrentTailnet":{"MagicDNSEnabled":true}}`, true},
		{"old tailscaled, suffix", `{"MagicDNSSuffix":"tn.ts.net"}`, false},
		{"old tailscaled, no suffix", `{}`, true},
	}
	for _, c := range cases {
		info, err := TailnetInfoFromStatus([]byte(c.st))
		if err != nil {
			t.Fatal(err)
		}
		if info.MagicDNSOff != c.off {
			t.Errorf("%s: MagicDNSOff = %v, want %v", c.name, info.MagicDNSOff, c.off)
		}
	}
}

// With MagicDNS off quad-100 forwards names it does not own to the OS
// resolver (mihomo), so nothing may be routed to it; hosts, CIDR rules and
// route-exclude stay so peer names and tailnet IPs keep working.
func TestTailscaleCoexistMagicDNSOff(t *testing.T) {
	for _, suffix := range []string{"tn.ts.net", ""} {
		got, err := TailscaleCoexist(TailnetInfo{
			MagicDNSSuffix: suffix, MagicDNSOff: true,
			PeerShortNames: []string{"nas"},
			PeerAddrs:      map[string][]string{"nas": {"100.64.0.2"}},
			Interface:      "utun9",
		})
		if err != nil {
			t.Fatal(err)
		}
		s := string(got)
		for _, bad := range []string{"nameserver-policy", "100.100.100.100", "in-addr.arpa", "ip6.arpa"} {
			if strings.Contains(s, bad) {
				t.Errorf("suffix %q: output contains %q:\n%s", suffix, bad, s)
			}
		}
		want := []string{
			"route-exclude-address+:\n    - 100.64.0.0/10\n    - fd7a:115c:a1e0::/48",
			"  nas:\n    - 100.64.0.2",
			"use-hosts: true",
			"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
			"IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve",
			"DOMAIN,nas,DIRECT",
			"- '*'",
		}
		if suffix != "" {
			want = append(want, "  nas.tn.ts.net:\n    - 100.64.0.2")
		}
		for _, w := range want {
			if !strings.Contains(s, w) {
				t.Errorf("suffix %q: missing %q:\n%s", suffix, w, s)
			}
		}
		// The composed result is a valid config.
		l, err := TailscaleCoexistLayer(TailnetInfo{MagicDNSSuffix: suffix, MagicDNSOff: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compose([]byte("dns:\n  enable: true\nrules:\n  - MATCH,DIRECT\n"), []Layer{l}); err != nil {
			t.Fatal(err)
		}
	}
}
