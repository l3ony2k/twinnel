package health

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
)

// DefaultRegistry returns a registry with all built-in checks, in report order.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	for _, c := range []Check{
		TailnetRoute{}, TUNExclude{}, TailscaleRules{}, MagicDNS{}, PeerNames{},
		ResolverBinding{}, FakeIPMode{},
		SystemDNS{}, VPNConflict{}, LeakGuard{}, LeakEgress{}, Controller{},
	} {
		r.Register(c)
	}
	return r
}

// tunConfig returns the TUN settings, preferring the running mihomo.
func tunConfig(ctx context.Context, env Env) (TUNConfig, string, error) {
	L := tx(ctx)
	if rt, err := env.MihomoRuntime(ctx); err == nil {
		return rt.Configs.TUN, L.srcRuntime, nil
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return TUNConfig{}, "", err
	}
	return cfg.TUN, L.srcFile, nil
}

// mihomoRules returns the rules, preferring the running mihomo.
func mihomoRules(ctx context.Context, env Env) ([]MihomoRule, string, error) {
	L := tx(ctx)
	if rt, err := env.MihomoRuntime(ctx); err == nil && len(rt.Rules) > 0 {
		return rt.Rules, L.srcRuntime, nil
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg.Rules, L.srcFile, nil
}

func runningTailscale(ctx context.Context, env Env) (*TailscaleStatus, *Result) {
	L := tx(ctx)
	ts, err := env.Tailscale(ctx)
	if err != nil {
		r := skipErr(L.msgTSUnavailable, err)
		return nil, &r
	}
	if ts.BackendState != "Running" {
		r := skipErr(L.msgTSNotRunning, "BackendState="+ts.BackendState)
		return nil, &r
	}
	return ts, nil
}

// ---------------------------------------------------------------------------

// TailnetRoute: tailnet traffic is routed to Tailscale's tun, not mihomo's.
type TailnetRoute struct{}

func (TailnetRoute) ID() string            { return "tailnet-route" }
func (c TailnetRoute) Title() string       { return c.title(&zhTexts) }
func (TailnetRoute) title(L *texts) string { return L.titleTailnetRoute }
func (TailnetRoute) Platforms() []string   { return AllPlatforms }

func (TailnetRoute) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	ifs, err := env.Interfaces(ctx)
	if err != nil {
		return Result{Status: StatusFail, Summary: fmt.Sprintf(L.msgReadFailed, "interfaces", err)}
	}
	tsIf := ""
	for _, a := range ts.TailscaleIPs {
		if tsIf = interfaceWith(ifs, a); tsIf != "" {
			break
		}
	}
	if tsIf == "" {
		if !ts.TUN {
			return skipText(L.msgTSUserspace)
		}
		return Result{Status: StatusFail, Summary: L.msgTSNoIface}
	}
	mihomoIf := ""
	if t, _, err := tunConfig(ctx, env); err == nil && t.Enable {
		mihomoIf = t.Device
	}

	dests := []netip.Addr{QuadAddr}
	n := 0
	for _, p := range ts.Peers {
		if p.Online && n < 3 {
			for _, a := range p.TailscaleIPs {
				if a.Is4() {
					dests = append(dests, a)
					n++
					break
				}
			}
		}
	}
	for _, a := range ts.TailscaleIPs {
		if a.Is6() {
			dests = append(dests, QuadAddrV6)
			break
		}
	}

	res := Result{Status: StatusOK, Summary: fmt.Sprintf(L.msgRouteOK, tsIf)}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evTSIface, tsIf, mihomoIfLabel(mihomoIf)))
	var bad []string
	for _, d := range dests {
		rl, err := env.RouteGet(ctx, d.String())
		if err != nil {
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %v", d, err))
			res.Status = Worse(res.Status, StatusWarn)
			continue
		}
		line := fmt.Sprintf("%s → %s", d, rl.Interface)
		if rl.Gateway != "" {
			line += " via " + rl.Gateway
		}
		switch {
		case rl.Interface == tsIf:
		case mihomoIf != "" && rl.Interface == mihomoIf:
			line += "  ← " + L.evIntoMihomo
			bad = append(bad, d.String())
		default:
			line += "  ← " + L.evNotTS
			bad = append(bad, d.String())
		}
		res.Evidence = append(res.Evidence, line)
	}
	if routes, err := env.Routes(ctx); err == nil {
		for _, rt := range routes {
			if rt.Interface == tsIf && (rt.Destination == "100.64/10" || rt.Destination == "100.64.0.0/10" || strings.HasPrefix(rt.Destination, "fd7a:115c:a1e0::/48")) {
				res.Evidence = append(res.Evidence, fmt.Sprintf(L.evRouteEntry, rt.Destination, rt.Interface))
			}
		}
	}
	if len(bad) > 0 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgRouteBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: L.fixRouteBad, AutoFix: FixCoexist}
	}
	return res
}

