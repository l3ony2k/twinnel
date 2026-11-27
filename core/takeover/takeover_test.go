package takeover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

const subBody = `proxies:
  - {name: n1, type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: pw-secret-1}
  - {name: n2, type: vmess, server: 192.0.2.2, port: 443, uuid: 11111111-2222-3333-4444-555555555555, cipher: auto}
proxy-groups:
  - {name: PROXY, type: select, proxies: [n1, n2, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.cn,DIRECT
  - GEOIP,CN,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`

const subFile = `port: 7890
allow-lan: true
mode: Rule
external-controller: 127.0.0.1:9090
` + subBody

const liveHeader = `# hand-written header
port: 7890
mixed-port: 7899
allow-lan: false
mode: rule
external-controller: 127.0.0.1:9090
secret: ""
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver: [https://doh.example/dns-query]
  fake-ip-filter:
    - '+.lan'
    - '+.ts.net'
    - '+.tailscale.com'
  nameserver-policy:
    '+.ts.net': [system]
    '+.lan': [system]
tun:
  enable: true
  auto-detect-interface: true
  route-exclude-address: [192.168.0.0/16, 100.64.0.0/10, fd7a:115c:a1e0::/48]
`

const liveRules = `rules:
  # My NAS
  - DOMAIN,nas.example.org,DIRECT
  # Tailscale
  - PROCESS-NAME,tailscaled,DIRECT
  - DOMAIN-SUFFIX,tailscale.com,DIRECT
  - DOMAIN-SUFFIX,ts.net,DIRECT
  - DOMAIN,derp1.tailscale.com,DIRECT
  - DOMAIN,alpha,DIRECT
  - DOMAIN,beta,DIRECT
  - DOMAIN,oldbox,DIRECT
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - IP-CIDR,100.100.100.100/32,DIRECT,no-resolve
  - IP-CIDR6,fd7a:115c:a1e0::/48,DIRECT,no-resolve
  - IP-CIDR6,fd00::/8,DIRECT,no-resolve
  - DOMAIN-SUFFIX,example.cn,DIRECT
  - GEOIP,CN,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`

const statusJSON = `{
  "Version": "1.2.3",
  "BackendState": "Running",
  "MagicDNSSuffix": "tailnet-0000.ts.net",
  "Self": {"DNSName": "alpha.tailnet-0000.ts.net.", "TailscaleIPs": ["100.64.0.1", "fd7a:115c:a1e0::1"]},
  "Peer": {"k1": {"DNSName": "beta.tailnet-0000.ts.net.", "TailscaleIPs": ["100.64.0.2"]}}
}`

type fakeProbe struct {
	dir string
}

func (f fakeProbe) Services(context.Context) ([]ServiceDef, error) {
	return []ServiceDef{
		{Label: "x.mihomo", Path: "/L/x.mihomo.plist", Kind: "launchd-daemon",
			Program: []string{filepath.Join(f.dir, "bin", "mihomo"), "-d", filepath.Join(f.dir, "conf"), "-f", "config.yaml"}},
		{Label: "x.tailscale", Path: "/L/x.ts.plist", Kind: "launchd-daemon", Program: []string{"/opt/bin/tailscaled"}},
	}, nil
}
func (f fakeProbe) MihomoVersion(context.Context, string, string) (string, error) {
	return "v1.19.0", nil
}
func (f fakeProbe) TailscaleStatus(context.Context) ([]byte, error) { return []byte(statusJSON), nil }
func (f fakeProbe) TailscaleInterface(context.Context) (string, error) {
	return "utun7", nil
}
func (f fakeProbe) TailscaleInstall() tailscale.Install {
	return tailscale.Install{Kind: tailscale.InstallTailscaled, Source: "brew"}
}
func (f fakeProbe) UserHome() string { return f.dir }

func setup(t *testing.T, live string) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"conf", "backup", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("conf/config.yaml", live)
	// A backup taken just before the NAS rule: explains live too, but is
	// further from a raw subscription.
	write("conf/config.yaml.bak-before-nas", strings.Replace(live, "  # My NAS\n  - DOMAIN,nas.example.org,DIRECT\n", "", 1))
	write("backup/raw-sub.yaml", subFile)
	write("backup/notes.yaml", "a: 1\n")
	write("backup/index.json", `[{"name":"main","source":"https://sub.example/clash/tok-SECRET","kind":"subscription"}]`)
	write("conf/geoip.metadb", "x")
	return dir
}

