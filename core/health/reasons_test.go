package health

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// errEnv is fakeEnv with failing reads.
type errEnv struct {
	*fakeEnv
	ifsErr, svcErr, vpnErr error
}

func (e errEnv) Interfaces(ctx context.Context) ([]Interface, error) {
	if e.ifsErr != nil {
		return nil, e.ifsErr
	}
	return e.fakeEnv.Interfaces(ctx)
}
func (e errEnv) NetworkServices(ctx context.Context) ([]NetworkService, error) {
	if e.svcErr != nil {
		return nil, e.svcErr
	}
	return e.fakeEnv.NetworkServices(ctx)
}
func (e errEnv) VPNs(ctx context.Context) ([]VPN, error) {
	if e.vpnErr != nil {
		return nil, e.vpnErr
	}
	return e.fakeEnv.VPNs(ctx)
}

// reasonCase makes one failure reason happen (synthetic data only).
type reasonCase struct {
	check Check
	env   func() Env
}

func withRules(e *fakeEnv, rules []MihomoRule) *fakeEnv {
	e.rt.Rules, e.cfg.Rules = rules, rules
	return e
}

func setProxy(e *fakeEnv, p MihomoProxy) *fakeEnv {
	e.rt.Proxies[p.Name] = p
	return e
}

// reasonCases: one case per reason in the catalog. TestEveryReasonHasACase
// fails when a reason has none.
var reasonCases = map[string]reasonCase{
	ReasonReadFailed: {TailnetRoute{}, func() Env { return errEnv{fakeEnv: healthyEnv(), ifsErr: errors.New("boom")} }},
	ReasonRouteNoIface: {TailnetRoute{}, func() Env {
		e := healthyEnv()
		e.ifs = e.ifs[:2]
		return e
	}},
	ReasonRouteLookup: {TailnetRoute{}, func() Env {
		e := healthyEnv()
		delete(e.routes, "100.101.0.7")
		return e
	}},
	ReasonRouteIntoMihomo: {TailnetRoute{}, func() Env {
		e := healthyEnv()
		e.routes["100.101.0.7"] = RouteLookup{Interface: "utun5"}
		return e
	}},
	ReasonRouteElsewhere: {TailnetRoute{}, func() Env {
		e := healthyEnv()
		e.routes["100.101.0.7"] = RouteLookup{Interface: "en0"}
		return e
	}},
	ReasonExcludeMissing: {TUNExclude{}, func() Env {
		e := healthyEnv()
		e.rt.Configs.TUN.RouteExcludeAddress = []string{"192.168.0.0/16"}
		return e
	}},
	ReasonRulesGlobal: {TailscaleRules{}, func() Env {
		e := healthyEnv()
		e.rt.Configs.Mode = "global"
		return setProxy(e, MihomoProxy{Name: "GLOBAL", Type: "Selector", Now: "node-a", All: []string{"node-a", "DIRECT"}})
	}},
	ReasonRulesNotDirect: {TailscaleRules{}, func() Env {
		return withRules(healthyEnv(), []MihomoRule{{0, "MATCH", "", "PROXY"}})
	}},
	ReasonRulesShadowed: {TailscaleRules{}, func() Env {
		e := healthyEnv()
		return withRules(e, append([]MihomoRule{{0, "GEOSITE", "cn", "PROXY"}}, e.rt.Rules...))
	}},
	ReasonMagicNoResolver: {MagicDNS{}, func() Env {
		e := healthyEnv()
		e.res = e.res[:1]
		return e
	}},
	ReasonMagicSystem: {MagicDNS{}, func() Env {
		e := healthyEnv()
		e.system["server.tn.ts.net"] = DNSAnswer{Rcode: "NOERROR", Addrs: []netip.Addr{a("198.18.0.5")}}
		return e
	}},
	ReasonMagicMihomo: {MagicDNS{}, func() Env {
		e := healthyEnv()
		e.mihomo["nas"] = DNSAnswer{Rcode: "NOERROR", Addrs: []netip.Addr{a("198.18.0.9")}}
		return e
	}},
	ReasonNamesNone: {PeerNames{}, func() Env {
		e := healthyEnv()
		return withRules(e, slices.DeleteFunc(slices.Clone(e.rt.Rules), func(r MihomoRule) bool { return r.Type == "DOMAIN" }))
	}},
	ReasonNamesOutdated: {PeerNames{}, func() Env {
		e := healthyEnv()
		return withRules(e, append([]MihomoRule{{0, "DOMAIN", "oldbox", "DIRECT"}}, e.rt.Rules...))
	}},
	ReasonBindStale: {ResolverBinding{}, func() Env {
		e := bindEnv()
		for k := range e.cfg.DNS.NameserverPolicy {
			if k != "+.lan" {
				e.cfg.DNS.NameserverPolicy[k] = []string{"100.100.100.100#utun9"}
			}
		}
		e.routes["100.100.100.100"] = RouteLookup{Interface: "utun11"}
		return e
	}},
	ReasonBindMissing: {ResolverBinding{}, func() Env { return bindEnv() }},
	ReasonFakeIPWildcard: {FakeIPMode{}, func() Env {
		e := healthyEnv()
		e.cfg.DNS.FakeIPFilter, e.cfg.DNS.FakeIPFilterMode = []string{"*"}, "whitelist"
		return e
	}},
	ReasonDNSNotMihomo: {SystemDNS{}, func() Env {
		e := healthyEnv()
		e.svcs[1].DNS = []string{"192.168.1.1"}
		e.rt.Configs.TUN.DNSHijack = nil
		return e
	}},
	ReasonDNSViaHijack: {SystemDNS{}, func() Env {
		e := healthyEnv()
		e.svcs[1].DNS = []string{"192.0.2.53"}
		return e
	}},
	ReasonVPNOther: {VPNConflict{}, func() Env {
		e := healthyEnv()
		e.vpns[0].Status = "Connected"
		return e
	}},
	ReasonVPNTailscale: {VPNConflict{}, func() Env {
		e := healthyEnv()
		e.vpns = append(e.vpns, VPN{Name: "Tailscale", BundleID: "io.tailscale.ipn.macsys", Status: "Connected"})
		return e
	}},
	ReasonVPNExitNode: {VPNConflict{}, func() Env {
		e := healthyEnv()
		e.ts.ExitNode = &e.ts.Peers[0]
		return e
	}},
	ReasonVPNOtherTUN: {VPNConflict{}, func() Env {
		e := healthyEnv()
		e.routes["1.1.1.1"] = RouteLookup{Interface: "utun3"}
		return e
	}},
	ReasonVPNNotDefault: {VPNConflict{}, func() Env {
		e := healthyEnv()
		e.routes["1.1.1.1"] = RouteLookup{Interface: "en0"}
		return e
	}},
	ReasonLeakMode: {LeakGuard{}, func() Env {
		e := healthyEnv()
		e.rt.Configs.Mode = "global"
		return e
	}},
	ReasonLeakNoGroup: {LeakGuard{}, func() Env {
		e := healthyEnv()
		e.settings.SensitiveGroups = []string{"Nope"}
		return e
	}},
	ReasonLeakSelected: {LeakGuard{}, func() Env {
		return setProxy(healthyEnv(), MihomoProxy{Name: "Google", Type: "Selector", Now: "DIRECT", All: []string{"PROXY", "DIRECT"}})
	}},
	ReasonLeakMayGo: {LeakGuard{}, func() Env {
		return setProxy(healthyEnv(), MihomoProxy{Name: "Meta", Type: "Selector", Now: "node-a", All: []string{"node-a", "DIRECT"}})
	}},
	ReasonEgressFailed: {LeakEgress{}, func() Env {
		e := healthyEnv()
		e.ts.Self.Addrs = []string{"203.0.113.50:41641"}
		e.egressDirect, e.egressErr = netip.Addr{}, errors.New("blocked")
		return e
	}},
	ReasonEgressSame: {LeakEgress{}, func() Env {
		e := healthyEnv()
		e.egressProxied = e.egressDirect
		return e
	}},
	ReasonCtlExposed: {Controller{}, func() Env {
		e := healthyEnv()
		e.cfg.ExternalController = "0.0.0.0:9090"
		return e
	}},
	ReasonCtlNoSecret: {Controller{}, func() Env {
		e := healthyEnv()
		e.cfg.SecretSet = false
		return e
	}},
}