func mihomoIfLabel(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------------------------------------------------------------------------

// TUNExclude: mihomo's TUN route-exclude-address covers the tailnet ranges.
type TUNExclude struct{}

func (TUNExclude) ID() string            { return "tun-route-exclude" }
func (c TUNExclude) Title() string       { return c.title(&zhTexts) }
func (TUNExclude) title(L *texts) string { return L.titleTUNExclude }
func (TUNExclude) Platforms() []string   { return AllPlatforms }

func (TUNExclude) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	t, src, err := tunConfig(ctx, env)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	if !t.Enable {
		return skipText(L.msgTUNOff)
	}
	res := Result{Status: StatusOK, Summary: L.msgExcludeOK}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evExcludeList, src, strings.Join(t.RouteExcludeAddress, ", ")))
	var missing []string
	for _, want := range []netip.Prefix{TailnetV4, TailnetV6} {
		if by, ok := covers(t.RouteExcludeAddress, want); ok {
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evCoveredBy, want, by))
		} else {
			missing = append(missing, want.String())
		}
	}
	if len(missing) > 0 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgExcludeMissing, strings.Join(missing, ", "))
		res.Fix = &FixHint{Text: L.fixExclude, AutoFix: FixCoexist}
	}
	return res
}

// ---------------------------------------------------------------------------

// TailscaleRules: tailscaled, control plane, DERP and tailnet IPs hit DIRECT
// before any proxy rule.
type TailscaleRules struct{}

func (TailscaleRules) ID() string            { return "tailscale-direct-rules" }
func (c TailscaleRules) Title() string       { return c.title(&zhTexts) }
func (TailscaleRules) title(L *texts) string { return L.titleTailscaleRules }
func (TailscaleRules) Platforms() []string   { return AllPlatforms }

func (TailscaleRules) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	rules, src, err := mihomoRules(ctx, env)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	// Rules only apply in rule mode: global sends everything to GLOBAL,
	// direct sends everything DIRECT.
	if rt, err := env.MihomoRuntime(ctx); err == nil {
		switch strings.ToLower(rt.Configs.Mode) {
		case "direct":
			return Result{Status: StatusOK, Summary: L.msgRulesDirect}
		case "global":
			ch := followNow(rt.Proxies, "GLOBAL")
			final := ch[len(ch)-1]
			if final != "DIRECT" {
				return Result{Status: StatusFail, Summary: fmt.Sprintf(L.msgRulesGlobal, final),
					Evidence: []string{strings.Join(ch, " → ")}, Fix: &FixHint{Text: L.fixRulesGlobal}}
			}
			return Result{Status: StatusOK, Summary: L.msgRulesDirect, Evidence: []string{strings.Join(ch, " → ")}}
		}
	}
	if len(rules) == 0 {
		return skipText(L.msgNoRules)
	}
	tsProcess := "tailscaled"
	if env.Platform() == Windows {
		tsProcess = "tailscaled.exe" // mihomo matches the executable's base name
	}
	probes := []probe{
		{process: tsProcess},
		{domain: "controlplane.tailscale.com"},
		{domain: "login.tailscale.com"},
		{domain: "derp1.tailscale.com"},
		{ip: netip.MustParseAddr("100.100.100.100")},
		{ip: netip.MustParseAddr("fd7a:115c:a1e0::53")},
	}
	res := Result{Status: StatusOK, Summary: L.msgRulesOK}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evRuleCount, src, len(rules)))
	var bad, risky []string
	for _, p := range probes {
		first, shadow := evalRules(rules, p)
		switch {
		case first == nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s", p, L.evNoMatch))
			bad = append(bad, p.String())
		case first.Target != "DIRECT":
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s  ← %s", p, ruleString(*first), L.evNotDirect))
			bad = append(bad, p.String())
		case len(shadow) > 0:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s  ← "+L.evShadowed, p, ruleString(*first), ruleString(shadow[0])))
			risky = append(risky, p.String())
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s", p, ruleString(*first)))
		}
	}
	switch {
	case len(bad) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgRulesBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: L.fixRules, AutoFix: FixCoexist}
	case len(risky) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(L.msgRulesRisky, strings.Join(risky, ", "))
		res.Fix = &FixHint{Text: L.fixRules, AutoFix: FixCoexist}
	}
	return res
}