func livePlan(t *testing.T, live string) (*Plan, string) {
	dir := setup(t, live)
	p, err := BuildPlan(context.Background(), fakeProbe{dir}, Options{NoValidate: true,
		Now: func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return p, dir
}

func TestBuildPlan(t *testing.T) {
	live := liveHeader + subBody[:strings.Index(subBody, "rules:")] + liveRules
	p, dir := livePlan(t, live)

	if p.Base == nil || filepath.Base(p.Base.Path) != "raw-sub.yaml" || !p.Base.Explains {
		t.Fatalf("base: %+v\ncandidates: %+v", p.Base, p.Candidates)
	}
	m := p.Discovery.Mihomo
	if m.Version != "v1.19.0" || m.Config != filepath.Join(dir, "conf", "config.yaml") || len(m.GeoFiles) != 1 {
		t.Fatalf("mihomo: %+v", m)
	}
	if len(m.Profiles) != 1 || strings.Contains(m.Profiles[0].Source, "tok-SECRET") {
		t.Fatalf("profiles: %+v", m.Profiles)
	}
	if p.Discovery.Tailscale.Interface != "utun7" || p.Coexist.Resolver != "100.100.100.100#utun7" {
		t.Fatalf("coexist: %+v", p.Coexist)
	}

	var ids []string
	for _, l := range p.Layers {
		ids = append(ids, l.ID)
	}
	// "oldbox" is a single-label DIRECT rule for a name that is not a
	// current peer: it is kept as a user layer, not dropped as a Tailscale
	// rule (dropping it would route "oldbox" by later rules).
	if strings.Join(ids, ",") != "base,user-1,user-2,user-3,tailscale-coexist" {
		t.Fatalf("layers: %v", ids)
	}
	baseLayer := string(p.Layers[0].Data())
	for _, want := range []string{"mixed-port: 7899", "mode: rule", "'<+.lan>':", "192.168.0.0/16"} {
		if !strings.Contains(baseLayer, want) {
			t.Errorf("base layer lacks %q:\n%s", want, baseLayer)
		}
	}
	for _, bad := range []string{"ts.net", "tailscale.com", "100.64.0.0/10", "\nport: 7890", "external-controller"} {
		if strings.Contains(baseLayer, bad) {
			t.Errorf("base layer has %q:\n%s", bad, baseLayer)
		}
	}
	// user-1 is applied first and lands below user-2 (prepends).
	if !strings.Contains(string(p.Layers[1].Data()), "fd00::/8") || !strings.Contains(string(p.Layers[2].Data()), "DOMAIN,oldbox,DIRECT") ||
		!strings.Contains(string(p.Layers[3].Data()), "nas.example.org") {
		t.Fatalf("user layers:\n%s\n%s\n%s", p.Layers[1].Data(), p.Layers[2].Data(), p.Layers[3].Data())
	}
	if p.Layers[3].Name != "My NAS" {
		t.Errorf("layer name %q", p.Layers[3].Name)
	}
	replaced := map[string]int{}
	for _, r := range p.Replaced {
		replaced[r.Path]++
	}
	if replaced["rules"] != 9 || replaced["dns.nameserver-policy"] != 1 || replaced["dns.fake-ip-filter"] != 2 || replaced["tun.route-exclude-address"] != 2 {
		t.Fatalf("replaced: %v\n%+v", replaced, p.Replaced)
	}

	if !p.Equivalent {
		t.Fatalf("want equivalent except deliberate:\n%s", p.Text())
	}
	areas := map[string]bool{}
	for _, c := range p.Changes {
		areas[c.Area] = true
	}
	for _, a := range []string{"dns.nameserver-policy", "dns.fake-ip-filter", "hosts", "rules"} {
		if !areas[a] {
			t.Errorf("missing deliberate change %s:\n%s", a, p.Text())
		}
	}
	// The live rules lack tailscale.io / beta.tailscale.net: both edits are
	// the coexist layer's. The stale peer "oldbox" is kept (user layer).
	if p.SemDiff.Rules.Equivalent || !strings.Contains(p.Text(), "2 Tailscale rule edit(s)") || strings.Contains(p.Text(), "- DOMAIN,oldbox") {
		t.Fatalf("rules:\n%s", p.Text())
	}

	out := filepath.Join(t.TempDir(), "plan")
	if err := p.WriteFiles(out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"plan.json", "composed.yaml", "semdiff.txt", "summary.txt", "layers/00-base.yaml", "layers/04-tailscale-coexist.yaml"} {
		fi, err := os.Stat(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, fi.Mode())
		}
	}
	for _, f := range []string{"plan.json", "semdiff.txt", "summary.txt"} {
		b, _ := os.ReadFile(filepath.Join(out, f))
		for _, s := range []string{"pw-secret-1", "11111111-2222", "tok-SECRET", "192.0.2.1"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s leaks %q", f, s)
			}
		}
	}
	// The composed config does carry the credentials (it is deployable).
	if b, _ := os.ReadFile(filepath.Join(out, "composed.yaml")); !strings.Contains(string(b), "pw-secret-1") {
		t.Error("composed config lost the proxies")
	}
}