// TestEveryReasonHasAFix: each reason happens in its case, and has a fix
// with what that kind needs, worded in every language on every platform.
func TestEveryReasonHasAFix(t *testing.T) {
	for _, reason := range Reasons() {
		t.Run(reason, func(t *testing.T) {
			for _, p := range AllPlatforms {
				f, ok := FixFor(reason, p)
				if !ok {
					t.Fatalf("%s: no fix on %s", reason, p)
				}
				switch f.Kind {
				case FixAuto:
					if f.Action == "" {
						t.Errorf("%s@%s: one-click fix without an action", reason, p)
					}
				case FixGuided:
					if f.Target == "" {
						t.Errorf("%s@%s: guided fix without a target", reason, p)
					}
				case FixManual:
				default:
					t.Errorf("%s@%s: kind %q", reason, p, f.Kind)
				}
				for tag := range fixTextTables {
					tx, ok := FixTextFor(tag, p, reason)
					if !ok || strings.TrimSpace(tx.Plain) == "" || strings.TrimSpace(tx.How) == "" {
						t.Errorf("%s@%s: no %s text", reason, p, tag)
					}
				}
			}
		})
	}
}

// TestEveryReasonHasACase: every reason in the catalog is produced by a
// check, in a case of its own.
func TestEveryReasonHasACase(t *testing.T) {
	for _, reason := range Reasons() {
		t.Run(reason, func(t *testing.T) {
			switch reason {
			case ReasonCheckTimeout, ReasonCheckPanicked:
				r := &Runner{Registry: NewRegistry(), Timeout: 20 * time.Millisecond}
				r.Registry.Register(slowCheck{time.Second})
				r.Registry.Register(panicCheck{})
				rep := r.Run(context.Background(), healthyEnv())
				for _, c := range rep.Checks {
					if slices.Contains(c.Reasons, reason) {
						return
					}
				}
				t.Fatalf("no check reported %s", reason)
			}
			rc, ok := reasonCases[reason]
			if !ok {
				t.Fatalf("no case makes %s happen: add one to reasonCases", reason)
			}
			r := run(t, rc.check, rc.env())
			if !slices.Contains(r.Reasons, reason) {
				t.Fatalf("%s: got %s %v (%s), want reason %s", rc.check.ID(), r.Status, r.Reasons, r.Summary, reason)
			}
			if r.Status != StatusWarn && r.Status != StatusFail {
				t.Errorf("%s: status %s with a reason", rc.check.ID(), r.Status)
			}
			if !strings.HasPrefix(reason, rc.check.ID()+"/") && strings.Contains(reason, "/") {
				t.Errorf("%s comes from %s", reason, rc.check.ID())
			}
		})
	}
}