// ---------------------------------------------------------------------------

// MagicDNS: MagicDNS names resolve to tailnet IPs, not fake-ips, through
// both the system resolver and mihomo's DNS.
type MagicDNS struct{}

func (MagicDNS) ID() string            { return "magicdns" }
func (c MagicDNS) Title() string       { return c.title(&zhTexts) }
func (MagicDNS) title(L *texts) string { return L.titleMagicDNS }
func (MagicDNS) Platforms() []string   { return AllPlatforms }

func (MagicDNS) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	if !ts.MagicDNS {
		return skipText(L.msgMagicDNSOff)
	}
	cfg, _ := env.MihomoConfig(ctx)
	fake := fakeIPRange(cfg)

	type name struct {
		n     string
		short bool
	}
	var names []name
	if self := strings.TrimSuffix(ts.Self.DNSName, "."); self != "" {
		names = append(names, name{self, false})
	}
	n := 0
	for _, p := range ts.Peers {
		if !p.Online || p.DNSName == "" || n >= 2 {
			continue
		}
		names = append(names, name{strings.TrimSuffix(p.DNSName, "."), false}, name{p.ShortName(), true})
		n++
	}

	// Look everything up in parallel: a slow mihomo answer costs seconds.
	type lookup struct {
		sys, mh       DNSAnswer
		sysErr, mhErr error
	}
	found := make([]lookup, len(names))
	var wg sync.WaitGroup
	for i, nm := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found[i].sys, found[i].sysErr = env.LookupSystem(ctx, nm.n)
			found[i].mh, found[i].mhErr = env.LookupMihomo(ctx, nm.n)
		}()
	}
	wg.Wait()

	res := Result{Status: StatusOK, Summary: L.msgMagicOK}
	var sysFail, mihomoFake, mihomoNoAnswer []string
	for i, nm := range names {
		sys, err := found[i].sys, found[i].sysErr
		switch {
		case err != nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evSys, nm.n, err.Error()))
			sysFail = append(sysFail, nm.n)
		case anyIn(sys.Addrs, fake):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evSys, nm.n, addrs(L, sys)+"  ← fake-ip"))
			sysFail = append(sysFail, nm.n)
		case !anyTailnet(sys.Addrs):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evSys, nm.n, addrs(L, sys)+"  ← "+L.evNotTailnet))
			sysFail = append(sysFail, nm.n)
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evSys, nm.n, addrs(L, sys)))
		}
		mh, err := found[i].mh, found[i].mhErr
		switch {
		case errors.Is(err, ErrDNSTimeout):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evMihomo, nm.n, L.evDNSTimeout))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		case err != nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evMihomo, nm.n, err.Error()))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		case anyIn(mh.Addrs, fake):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evMihomo, nm.n, addrs(L, mh)+"  ← fake-ip"))
			mihomoFake = append(mihomoFake, nm.n)
		case !anyTailnet(mh.Addrs):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evMihomo, nm.n, addrs(L, mh)))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evMihomo, nm.n, addrs(L, mh)))
		}
	}

	// Resolver configuration: a scoped resolver for the tailnet suffix
	// pointing at 100.100.100.100, and reverse zones for 100.x.
	if rs, err := env.Resolvers(ctx); err == nil {
		suffixOK, reverse := false, 0
		for _, rv := range rs {
			toQuad := false
			for _, ns := range rv.Nameservers {
				if ns == QuadAddr.String() || ns == QuadAddrV6.String() {
					toQuad = true
				}
			}
			if !toQuad {
				continue
			}
			if rv.Domain == ts.MagicDNSSuffix || rv.Domain == "ts.net" {
				suffixOK = true
			}
			if strings.HasSuffix(rv.Domain, ".100.in-addr.arpa") {
				reverse++
			}
		}
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evResolverCfg, ts.MagicDNSSuffix, yesNo(L, suffixOK), reverse))
		if !suffixOK {
			sysFail = append(sysFail, ts.MagicDNSSuffix+" resolver")
		}
	}
	if cfg != nil {
		for _, k := range []string{"+." + strings.Trim(ts.MagicDNSSuffix, "."), "+.ts.net", "*"} {
			if pol, ok := cfg.DNS.NameserverPolicy[k]; ok {
				res.Evidence = append(res.Evidence, fmt.Sprintf(L.evPolicy, k, strings.Join(pol, ", ")))
			}
		}
	}

	switch {
	case len(sysFail) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgMagicSysFail, strings.Join(uniq(sysFail), ", "))
		res.Fix = &FixHint{Text: L.fixMagicSys}
	case len(mihomoFake) > 0 || len(mihomoNoAnswer) > 0:
		res.Status = StatusWarn
		var parts []string
		if len(mihomoFake) > 0 {
			parts = append(parts, fmt.Sprintf(L.msgMagicMihomoFake, strings.Join(mihomoFake, ", ")))
		}
		if len(mihomoNoAnswer) > 0 {
			parts = append(parts, fmt.Sprintf(L.msgMagicMihomoNone, strings.Join(mihomoNoAnswer, ", ")))
		}
		res.Summary = strings.Join(parts, "；")
		fix := L.fixMagicMihomo
		if cfg != nil {
			pol := cfg.DNS.NameserverPolicy["+."+strings.Trim(ts.MagicDNSSuffix, ".")]
			if len(pol) == 0 {
				pol = cfg.DNS.NameserverPolicy["+.ts.net"]
			}
			if len(pol) == 1 && pol[0] == "system" {
				fix = L.fixMagicSystemLoop + fix
			}
		}
		res.Fix = &FixHint{Text: fix, AutoFix: FixCoexist}
	}
	return res
}

