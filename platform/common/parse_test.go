package common

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestParseRuleLineRegexWithCommas(t *testing.T) {
	r := ParseRuleLine(3, `DOMAIN-REGEX,^a{1,3}\.example$,PROXY`)
	if r.Type != "DOMAINREGEX" || r.Payload != `^a{1,3}\.example$` || r.Target != "PROXY" {
		t.Fatalf("%+v", r)
	}
}
