package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLeakGuard(t *testing.T) {
	sub := `proxies:
  - {name: a, type: ss}
  - {name: home, type: direct}
proxy-groups:
  - {name: PROXY, type: select, proxies: [a, DIRECT]}
  - {name: Google, type: select, proxies: [DIRECT, a, home, b]}
  - {name: OnlyDirect, type: select, proxies: [DIRECT]}
  - {name: Provider, type: select, proxies: [DIRECT], use: [pv]}
  - {name: All, type: select, proxies: [DIRECT], include-all: true, exclude-type: Http}
  - {name: Nested, type: select, proxies: [Mid, b]}
  - {name: Mid, type: select, proxies: [PROXY]}
  - {name: Cycle1, type: select, proxies: [Cycle2, DIRECT], empty-fallback: COMPATIBLE}
  - {name: Cycle2, type: select, proxies: [Cycle1, c, home]}
  - {name: Untouched, type: select, proxies: [DIRECT]}
`
	groups := []string{"Google", "OnlyDirect", "Provider", "All", "Nested", "Cycle1", "Nope"}
	r, err := ComposeReport([]byte(sub), []Layer{LeakGuardLayer(groups)})
	if err != nil {
		t.Fatal(err)
	}
	want := `proxies:
  - {name: a, type: ss}
  - {name: home, type: direct}
proxy-groups:
  - {name: PROXY, type: select, proxies: [a, DIRECT]}
  - {name: Google, type: select, proxies: [a, b], empty-fallback: REJECT}
  - {name: OnlyDirect, type: select, proxies: [REJECT]}
  - {name: Provider, type: select, proxies: [], use: [pv], empty-fallback: REJECT, exclude-type: Direct}
  - {name: All, type: select, proxies: [], include-all: true, exclude-type: Http|Direct, empty-fallback: REJECT}
  - {name: Nested, type: select, proxies: [Mid, b], empty-fallback: REJECT}
  - {name: Mid, type: select, proxies: [PROXY]}
  - {name: Cycle1, type: select, proxies: [Cycle2], empty-fallback: REJECT}
  - {name: Cycle2, type: select, proxies: [Cycle1, c, home]}
  - {name: Untouched, type: select, proxies: [DIRECT]}
`
	if g, w := canon(t, string(r.Config)), canon(t, want); g != w {
		t.Errorf("got:\n%s\nwant:\n%s", g, w)
	}
	wantWarn := []string{
		`leak-guard: proxy group "OnlyDirect" has no members left; set to REJECT`,
		`leak-guard: proxy group "Nested" can still reach DIRECT through [Mid] (nested groups are not changed: they may carry other traffic)`,
		`leak-guard: proxy group "Cycle1" can still reach DIRECT through [Cycle2] (nested groups are not changed: they may carry other traffic)`,
		`leak-guard: proxy group "Nope" not found`,
	}
	if !reflect.DeepEqual(r.Warnings, wantWarn) {
		t.Errorf("warnings:\n%q\nwant:\n%q", r.Warnings, wantWarn)
	}
}

func TestLeakGuardNoGroups(t *testing.T) {
	r, err := ComposeReport([]byte("mode: rule\n"), []Layer{LeakGuardLayer([]string{"G"})})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 {
		t.Errorf("warnings = %q", r.Warnings)
	}
	if _, err := Compose([]byte("proxy-groups: x\n"), []Layer{LeakGuardLayer([]string{"G"})}); err == nil {
		t.Error("proxy-groups scalar: no error")
	}
}

const nestedSub = `proxies:
  - {name: a, type: ss}
proxy-providers:
  pv: {type: file, path: ./pv.yaml}
dns:
  nameserver: ['https://dns.example/dns-query#DnsVia']
proxy-groups:
  - {name: G1, type: select, proxies: [Only, Shared, Chain1, a]}
  - {name: G2, type: select, proxies: [Only, Deep, DnsVia, Dialer]}
  - {name: Only, type: fallback, use: [pv], url: 'http://x', interval: 300}
  - {name: Deep, type: select, proxies: [Leaf]}
  - {name: Leaf, type: url-test, include-all: true, url: 'http://x', interval: 300}
  - {name: Shared, type: url-test, use: [pv], url: 'http://x', interval: 300}
  - {name: Chain1, type: select, proxies: [Chain2], use: [pv]}
  - {name: Chain2, type: select, proxies: [Chain1], use: [pv], empty-fallback: COMPATIBLE}
  - {name: DnsVia, type: select, use: [pv]}
  - {name: Dialer, type: select, use: [pv]}
  - {name: Other, type: select, proxies: [Shared, DIRECT]}
rules:
  - DOMAIN-SUFFIX,a.com,G1
  - MATCH,Other
sub-rules:
  s: [MATCH,G2]
`