func anyIn(as []netip.Addr, p netip.Prefix) bool {
	for _, a := range as {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func anyTailnet(as []netip.Addr) bool {
	for _, a := range as {
		if TailnetV4.Contains(a) || TailnetV6.Contains(a) {
			return true
		}
	}
	return false
}

func addrs(L *texts, a DNSAnswer) string {
	if len(a.Addrs) == 0 {
		if a.Rcode == "" {
			return L.evEmpty
		}
		return a.Rcode + " " + L.evEmpty
	}
	var s []string
	for _, x := range a.Addrs {
		s = append(s, x.String())
	}
	return strings.Join(s, ", ")
}

func yesNo(L *texts, b bool) string {
	if b {
		return L.labelYes
	}
	return L.labelNo
}

// ---------------------------------------------------------------------------

// PeerNames: tailnet short names written in rules match current peers.
type PeerNames struct{}

func (PeerNames) ID() string            { return "peer-short-names" }
func (c PeerNames) Title() string       { return c.title(&zhTexts) }
func (PeerNames) title(L *texts) string { return L.titlePeerNames }
func (PeerNames) Platforms() []string   { return AllPlatforms }

func (PeerNames) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	rules, src, err := mihomoRules(ctx, env)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	inRules := map[string]bool{}
	for _, ru := range rules {
		if ru.Type == "DOMAIN" && ru.Target == "DIRECT" && ru.Payload != "" && !strings.Contains(ru.Payload, ".") {
			inRules[strings.ToLower(ru.Payload)] = true
		}
	}
	peers := map[string]bool{}
	for _, p := range append([]TailscalePeer{ts.Self}, ts.Peers...) {
		if s := strings.ToLower(p.ShortName()); s != "" {
			peers[s] = true
		}
	}
	var missing, stale []string
	for p := range peers {
		if !inRules[p] {
			missing = append(missing, p)
		}
	}
	for p := range inRules {
		if !peers[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	res := Result{Status: StatusOK, Summary: fmt.Sprintf(L.msgNamesOK, len(peers))}
	res.Evidence = append(res.Evidence,
		fmt.Sprintf(L.evNamesPeers, strings.Join(sortedKeys(peers), ", ")),
		fmt.Sprintf(L.evNamesRules, src, strings.Join(sortedKeys(inRules), ", ")))
	if len(inRules) == 0 {
		res.Status = StatusWarn
		res.Summary = L.msgNamesNone
		res.Fix = &FixHint{Text: L.fixNames, AutoFix: FixCoexist}
		return res
	}
	if len(missing) > 0 || len(stale) > 0 {
		res.Status = StatusWarn
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, fmt.Sprintf(L.msgNamesMissing, strings.Join(missing, ", ")))
		}
		if len(stale) > 0 {
			parts = append(parts, fmt.Sprintf(L.msgNamesStale, strings.Join(stale, ", ")))
		}
		res.Summary = strings.Join(parts, "；")
		res.Fix = &FixHint{Text: L.fixNames, AutoFix: FixCoexist}
	}
	return res
}

func sortedKeys(m map[string]bool) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// ---------------------------------------------------------------------------

// SystemDNS: every active network service resolves through mihomo.
type SystemDNS struct{}

func (SystemDNS) ID() string            { return "system-dns" }
func (c SystemDNS) Title() string       { return c.title(&zhTexts) }
func (SystemDNS) title(L *texts) string { return L.titleSystemDNS }
func (SystemDNS) Platforms() []string   { return AllPlatforms }

func (SystemDNS) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	if !cfg.DNS.Enable {
		return skipText(L.msgMihomoDNSOff)
	}
	listen := hostOf(cfg.DNS.Listen)
	want := map[string]bool{listen: true}
	if listen == "0.0.0.0" || listen == "" || listen == "::" {
		want["127.0.0.1"] = true
	}
	if env.Platform() == Windows && want["127.0.0.1"] {
		// dns.point sets ::1 as the IPv6 server on Windows: nothing
		// listens there, so IPv6 queries fail on the host instead of
		// leaving it (platform/windows/sysdns.go).
		want["::1"] = true
	}
	tun, _, _ := tunConfig(ctx, env)
	hijack := hijacksAll53(tun)

	svcs, err := env.NetworkServices(ctx)
	if err != nil {
		return Result{Status: StatusFail, Summary: fmt.Sprintf(L.msgReadFailed, "network services", err)}
	}
	ifs, _ := env.Interfaces(ctx)
	active := map[string]bool{}
	for _, i := range ifs {
		if i.Up && i.Active {
			for _, a := range i.Addrs {
				if a.Is4() && !a.IsLinkLocalUnicast() {
					active[i.Name] = true
				}
			}
		}
	}
	// The default resolver shows what DHCP handed out for services without
	// manual DNS.
	var defaultNS []string
	if rs, err := env.Resolvers(ctx); err == nil {
		for _, rv := range rs {
			if rv.Domain == "" && !rv.Scoped && len(rv.Nameservers) > 0 {
				defaultNS = rv.Nameservers
				break
			}
		}
	}

	res := Result{Status: StatusOK, Summary: fmt.Sprintf(L.msgDNSOK, cfg.DNS.Listen)}
	if strings.TrimSpace(cfg.DNS.Listen) == "" {
		res.Summary = L.msgDNSOKNoListen
	}
	var bad, viaHijack []string
	nActive := 0
	for _, s := range svcs {
		if s.Device == "" {
			continue // VPN services are covered by vpn-conflict
		}
		if !s.Enabled || !active[s.Device] {
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evSvcInactive, s.Name, s.Device))
			continue
		}
		nActive++
		servers, how := s.DNS, dnsSourceLabel(L, s)
		if len(servers) == 0 {
			servers = defaultNS
		}
		ok := len(servers) > 0
		for _, ns := range servers {
			if !want[ns] {
				ok = false
			}
		}
		line := fmt.Sprintf(L.evSvc, s.Name, s.Device, strings.Join(servers, ", "), how)
		if ok {
			res.Evidence = append(res.Evidence, line)
			continue
		}
		// Not pointing at mihomo: dns-hijack only catches queries that enter
		// the TUN, i.e. not to servers in route-exclude-address.
		excluded := false
		for _, ns := range servers {
			if a, err := netip.ParseAddr(ns); err == nil && inAny(tun.RouteExcludeAddress, a) {
				excluded = true
			}
		}
		if hijack && !excluded {
			res.Evidence = append(res.Evidence, line+"  ← "+L.evHijackCovers)
			viaHijack = append(viaHijack, s.Name)
		} else {
			if excluded {
				line += "  ← " + L.evExcludedDNS
			}
			res.Evidence = append(res.Evidence, line)
			bad = append(bad, s.Name)
		}
	}
	if len(defaultNS) > 0 {
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evDefaultResolver, strings.Join(defaultNS, ", ")))
	}
	switch {
	case len(bad) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgDNSBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: dnsFixText(L, env.Platform(), listen), AutoFix: FixSystemDNS}
	case len(viaHijack) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(L.msgDNSHijack, strings.Join(viaHijack, ", "))
		res.Fix = &FixHint{Text: dnsFixText(L, env.Platform(), listen), AutoFix: FixSystemDNS}
	case nActive == 0:
		// Offline: nothing to check, nothing to fix here.
		res.Status = StatusSkip
		res.Summary = L.msgDNSNoActive
	}
	return res
}