func TestBuildPlanUnexpected(t *testing.T) {
	// A user rule sending ts.net through the proxy above the Tailscale rules
	// can't be kept above the coexist layer: the plan must flag it.
	rules := strings.Replace(liveRules, "  # My NAS\n", "  - DOMAIN-SUFFIX,funnel.ts.net,PROXY\n", 1)
	live := liveHeader + subBody[:strings.Index(subBody, "rules:")] + rules
	p, _ := livePlan(t, live)
	if p.Equivalent {
		t.Fatalf("want unexpected change:\n%s", p.Text())
	}
	if !strings.Contains(p.Text(), "[UNEXPECTED] rules: - DOMAIN-SUFFIX,funnel.ts.net,PROXY") ||
		strings.Contains(p.Text(), "[UNEXPECTED] port") {
		t.Fatalf("text:\n%s", p.Text())
	}
}

func TestBuildPlanBaseOnlyKey(t *testing.T) {
	// The base sets "port", the live config doesn't: flagged, not hidden.
	live := strings.Replace(liveHeader, "port: 7890\n", "", 1) + subBody[:strings.Index(subBody, "rules:")] + liveRules
	p, _ := livePlan(t, live)
	if p.Equivalent || !strings.Contains(p.Text(), "[UNEXPECTED] port: added 7890") {
		t.Fatalf("text:\n%s", p.Text())
	}
}

func TestBuildPlanExplicitBase(t *testing.T) {
	live := liveHeader + subBody[:strings.Index(subBody, "rules:")] + liveRules
	dir := setup(t, live)
	bak := filepath.Join(dir, "conf", "config.yaml.bak-before-nas")
	p, err := BuildPlan(context.Background(), fakeProbe{dir}, Options{NoValidate: true, BasePath: bak})
	if err != nil {
		t.Fatal(err)
	}
	if p.Base.Path != bak || !p.Base.Explicit {
		t.Fatalf("base %+v", p.Base)
	}
	// Everything but the NAS rule is already in that base.
	if len(p.Layers) != 3 || !p.Equivalent {
		t.Fatalf("layers %d equivalent %v\n%s", len(p.Layers), p.Equivalent, p.Text())
	}
	_, err = BuildPlan(context.Background(), fakeProbe{dir}, Options{NoValidate: true, BasePath: filepath.Join(dir, "nope.yaml")})
	if err == nil {
		t.Fatal("missing base must fail")
	}
}

func TestConfigFromArgs(t *testing.T) {
	cases := []struct {
		args      []string
		dir, file string
	}{
		{[]string{"mihomo", "-d", "/c", "-f", "/c/x.yaml"}, "/c", "/c/x.yaml"},
		{[]string{"mihomo", "-d=/c"}, "/c", "/c/config.yaml"},
		{[]string{"mihomo", "-f", "/a/b.yaml"}, "/a", "/a/b.yaml"},
		{[]string{"mihomo", "-d", "/c", "-f", "b.yaml"}, "/c", "/c/b.yaml"},
		{[]string{"mihomo"}, "", ""},
	}
	for _, c := range cases {
		d, f := ConfigFromArgs(c.args)
		if d != c.dir || f != c.file {
			t.Errorf("%v: %q %q", c.args, d, f)
		}
	}
}

