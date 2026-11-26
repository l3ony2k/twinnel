package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestTailscaleCoexist(t *testing.T) {
	got, err := TailscaleCoexist(TailnetInfo{
		MagicDNSSuffix: "Example-Tailnet.ts.net.",
		PeerShortNames: []string{"nas", "Laptop", "nas", " phone "},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `# Managed by duet: Tailscale coexistence. Regenerated when the tailnet changes; do not edit.
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
  nameserver-policy:
    <+.ts.net>:
      - system
    <+.beta.tailscale.net>:
      - system
'+rules':
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
		"'+.corp.example.com'", "<+.corp.example.com>:\n      - 100.100.100.100",
		"PROCESS-NAME,tailscaled.exe,DIRECT", "DOMAIN-SUFFIX,corp.example.com,DIRECT",
	} {
		if !strings.Contains(string(got), s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
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
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("got %+v, want %+v", info, want)
	}
	if _, err := TailnetInfoFromStatus([]byte("{")); err == nil {
		t.Error("bad json: no error")
	}
}