// dnsSourceLabel says where a service's DNS servers come from. Without a
// DNSSource (macOS) configured servers are manual and none means DHCP.
func dnsSourceLabel(L *texts, s NetworkService) string {
	switch s.DNSSource {
	case DNSSourceManual:
		return L.labelManual
	case DNSSourceAuto:
		return L.labelDHCP
	case DNSSourceMixed:
		return L.labelMixed
	case DNSSourceUnknown:
		return L.labelLinkDNS
	case DNSSourceGlobal:
		return L.labelGlobalDNS
	}
	if len(s.DNS) == 0 {
		return L.labelDHCP
	}
	return L.labelManual
}

// dnsFixText is the system-dns fix hint in the platform's own tools.
func dnsFixText(L *texts, platform, listen string) string {
	switch platform {
	case Linux:
		return fmt.Sprintf(L.fixDNSLinux, listen, listen, listen)
	case Windows:
		return fmt.Sprintf(L.fixDNSWindows, listen, listen)
	}
	return fmt.Sprintf(L.fixDNS, listen)
}

// ---------------------------------------------------------------------------

// VPNConflict: no other VPN holds the default route, and no exit node.
type VPNConflict struct{}

func (VPNConflict) ID() string            { return "vpn-conflict" }
func (c VPNConflict) Title() string       { return c.title(&zhTexts) }
func (VPNConflict) title(L *texts) string { return L.titleVPNConflict }
func (VPNConflict) Platforms() []string   { return AllPlatforms }

