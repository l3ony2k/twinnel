package config

import "testing"

func TestBlame(t *testing.T) {
	sub := []byte("proxies:\n  - {name: node-a}\nproxy-groups:\n  - {name: PROXY, proxies: [node-a]}\n")
	layers := []Layer{
		{ID: "ok", Name: "局域网直连", Kind: KindUser, Enabled: true, Override: []byte("+rules:\n  - DOMAIN,a.example,DIRECT\n")},
		{ID: "bad", Name: "坏的覆写", Kind: KindUser, Enabled: true, Override: []byte("+proxy-groups:\n  - {name: G, type: select, proxies: [missing-node]}\n")},
		{ID: "off", Name: "停用", Kind: KindUser, Enabled: false, Override: []byte("x: missing-node\n")},
		{ID: "longer", Name: "长名", Kind: KindUser, Enabled: true, Override: []byte("x: missing-node-2\n")},
		{ID: "coexist", Name: "Tailscale", Kind: KindManaged, Enabled: true, Override: []byte("x: missing-node\n")},
	}
	got := Blame([]string{"proxy group[0]: 'missing-node' not found", `rule 3: "node-a" ok`, "'nowhere' not found"}, sub, layers)
	if len(got) != 2 || got[0] != (Culprit{Name: "missing-node", Layer: "bad", Title: "坏的覆写"}) || got[1] != (Culprit{Name: "node-a"}) {
		t.Fatalf("Blame = %+v", got)
	}
}