func TestEscapeKey(t *testing.T) {
	for in, want := range map[string]string{"+.lan": "<+.lan>", "a+": "<a+>", "x!": "<x!>", "plain": "plain", "*": "*"} {
		if got := escapeKey(in, false); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
	if got := escapeKey("dns!", true); got != "dns!" {
		t.Error(got)
	}
	if got := escapeKey("+.lan!", true); got != "<+.lan>!" {
		t.Error(got)
	}
}

func TestNeedsReplaceUsesBang(t *testing.T) {
	live := []byte("dns: {enable: true}\nproxies: [{name: a, type: direct}]\nrules: ['MATCH,DIRECT']\n")
	base := []byte("dns: {enable: false, listen: ':53'}\nproxies: [{name: a, type: direct}]\nrules: ['MATCH,DIRECT']\n")
	d, err := DeriveLayers(live, base, config.TailnetInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Layers[0].Data()), "dns!:") {
		t.Fatalf("want dns! replace:\n%s", d.Layers[0].Data())
	}
	got, err := config.Compose(base, []config.Layer{d.Layers[0].Layer()})
	if err != nil {
		t.Fatal(err)
	}
	sd, err := config.SemanticDiff(live, got)
	if err != nil {
		t.Fatal(err)
	}
	if !sd.Equivalent() {
		t.Fatalf("not equivalent:\n%s", sd.Text(0))
	}
}

func TestScoreExplainsNeedsSameProviders(t *testing.T) {
	cfg := "proxies:\n  - {name: a, type: ss}\nproxy-groups:\n  - {name: G, type: select, proxies: [a]}\nrule-providers:\n  r: {type: http, url: 'https://example.invalid/%s'}\nrules: ['RULE-SET,r,G', 'MATCH,G']\n"
	live, _ := decodeDoc([]byte(fmt.Sprintf(cfg, "new")))
	same, _ := decodeDoc([]byte(fmt.Sprintf(cfg, "new")))
	old, _ := decodeDoc([]byte(fmt.Sprintf(cfg, "old")))
	var c1, c2 Candidate
	scoreDoc(live, same, &c1)
	scoreDoc(live, old, &c2)
	if !c1.Explains || c2.Explains {
		t.Fatalf("explains: same %v, old provider %v", c1.Explains, c2.Explains)
	}
}

// Finding 13: discovery runs as root in the helper. A candidate that is a
// symlink out of its dir is not read, and a symlinked backup dir is not
// searched.
func TestDiscoveryRefusesEscapingSymlinks(t *testing.T) {
	live := liveHeader + subBody[:strings.Index(subBody, "rules:")] + liveRules
	dir := setup(t, live)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "victim.yaml"), []byte(subFile), 0o600)
	if err := os.Symlink(filepath.Join(outside, "victim.yaml"), filepath.Join(dir, "conf", "evil.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "conf", "backup-link")); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), fakeProbe{dir}, Options{NoValidate: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Candidates {
		if strings.Contains(c.Path, "backup-link") {
			t.Errorf("searched a symlinked dir: %s", c.Path)
		}
		if strings.HasSuffix(c.Path, "evil.yaml") && (c.Error == "" || c.ProxiesTotal > 0) {
			t.Errorf("read a symlink leaving the dir: %+v", c)
		}
	}
	if filepath.Base(p.Base.Path) != "raw-sub.yaml" {
		t.Errorf("base %s", p.Base.Path)
	}
}

// A live config written while MagicDNS was on still has the quad-100
// policy; with MagicDNS off now those entries are still the coexist layer's.
func TestManagedSetMagicDNSOff(t *testing.T) {
	ms, err := newManagedSet(config.TailnetInfo{MagicDNSSuffix: "tn.ts.net", MagicDNSOff: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"+.tn.ts.net", "*", "+.64.100.in-addr.arpa", "+.ts.net"} {
		if !ms.policy[k] {
			t.Errorf("policy key %q not recognised", k)
		}
	}
}