func (VPNConflict) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	res := Result{Status: StatusOK, Summary: L.msgVPNOK}
	var problems []string
	fail := func(s Status, msg, cause string) {
		res.Status = Worse(res.Status, s)
		problems = append(problems, msg)
		res.Causes = append(res.Causes, cause)
	}

	if vpns, err := env.VPNs(ctx); err != nil {
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.msgReadFailed, "VPN list", err))
		res.Status = Worse(res.Status, StatusWarn)
	} else {
		for _, v := range vpns {
			line := fmt.Sprintf("%s [%s] %s", v.Name, v.BundleID, v.Status)
			isTS := strings.HasPrefix(v.BundleID, "io.tailscale.")
			switch {
			case v.Connected() && isTS:
				fail(StatusWarn, fmt.Sprintf(L.msgVPNTailscaleNE, v.Name), CauseTSNE)
				line += "  ← " + L.evTSNE
			case v.Connected():
				fail(StatusFail, fmt.Sprintf(L.msgVPNConnected, v.Name), CauseOtherVPN)
				line += "  ← " + L.evConnected
			}
			res.Evidence = append(res.Evidence, line)
		}
		if len(vpns) == 0 {
			res.Evidence = append(res.Evidence, L.evNoVPN)
		}
	}

	if ts, err := env.Tailscale(ctx); err == nil && ts.BackendState == "Running" {
		if ts.ExitNode != nil {
			msg := fmt.Sprintf(L.msgExitNode, ts.ExitNode.HostName)
			if env.Settings().AllowExitNode {
				fail(StatusWarn, msg, CauseExitNode)
			} else {
				fail(StatusFail, msg, CauseExitNode)
			}
		} else {
			res.Evidence = append(res.Evidence, L.evNoExitNode)
		}
	}

	// Who holds the default route? Expect mihomo's TUN when it is enabled.
	if rl, err := env.RouteGet(ctx, "1.1.1.1"); err == nil {
		tun, _, terr := tunConfig(ctx, env)
		line := fmt.Sprintf(L.evDefaultRoute, rl.Interface)
		if env.Platform() == Linux {
			if st, msg := linuxDefaultRoute(L, rl.Interface, tun, terr == nil); st != StatusOK {
				line += "  ← " + L.evNotMihomoTUN
				fail(st, msg, CauseOtherTUN)
			}
		} else if env.Platform() == Windows {
			if st, msg := windowsDefaultRoute(L, rl, tun, terr == nil); st != StatusOK {
				line += "  ← " + L.evNotMihomoTUN
				fail(st, msg, CauseOtherTUN)
			}
		} else if tun.Device == "utun" {
			// A bare "utun" (read from the config file when the controller
			// does not answer) lets macOS pick the number: the real name
			// is unknown, so there is nothing to compare.
		} else if terr == nil && tun.Enable && tun.Device != "" && rl.Interface != tun.Device && strings.HasPrefix(rl.Interface, "utun") {
			line += "  ← " + L.evNotMihomoTUN
			fail(StatusFail, fmt.Sprintf(L.msgOtherTUN, rl.Interface), CauseOtherTUN)
		} else if terr == nil && tun.Enable && tun.Device != "" && rl.Interface != tun.Device {
			line += "  ← " + L.evNotMihomoTUN
			fail(StatusWarn, fmt.Sprintf(L.msgTUNNotDefault, tun.Device, rl.Interface), CauseRoute)
		}
		res.Evidence = append(res.Evidence, line)
	}

	if len(problems) > 0 {
		res.Summary = strings.Join(problems, "；")
		res.Fix = &FixHint{Text: L.fixVPN}
		if len(res.Causes) == 1 && res.Causes[0] == CauseExitNode {
			res.Fix.AutoFix = FixExitNodeOff
		}
	}
	return res
}

