package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseRule(t *testing.T) {
	cases := []struct{ in, canon, target string }{
		{"DOMAIN-SUFFIX,Example.COM,DIRECT", "DOMAIN-SUFFIX,example.com,DIRECT", "DIRECT"},
		{" IP-CIDR6 , fd7a:115c:a1e0::1/48 , DIRECT , no-resolve", "IP-CIDR,fd7a:115c:a1e0::/48,DIRECT,no-resolve", "DIRECT"},
		{"MATCH,PROXY", "MATCH,PROXY", "PROXY"},
		{"AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT", "AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT", "REJECT"},
		{"GEOIP,CN,DIRECT,no-resolve", "GEOIP,CN,DIRECT,no-resolve", "DIRECT"},
		{"DIRECT", "DIRECT,,", ""}, // malformed: kept, no panic
		{"", ",,", ""},
	}
	for _, c := range cases {
		r := ParseRule(c.in)
		if r.Canonical() != c.canon || r.Target != c.target {
			t.Errorf("%q: got %q target %q", c.in, r.Canonical(), r.Target)
		}
	}
}

func TestCovers(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"DOMAIN-SUFFIX,tailscale.com,DIRECT", "DOMAIN,derp1.tailscale.com,DIRECT", true},
		{"DOMAIN-SUFFIX,tailscale.com,DIRECT", "DOMAIN,tailscale.com,PROXY", true},
		{"DOMAIN-SUFFIX,tailscale.com,DIRECT", "DOMAIN,xtailscale.com,DIRECT", false},
		{"DOMAIN-SUFFIX,a.com,DIRECT", "DOMAIN-SUFFIX,b.a.com,DIRECT", true},
		{"DOMAIN-SUFFIX,b.a.com,DIRECT", "DOMAIN-SUFFIX,a.com,DIRECT", false},
		{"DOMAIN-KEYWORD,goog,PROXY", "DOMAIN-SUFFIX,google.com,DIRECT", true},
		{"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve", "IP-CIDR,100.100.100.100/32,DIRECT,no-resolve", true},
		{"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve", "IP-CIDR,100.100.100.100/32,DIRECT", false},
		{"IP-CIDR,100.64.0.0/10,DIRECT", "IP-CIDR,100.100.100.100/32,DIRECT,no-resolve", true},
		{"IP-CIDR,0.0.0.0/0,DIRECT", "IP-CIDR6,::/0,DIRECT", false},
		{"MATCH,DIRECT", "GEOIP,CN,DIRECT", true},
		{"GEOSITE,google,PROXY", "DOMAIN,google.com,PROXY", false},
		{"SRC-IP-CIDR,10.0.0.0/8,DIRECT", "SRC-IP-CIDR,10.1.0.0/16,DIRECT", false},
	}
	for _, c := range cases {
		if got := Covers(ParseRule(c.a), ParseRule(c.b)); got != c.want {
			t.Errorf("Covers(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestDiffRulesEquivalentReorder(t *testing.T) {
	tail := []string{"GEOSITE,google,PROXY", "GEOIP,CN,DIRECT", "MATCH,PROXY"}
	a := append([]string{
		"DOMAIN-SUFFIX,microsoft.com,DIRECT",
		"PROCESS-NAME,tailscaled,DIRECT",
		"DOMAIN-SUFFIX,tailscale.com,DIRECT",
		"DOMAIN,controlplane.tailscale.com,DIRECT", // shadowed
		"DOMAIN,laptop,DIRECT",
		"DOMAIN,macbook,DIRECT",
		"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
		"IP-CIDR,100.100.100.100/32,DIRECT,no-resolve", // shadowed
	}, tail...)
	a = append(a, "GEOIP,CN,DIRECT") // duplicate (and after MATCH)
	b := append([]string{
		"PROCESS-NAME,tailscaled,DIRECT",
		"DOMAIN-SUFFIX,tailscale.com,DIRECT",
		"DOMAIN,macbook,DIRECT",
		"DOMAIN,laptop,DIRECT",
		"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
		"DOMAIN-SUFFIX,microsoft.com,DIRECT",
	}, tail...)
	d := DiffRules(a, b)
	if !d.Equivalent || !d.Reordered || d.Identical {
		t.Fatalf("want equivalent+reordered: %+v", d)
	}
	if d.DupA != 1 || len(d.DeadA) != 2 {
		t.Fatalf("dup/dead A: %d %v", d.DupA, d.DeadA)
	}
}

func TestDiffRulesTargetMatters(t *testing.T) {
	// Swapping rules with different targets is not provably equivalent.
	a := []string{"DOMAIN,a.com,DIRECT", "DOMAIN-SUFFIX,com,PROXY", "MATCH,DIRECT"}
	b := []string{"DOMAIN-SUFFIX,com,PROXY", "DOMAIN,a.com,DIRECT", "MATCH,DIRECT"}
	d := DiffRules(a, b)
	if d.Equivalent {
		t.Fatal("reordering across targets must not be equivalent")
	}
	if len(d.DeadB) != 1 || !strings.Contains(d.DeadB[0].Rule, "a.com") {
		t.Fatalf("a.com is dead in B: %+v", d.DeadB)
	}
	if len(d.Edits) == 0 {
		t.Fatal("expected edits")
	}
}

func TestDiffRulesRedundantInsideBlock(t *testing.T) {
	// A later same-target rule covering an earlier one makes it redundant.
	a := []string{"DOMAIN,x.ts.net,DIRECT", "DOMAIN-SUFFIX,ts.net,DIRECT", "MATCH,PROXY"}
	b := []string{"DOMAIN-SUFFIX,ts.net,DIRECT", "MATCH,PROXY"}
	if d := DiffRules(a, b); !d.Equivalent {
		t.Fatalf("want equivalent: %+v", d)
	}
}

const semA = `
mixed-port: 7890
mode: Rule
secret: hunter2
dns:
  enable: true
  nameserver: [https://a.example/dns-query, https://b.example/dns-query]
  fake-ip-filter: ['+.lan', '+.local']
proxies:
  - {name: n1, type: ss, server: 1.2.3.4, port: 1, password: pw1, cipher: aes-128-gcm}
  - {name: n2, type: ss, server: 1.2.3.5, port: 1, password: pw2, cipher: aes-128-gcm}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, n2, DIRECT]}
proxy-providers:
  p: {type: http, url: "https://sub.example/token123", interval: 3600}
rules:
  - DOMAIN-SUFFIX,lan,DIRECT
  - MATCH,PROXY
`

const semB = `
mixed-port: 7899
mode: rule
secret: s3cret
dns:
  enable: true
  nameserver: [https://b.example/dns-query, https://a.example/dns-query]
  fake-ip-filter: ['+.local', '+.lan', '*']
proxies:
  - {name: n1, type: ss, server: 1.2.3.4, port: 1, password: pwX, cipher: aes-128-gcm}
  - {name: n3, type: ss, server: 1.2.3.6, port: 1, password: pw3, cipher: aes-128-gcm}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, n3]}
proxy-providers:
  p: {type: http, url: "https://sub.example/token456", interval: 3600}
rules:
  - DOMAIN-SUFFIX,lan,DIRECT
  - MATCH,PROXY
`

func TestSemanticDiffNoSecrets(t *testing.T) {
	d, err := SemanticDiff([]byte(semA), []byte(semB))
	if err != nil {
		t.Fatal(err)
	}
	if d.Equivalent() {
		t.Fatal("not equivalent")
	}
	txt := d.Text(0)
	for _, s := range []string{"hunter2", "s3cret", "pw1", "pwX", "pw3", "token123", "token456", "1.2.3.4"} {
		if strings.Contains(txt, s) {
			t.Errorf("text leaks %q:\n%s", s, txt)
		}
	}
	want := []string{
		"mixed-port: 7890 → 7899",
		"secret: <redacted> → <redacted>",
		"dns.nameserver: same entries, different order",
		"dns.fake-ip-filter: added *",
		`changed: "n1"`, `only in A: "n2"`, `only in B: "n3"`,
		`changed: "p"`,
		"PROXY.proxies:",
		"rules: A=2 B=2, identical",
	}
	for _, w := range want {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q in:\n%s", w, txt)
		}
	}
	if strings.Contains(txt, "mode:") {
		t.Errorf("mode Rule/rule must compare equal:\n%s", txt)
	}
}

func TestSemanticDiffEquivalent(t *testing.T) {
	b := strings.Replace(semA, "fake-ip-filter: ['+.lan', '+.local']", "fake-ip-filter: ['+.local', '+.lan', '+.lan']", 1)
	d, err := SemanticDiff([]byte(semA), []byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Equivalent() {
		t.Fatalf("want equivalent:\n%s", d.Text(0))
	}
}

func TestRedactURL(t *testing.T) {
	if got := RedactURL("https://u:p@sub.example/clash/tok?x=1", true); got != "https://<redacted>@sub.example/<redacted>" {
		t.Error(got)
	}
	if got := RedactText("see https://doh.pub/dns-query and https://x.example/a?token=1"); got != "see https://doh.pub/dns-query and https://x.example/a?<redacted>" {
		t.Error(got)
	}
}

// TestRealSemDiff compares two real configs (read-only). It prints only the
// redacted text.
//
//	DUET_SEMDIFF_A=live.yaml DUET_SEMDIFF_B=composed.yaml go test -run RealSemDiff -v ./core/config/
func TestRealSemDiff(t *testing.T) {
	pa, pb := os.Getenv("DUET_SEMDIFF_A"), os.Getenv("DUET_SEMDIFF_B")
	if pa == "" || pb == "" {
		t.Skip("set DUET_SEMDIFF_A and DUET_SEMDIFF_B")
	}
	a, err := os.ReadFile(pa)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(pb)
	if err != nil {
		t.Fatal(err)
	}
	d, err := SemanticDiff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + d.Text(60))
}