// TestProblemsHaveReasons: no warn or fail without a reason, so no row is
// left without a fix (every case's other reasons are in the catalog too).
func TestProblemsHaveReasons(t *testing.T) {
	for name, rc := range reasonCases {
		for _, p := range AllPlatforms {
			env := rc.env()
			if fe, ok := env.(*fakeEnv); ok {
				fe.platform = p
			}
			r := rc.check.Run(context.Background(), env)
			if (r.Status == StatusWarn || r.Status == StatusFail) && len(r.Reasons) == 0 {
				t.Errorf("%s on %s: %s without a reason: %s", name, p, r.Status, r.Summary)
			}
			for _, x := range r.Reasons {
				if _, ok := FixFor(x, p); !ok {
					t.Errorf("%s on %s: reason %q has no fix", name, p, x)
				}
			}
		}
	}
}

// TestReasonConstantsInCatalog parses reasons.go: a Reason* constant
// added without a catalog entry fails here.
func TestReasonConstantsInCatalog(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "reasons.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Reason") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				n++
				if _, ok := fixCatalog[v]; !ok {
					t.Errorf("%s (%q) has no entry in fixCatalog", name.Name, v)
				}
			}
		}
	}
	if n < 30 {
		t.Fatalf("found only %d reason constants: parser out of date?", n)
	}
}