// linuxTunnelPrefixes name the tunnel devices of common VPNs on Linux:
// OpenVPN/generic tun and tap, WireGuard (wg-quick, NetworkManager),
// PPP-based VPNs, strongSwan/libreswan, and vendor clients.
var linuxTunnelPrefixes = []string{"tun", "tap", "wg", "ppp", "ipsec", "vti", "xfrm", "nordlynx", "proton", "mullvad", "cscotun", "gpd", "utun"}

// linuxDefaultRoute judges the interface holding the default route on
// Linux. Another tunnel there (anything that is neither mihomo's TUN nor
// tailscaled's tailscale*) takes traffic away from mihomo: fail, even when
// mihomo's TUN is off. mihomo's TUN not holding it otherwise is a warning,
// as on macOS. haveTUN is false when the mihomo state could not be read.
func linuxDefaultRoute(L *texts, iface string, tun TUNConfig, haveTUN bool) (Status, string) {
	mihomoDev := tun.Device
	isMihomo := iface == mihomoDev
	if mihomoDev == "" {
		// mihomo's default TUN name on Linux: Meta, or Meta1, ... when taken.
		mihomoDev, isMihomo = "Meta", strings.HasPrefix(iface, "Meta")
	}
	if iface == "" || (haveTUN && tun.Enable && isMihomo) || strings.HasPrefix(iface, "tailscale") {
		return StatusOK, ""
	}
	for _, p := range linuxTunnelPrefixes {
		if strings.HasPrefix(iface, p) {
			return StatusFail, fmt.Sprintf(L.msgOtherTUN, iface)
		}
	}
	if haveTUN && tun.Enable {
		return StatusWarn, fmt.Sprintf(L.msgTUNNotDefault, mihomoDev, iface)
	}
	return StatusOK, ""
}

// RouteFlagTunnel marks, in RouteLookup.Flags, a route whose interface is a
// tunnel adapter (Windows: if-type TUNNEL/PPP/PROP_VIRTUAL, or a known VPN
// driver such as Wintun, WireGuard or TAP-Windows).
const RouteFlagTunnel = "tunnel"

// windowsTailscaleAdapter is the name of the official client's wintun
// adapter.
const windowsTailscaleAdapter = "Tailscale"

// windowsDefaultRoute judges the adapter holding the default route on
// Windows, like linuxDefaultRoute, but tunnel-ness comes from the Env
// (RouteFlagTunnel) instead of the device name: Windows adapters carry
// friendly names the user can change. mihomo's TUN is tun.device, else
// "Meta" (Meta1, ... when taken).
func windowsDefaultRoute(L *texts, rl RouteLookup, tun TUNConfig, haveTUN bool) (Status, string) {
	iface := rl.Interface
	mihomoDev := tun.Device
	isMihomo := iface == mihomoDev
	if mihomoDev == "" {
		mihomoDev, isMihomo = "Meta", strings.HasPrefix(iface, "Meta")
	}
	if iface == "" || (haveTUN && tun.Enable && isMihomo) || iface == windowsTailscaleAdapter {
		return StatusOK, ""
	}
	tunnel := false
	for _, f := range rl.Flags {
		if f == RouteFlagTunnel {
			tunnel = true
		}
	}
	// mihomo's TUN while mihomo is not readable is not "another VPN".
	if tunnel && !isMihomo {
		return StatusFail, fmt.Sprintf(L.msgOtherTUN, iface)
	}
	if haveTUN && tun.Enable && !isMihomo {
		return StatusWarn, fmt.Sprintf(L.msgTUNNotDefault, mihomoDev, iface)
	}
	return StatusOK, ""
}

// ---------------------------------------------------------------------------

// LeakGuard: sensitive proxy groups never end at DIRECT.
type LeakGuard struct{}

func (LeakGuard) ID() string            { return "leak-guard" }
func (c LeakGuard) Title() string       { return c.title(&zhTexts) }
func (LeakGuard) title(L *texts) string { return L.titleLeakGuard }
func (LeakGuard) Platforms() []string   { return AllPlatforms }