func TestLeakGuardNested(t *testing.T) {
	sub := strings.Replace(nestedSub, "  - {name: a, type: ss}\n", "  - {name: a, type: ss}\n  - {name: x, type: ss, dialer-proxy: Dialer}\n", 1)
	groups := []string{"G1", "G2"}
	fallback := func(cfg []byte) map[string]string {
		var o struct {
			Groups []struct {
				Name string `yaml:"name"`
				EF   string `yaml:"empty-fallback"`
			} `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(cfg, &o); err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, g := range o.Groups {
			m[g.Name] = g.EF
		}
		return m
	}

	// Off: nested groups untouched, dynamic ones warned about.
	r, err := ComposeReport([]byte(sub), []Layer{LeakGuardLayer(groups)})
	if err != nil {
		t.Fatal(err)
	}
	off := fallback(r.Config)
	for _, n := range []string{"Only", "Deep", "Leaf", "Shared", "Chain1", "DnsVia", "Dialer"} {
		if off[n] != "" {
			t.Errorf("off: %s got empty-fallback %q", n, off[n])
		}
	}
	if off["Chain2"] != "COMPATIBLE" || off["G1"] != "REJECT" || off["G2"] != "REJECT" {
		t.Errorf("off: %v", off)
	}
	warned := strings.Join(r.Warnings, "\n")
	for _, n := range []string{"Only", "Leaf", "Shared", "Chain1", "Chain2", "DnsVia", "Dialer"} {
		if !strings.Contains(warned, fmt.Sprintf("nested group %q", n)) {
			t.Errorf("off: no warning for %s:\n%s", n, warned)
		}
	}
	if strings.Contains(warned, `nested group "Deep"`) {
		t.Errorf("off: static nested group Deep warned:\n%s", warned)
	}

	// On: only groups used by guarded groups alone get REJECT.
	r, err = ComposeReport([]byte(sub), []Layer{LeakGuardLayerWith(groups, LeakGuardOptions{Nested: true})})
	if err != nil {
		t.Fatal(err)
	}
	on := fallback(r.Config)
	want := map[string]string{
		"G1": "REJECT", "G2": "REJECT",
		"Only": "REJECT", "Deep": "REJECT", "Leaf": "REJECT", // guarded only, also transitively
		"Chain1": "REJECT", "Chain2": "REJECT", // a cycle reached only from G1
		"Shared": "", // also in Other
		"DnsVia": "", // named by a DNS server ("#DnsVia")
		"Dialer": "", // a proxy's dialer-proxy
		"Other":  "", // not nested
	}
	for n, w := range want {
		if on[n] != w {
			t.Errorf("on: %s empty-fallback %q, want %q", n, on[n], w)
		}
	}
	warned = strings.Join(r.Warnings, "\n")
	for _, n := range []string{"Shared", "DnsVia", "Dialer"} {
		if !strings.Contains(warned, fmt.Sprintf("nested group %q of a guarded group goes DIRECT when it has no members at run time (it is used by other traffic too", n)) {
			t.Errorf("on: no shared warning for %s:\n%s", n, warned)
		}
	}
	for _, n := range []string{"Only", "Leaf", "Chain1", "Chain2"} {
		if strings.Contains(warned, fmt.Sprintf("nested group %q", n)) {
			t.Errorf("on: %s still warned:\n%s", n, warned)
		}
	}
	// A rule naming a nested group makes it shared.
	sub2 := strings.Replace(sub, "  - MATCH,Other\n", "  - DOMAIN,b.com,Only\n  - MATCH,Other\n", 1)
	r, err = ComposeReport([]byte(sub2), []Layer{LeakGuardLayerWith(groups, LeakGuardOptions{Nested: true})})
	if err != nil {
		t.Fatal(err)
	}
	if ef := fallback(r.Config)["Only"]; ef != "" {
		t.Errorf("rule target Only got empty-fallback %q", ef)
	}
	// An unguarded parent of a guard-only group's parent makes both shared.
	sub3 := strings.Replace(sub, "proxies: [Shared, DIRECT]}", "proxies: [Shared, Deep, DIRECT]}", 1)
	r, err = ComposeReport([]byte(sub3), []Layer{LeakGuardLayerWith(groups, LeakGuardOptions{Nested: true})})
	if err != nil {
		t.Fatal(err)
	}
	if got := fallback(r.Config); got["Deep"] != "" || got["Leaf"] != "" {
		t.Errorf("Deep/Leaf shared via Other: %v", got)
	}
}
