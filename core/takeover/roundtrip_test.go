package takeover

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/config"
)

// randomBase builds a synthetic subscription.
func randomBase(r *rand.Rand) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mixed-port: %d\nmode: rule\nlog-level: info\n", 7890+r.Intn(3))
	b.WriteString("dns:\n  enable: true\n  enhanced-mode: fake-ip\n  nameserver: [223.5.5.5]\n")
	switch r.Intn(3) {
	case 0:
		b.WriteString("  fake-ip-filter: ['+.lan', '+.local']\n")
	case 1:
		b.WriteString("  fake-ip-filter-mode: rule\n  fake-ip-filter: ['DOMAIN-SUFFIX,lan,real-ip', 'MATCH,fake-ip']\n")
	}
	b.WriteString("proxies:\n")
	np := 1 + r.Intn(4)
	for i := 0; i < np; i++ {
		fmt.Fprintf(&b, "  - {name: p%d, type: ss, server: 192.0.2.%d, port: 443, cipher: aes-128-gcm, password: x}\n", i, i+1)
	}
	b.WriteString("proxy-groups:\n  - name: PROXY\n    type: select\n    proxies: [")
	for i := 0; i < np; i++ {
		fmt.Fprintf(&b, "p%d, ", i)
	}
	b.WriteString("DIRECT]\n")
	if r.Intn(2) == 0 {
		b.WriteString("rule-providers:\n  ads: {type: http, behavior: domain, url: 'https://example.invalid/ads.yaml', interval: 86400}\n")
	}
	b.WriteString("rules:\n")
	for i, n := 0, r.Intn(6); i < n; i++ {
		b.WriteString("  - " + randomRule(r) + "\n")
	}
	b.WriteString("  - MATCH,PROXY\n")
	return b.String()
}

func randomRule(r *rand.Rand) string {
	targets := []string{"DIRECT", "PROXY", "REJECT"}
	t := targets[r.Intn(len(targets))]
	switch r.Intn(8) {
	case 0:
		return fmt.Sprintf("DOMAIN-SUFFIX,s%d.example,%s", r.Intn(5), t)
	case 1:
		return fmt.Sprintf("DOMAIN,h%d.s%d.example,%s", r.Intn(3), r.Intn(5), t)
	case 2:
		return fmt.Sprintf("DOMAIN-KEYWORD,k%d,%s", r.Intn(4), t)
	case 3:
		return fmt.Sprintf("IP-CIDR,10.%d.0.0/16,%s,no-resolve", r.Intn(4), t)
	case 4:
		return "GEOIP,CN," + t
	case 5:
		// Tailscale-ish rules users often hand-write.
		return []string{"DOMAIN-SUFFIX,tailscale.com,DIRECT", "IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
			"DOMAIN,nas,DIRECT", "DOMAIN,router,DIRECT", "DOMAIN-SUFFIX,ts.net,PROXY"}[r.Intn(5)]
	case 6:
		return fmt.Sprintf("AND,((NETWORK,udp),(DST-PORT,%d)),%s", 400+r.Intn(3), t)
	}
	return fmt.Sprintf("DOMAIN-REGEX,^a{1,%d}\\.example$,%s", 1+r.Intn(3), t)
}

// randomUserLayer is a hand edit on top of the subscription.
func randomUserLayer(r *rand.Rand) string {
	var b strings.Builder
	if r.Intn(2) == 0 {
		fmt.Fprintf(&b, "mixed-port: %d\n", 7895+r.Intn(3))
	}
	if r.Intn(2) == 0 {
		b.WriteString("dns:\n  nameserver-policy:\n    '<+.corp.example>': [10.0.0.53]\n")
	}
	if r.Intn(2) == 0 {
		b.WriteString("tun:\n  enable: true\n  auto-detect-interface: true\n")
	}
	if n := r.Intn(6); n > 0 {
		b.WriteString("'+rules':\n")
		for i := 0; i < n; i++ {
			b.WriteString("  - '" + randomRule(r) + "'\n")
		}
	}
	return b.String()
}