func (LeakGuard) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	rt, err := env.MihomoRuntime(ctx)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	groups := env.Settings().SensitiveGroups
	if len(groups) == 0 {
		return skipText(L.msgNoSensitive)
	}
	res := Result{Status: StatusOK, Summary: fmt.Sprintf(L.msgLeakOK, strings.Join(groups, ", "))}
	var fails, warns []string
	if rt.Configs.Mode == "global" || rt.Configs.Mode == "direct" {
		fails = append(fails, fmt.Sprintf(L.msgLeakMode, rt.Configs.Mode))
	}
	for _, g := range groups {
		p, ok := rt.Proxies[g]
		if !ok || !p.IsGroup() {
			warns = append(warns, fmt.Sprintf(L.msgLeakNoGroup, g))
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.msgLeakNoGroup, g))
			continue
		}
		// Follow the current selection down to a real proxy.
		chain := followNow(rt.Proxies, g)
		final := chain[len(chain)-1]
		cur := rt.Proxies[final]
		line := fmt.Sprintf(L.evLeakChain, strings.Join(chain, " → "))
		if final == "DIRECT" || cur.Type == "Direct" {
			line += "  ← " + L.evLeakDirect
			fails = append(fails, fmt.Sprintf(L.msgLeakSelected, g))
		}
		res.Evidence = append(res.Evidence, line)

		// Can the group reach DIRECT, automatically or by selection?
		auto, manual := directPaths(rt.Proxies, g, map[string]bool{})
		for _, a := range auto {
			fails = append(fails, fmt.Sprintf(L.msgLeakAuto, g, a))
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evLeakAuto, a))
		}
		for _, m := range manual {
			warns = append(warns, fmt.Sprintf(L.msgLeakManual, g, m))
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evLeakManual, m))
		}
	}
	switch {
	case len(fails) > 0:
		res.Status = StatusFail
		res.Summary = strings.Join(append(fails, warns...), "；")
		res.Fix = &FixHint{Text: L.fixLeak, AutoFix: FixLeakGuard}
	case len(warns) > 0:
		res.Status = StatusWarn
		res.Summary = strings.Join(warns, "；")
		res.Fix = &FixHint{Text: L.fixLeak, AutoFix: FixLeakGuard}
	}
	return res
}

// directPaths lists the paths from group g to DIRECT. A path is "auto" when
// every group on it picks members by itself (url-test, fallback,
// load-balance), so mihomo may switch to DIRECT without the user; otherwise
// it is "manual" (a selector the user could set to DIRECT).
func directPaths(px map[string]MihomoProxy, g string, seen map[string]bool) (auto, manual []string) {
	if seen[g] {
		return nil, nil
	}
	seen[g] = true
	p := px[g]
	autoGroup := p.Type != "Selector"
	for _, m := range p.All {
		mp, known := px[m]
		isDirect := m == "DIRECT" || (known && mp.Type == "Direct")
		switch {
		case isDirect && autoGroup:
			auto = append(auto, g+" → "+m)
		case isDirect:
			manual = append(manual, g+" → "+m)
		case known && mp.IsGroup():
			a, mn := directPaths(px, m, seen)
			for _, x := range a {
				if autoGroup {
					auto = append(auto, g+" → "+x)
				} else {
					manual = append(manual, g+" → "+x)
				}
			}
			for _, x := range mn {
				manual = append(manual, g+" → "+x)
			}
		}
	}
	return auto, manual
}

// ---------------------------------------------------------------------------

// Controller: the external controller listens on localhost and has a secret.
type Controller struct{}

func (Controller) ID() string            { return "controller-security" }
func (c Controller) Title() string       { return c.title(&zhTexts) }
func (Controller) title(L *texts) string { return L.titleController }
func (Controller) Platforms() []string   { return AllPlatforms }

func (Controller) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skipErr(L.msgMihomoUnavailable, err)
	}
	if cfg.ExternalController == "" {
		return Result{Status: StatusOK, Summary: L.msgCtlDisabled}
	}
	res := Result{Status: StatusOK, Summary: L.msgCtlOK}
	host := hostOf(cfg.ExternalController)
	res.Evidence = append(res.Evidence,
		fmt.Sprintf("external-controller: %s", cfg.ExternalController),
		fmt.Sprintf("secret: %s", map[bool]string{true: L.evSecretSet, false: L.evSecretEmpty}[cfg.SecretSet]))
	var problems []string
	if !isLoopbackHost(host) {
		res.Status = StatusFail
		problems = append(problems, fmt.Sprintf(L.msgCtlExposed, host))
	}
	if !cfg.SecretSet {
		res.Status = Worse(res.Status, StatusWarn)
		problems = append(problems, L.msgCtlNoSecret)
	}
	if rt, err := env.MihomoRuntime(ctx); err == nil && !rt.AuthUsed {
		res.Evidence = append(res.Evidence, L.evCtlNoAuth)
	}
	if len(problems) > 0 {
		res.Summary = strings.Join(problems, "；")
		res.Fix = &FixHint{Text: L.fixCtl, AutoFix: FixController}
	}
	return res
}