// partyCompose builds a live config the way Clash Party does: overrides
// merged in order, no rule dedupe or key reordering (layer transforms, i.e.
// the coexist layer's fake-ip-filter-mode fix, still run).
func partyCompose(base string, layers []config.Layer) ([]byte, error) {
	root, err := config.Parse([]byte(base))
	if err != nil {
		return nil, err
	}
	for _, l := range layers {
		p, err := config.Parse(l.Override)
		if err != nil {
			return nil, err
		}
		if err := config.Merge(root, p); err != nil {
			return nil, err
		}
		if l.Transform != nil {
			if _, err := l.Transform(root); err != nil {
				return nil, err
			}
		}
	}
	return config.Encode(root)
}

// TestDeriveLayersRoundTrip: for random subscriptions and hand edits, the
// layers derived from the live config, composed on the base with the coexist
// layer, behave like the live config (live itself = base + edits + coexist).
func TestDeriveLayersRoundTrip(t *testing.T) {
	info := config.TailnetInfo{MagicDNSSuffix: "tn-0.ts.net", PeerShortNames: []string{"alpha", "beta"},
		PeerAddrs: map[string][]string{"alpha": {"100.64.0.1"}, "beta": {"100.64.0.2"}}, Interface: "utun7"}
	coexist, err := config.TailscaleCoexistLayer(info)
	if err != nil {
		t.Fatal(err)
	}
	checked, warned := 0, 0
	for seed := int64(0); seed < 500; seed++ {
		r := rand.New(rand.NewSource(seed))
		base := randomBase(r)
		var layers []config.Layer
		for i, n := 0, r.Intn(3); i < n; i++ {
			layers = append(layers, config.OverrideLayer(fmt.Sprintf("u%d", i), "u", config.KindUser, config.StageUser, []byte(randomUserLayer(r))))
		}
		withCoexist := r.Intn(4) != 0
		if withCoexist {
			layers = append(layers, coexist)
		}
		live, err := partyCompose(base, layers)
		if err != nil {
			t.Fatalf("seed %d: live: %v", seed, err)
		}
		der, err := DeriveLayers(live, []byte(base), info)
		if err != nil {
			t.Fatalf("seed %d: derive: %v", seed, err)
		}
		var dl []config.Layer
		for _, l := range der.Layers {
			dl = append(dl, l.Layer())
		}
		dl = append(dl, coexist)
		composed, err := config.Compose([]byte(base), dl)
		if err != nil {
			t.Fatalf("seed %d: compose: %v", seed, err)
		}
		sd, err := config.SemanticDiff(live, composed)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if len(der.Warnings) > 0 {
			warned++
		}
		if !withCoexist {
			// Composed adds the coexist layer: every difference must be
			// one the plan marks as deliberate (a Tailscale change).
			ms, err := newManagedSet(info)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range classify(sd, ms, Coexist{Resolver: resolverOf(info)}, coexistPrefixEquivalent(live, composed, coexist.Override)) {
				// Live routed ts.net through PROXY; coexist sends it DIRECT.
				// That is a real change and the plan must show it.
				// The edit list may show more lines (the dropped rule
				// reshapes same-target blocks); any rule edit is fine then.
				if c.Area == "rules" && strings.Contains(string(live), "DOMAIN-SUFFIX,ts.net,PROXY") {
					continue
				}
				if !c.Deliberate {
					t.Fatalf("seed %d: non-deliberate change %s: %s\n%s\nlive:\n%s\ncomposed:\n%s", seed, c.Area, c.What, sd.Text(20), live, composed)
				}
			}
			continue
		}
		if !sd.Equivalent() {
			t.Fatalf("seed %d: composed differs from live:\n%s\nbase:\n%s\nlive:\n%s\ncomposed:\n%s", seed, sd.Text(20), base, live, composed)
		}
	}
	if warned > 0 {
		t.Errorf("%d of %d derivations warned", warned, checked)
	}
	if checked != 500 {
		t.Fatalf("checked %d", checked)
	}
}
