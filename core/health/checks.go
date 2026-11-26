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
		SystemDNS{}, VPNConflict{}, DirectPeers{}, LeakGuard{}, LeakEgress{}, Controller{},
	} {
		r.Register(c)
	}
	return r
}

// tunConfig returns the TUN settings, preferring the running mihomo.
func tunConfig(ctx context.Context, env Env) (TUNConfig, string, error) {
	if rt, err := env.MihomoRuntime(ctx); err == nil {
		return rt.Configs.TUN, srcRuntime, nil
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return TUNConfig{}, "", err
	}
	return cfg.TUN, srcFile, nil
}

// mihomoRules returns the rules, preferring the running mihomo.
func mihomoRules(ctx context.Context, env Env) ([]MihomoRule, string, error) {
	if rt, err := env.MihomoRuntime(ctx); err == nil && len(rt.Rules) > 0 {
		return rt.Rules, srcRuntime, nil
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg.Rules, srcFile, nil
}

func runningTailscale(ctx context.Context, env Env) (*TailscaleStatus, *Result) {
	ts, err := env.Tailscale(ctx)
	if err != nil {
		r := skip(msgTSUnavailable, err)
		return nil, &r
	}
	if ts.BackendState != "Running" {
		r := skip(msgTSNotRunning, ts.BackendState)
		return nil, &r
	}
	return ts, nil
}

// ---------------------------------------------------------------------------

// TailnetRoute: tailnet traffic is routed to Tailscale's tun, not mihomo's.
type TailnetRoute struct{}

func (TailnetRoute) ID() string          { return "tailnet-route" }
func (TailnetRoute) Title() string       { return titleTailnetRoute }
func (TailnetRoute) Platforms() []string { return AllPlatforms }

func (TailnetRoute) Run(ctx context.Context, env Env) Result {
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	ifs, err := env.Interfaces(ctx)
	if err != nil {
		return Result{Status: StatusFail, Summary: fmt.Sprintf(msgReadFailed, "interfaces", err)}
	}
	tsIf := ""
	for _, a := range ts.TailscaleIPs {
		if tsIf = interfaceWith(ifs, a); tsIf != "" {
			break
		}
	}
	if tsIf == "" {
		if !ts.TUN {
			return skip(msgTSUserspace)
		}
		return Result{Status: StatusFail, Summary: msgTSNoIface}
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

	res := Result{Status: StatusOK, Summary: fmt.Sprintf(msgRouteOK, tsIf)}
	res.Evidence = append(res.Evidence, fmt.Sprintf(evTSIface, tsIf, mihomoIfLabel(mihomoIf)))
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
			line += "  ← " + evIntoMihomo
			bad = append(bad, d.String())
		default:
			line += "  ← " + evNotTS
			bad = append(bad, d.String())
		}
		res.Evidence = append(res.Evidence, line)
	}
	if routes, err := env.Routes(ctx); err == nil {
		for _, rt := range routes {
			if rt.Interface == tsIf && (rt.Destination == "100.64/10" || rt.Destination == "100.64.0.0/10" || strings.HasPrefix(rt.Destination, "fd7a:115c:a1e0::/48")) {
				res.Evidence = append(res.Evidence, fmt.Sprintf(evRouteEntry, rt.Destination, rt.Interface))
			}
		}
	}
	if len(bad) > 0 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(msgRouteBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: fixRouteBad, AutoFix: "tailscale-coexist-override"}
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

func (TUNExclude) ID() string          { return "tun-route-exclude" }
func (TUNExclude) Title() string       { return titleTUNExclude }
func (TUNExclude) Platforms() []string { return AllPlatforms }

func (TUNExclude) Run(ctx context.Context, env Env) Result {
	t, src, err := tunConfig(ctx, env)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
	}
	if !t.Enable {
		return skip(msgTUNOff)
	}
	res := Result{Status: StatusOK, Summary: msgExcludeOK}
	res.Evidence = append(res.Evidence, fmt.Sprintf(evExcludeList, src, strings.Join(t.RouteExcludeAddress, ", ")))
	var missing []string
	for _, want := range []netip.Prefix{TailnetV4, TailnetV6} {
		if by, ok := covers(t.RouteExcludeAddress, want); ok {
			res.Evidence = append(res.Evidence, fmt.Sprintf(evCoveredBy, want, by))
		} else {
			missing = append(missing, want.String())
		}
	}
	if len(missing) > 0 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(msgExcludeMissing, strings.Join(missing, ", "))
		res.Fix = &FixHint{Text: fixExclude, AutoFix: "tailscale-coexist-override"}
	}
	return res
}

// ---------------------------------------------------------------------------

// TailscaleRules: tailscaled, control plane, DERP and tailnet IPs hit DIRECT
// before any proxy rule.
type TailscaleRules struct{}

func (TailscaleRules) ID() string          { return "tailscale-direct-rules" }
func (TailscaleRules) Title() string       { return titleTailscaleRules }
func (TailscaleRules) Platforms() []string { return AllPlatforms }

func (TailscaleRules) Run(ctx context.Context, env Env) Result {
	rules, src, err := mihomoRules(ctx, env)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
	}
	if len(rules) == 0 {
		return skip(msgNoRules)
	}
	probes := []probe{
		{process: "tailscaled"},
		{domain: "controlplane.tailscale.com"},
		{domain: "login.tailscale.com"},
		{domain: "derp1.tailscale.com"},
		{ip: netip.MustParseAddr("100.100.100.100")},
		{ip: netip.MustParseAddr("fd7a:115c:a1e0::53")},
	}
	res := Result{Status: StatusOK, Summary: msgRulesOK}
	res.Evidence = append(res.Evidence, fmt.Sprintf(evRuleCount, src, len(rules)))
	var bad, risky []string
	for _, p := range probes {
		first, shadow := evalRules(rules, p)
		switch {
		case first == nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s", p, evNoMatch))
			bad = append(bad, p.String())
		case first.Target != "DIRECT":
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s  ← %s", p, ruleString(*first), evNotDirect))
			bad = append(bad, p.String())
		case len(shadow) > 0:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s  ← "+evShadowed, p, ruleString(*first), ruleString(shadow[0])))
			risky = append(risky, p.String())
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf("%s → %s", p, ruleString(*first)))
		}
	}
	switch {
	case len(bad) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(msgRulesBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: fixRules, AutoFix: "tailscale-coexist-override"}
	case len(risky) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(msgRulesRisky, strings.Join(risky, ", "))
		res.Fix = &FixHint{Text: fixRules, AutoFix: "tailscale-coexist-override"}
	}
	return res
}

// ---------------------------------------------------------------------------

// MagicDNS: MagicDNS names resolve to tailnet IPs, not fake-ips, through
// both the system resolver and mihomo's DNS.
type MagicDNS struct{}

func (MagicDNS) ID() string          { return "magicdns" }
func (MagicDNS) Title() string       { return titleMagicDNS }
func (MagicDNS) Platforms() []string { return AllPlatforms }

func (MagicDNS) Run(ctx context.Context, env Env) Result {
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	if !ts.MagicDNS {
		return skip(msgMagicDNSOff)
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

	res := Result{Status: StatusOK, Summary: msgMagicOK}
	var sysFail, mihomoFake, mihomoNoAnswer []string
	for i, nm := range names {
		sys, err := found[i].sys, found[i].sysErr
		switch {
		case err != nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf(evSys, nm.n, err.Error()))
			sysFail = append(sysFail, nm.n)
		case anyIn(sys.Addrs, fake):
			res.Evidence = append(res.Evidence, fmt.Sprintf(evSys, nm.n, addrs(sys)+"  ← fake-ip"))
			sysFail = append(sysFail, nm.n)
		case !anyTailnet(sys.Addrs):
			res.Evidence = append(res.Evidence, fmt.Sprintf(evSys, nm.n, addrs(sys)+"  ← "+evNotTailnet))
			sysFail = append(sysFail, nm.n)
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf(evSys, nm.n, addrs(sys)))
		}
		mh, err := found[i].mh, found[i].mhErr
		switch {
		case errors.Is(err, ErrDNSTimeout):
			res.Evidence = append(res.Evidence, fmt.Sprintf(evMihomo, nm.n, evDNSTimeout))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		case err != nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf(evMihomo, nm.n, err.Error()))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		case anyIn(mh.Addrs, fake):
			res.Evidence = append(res.Evidence, fmt.Sprintf(evMihomo, nm.n, addrs(mh)+"  ← fake-ip"))
			mihomoFake = append(mihomoFake, nm.n)
		case !anyTailnet(mh.Addrs):
			res.Evidence = append(res.Evidence, fmt.Sprintf(evMihomo, nm.n, addrs(mh)))
			mihomoNoAnswer = append(mihomoNoAnswer, nm.n)
		default:
			res.Evidence = append(res.Evidence, fmt.Sprintf(evMihomo, nm.n, addrs(mh)))
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
		res.Evidence = append(res.Evidence, fmt.Sprintf(evResolverCfg, ts.MagicDNSSuffix, yesNo(suffixOK), reverse))
		if !suffixOK {
			sysFail = append(sysFail, ts.MagicDNSSuffix+" resolver")
		}
	}
	if cfg != nil {
		for _, k := range []string{"+." + strings.Trim(ts.MagicDNSSuffix, "."), "+.ts.net", "*"} {
			if pol, ok := cfg.DNS.NameserverPolicy[k]; ok {
				res.Evidence = append(res.Evidence, fmt.Sprintf(evPolicy, k, strings.Join(pol, ", ")))
			}
		}
	}

	switch {
	case len(sysFail) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(msgMagicSysFail, strings.Join(uniq(sysFail), ", "))
		res.Fix = &FixHint{Text: fixMagicSys}
	case len(mihomoFake) > 0 || len(mihomoNoAnswer) > 0:
		res.Status = StatusWarn
		var parts []string
		if len(mihomoFake) > 0 {
			parts = append(parts, fmt.Sprintf(msgMagicMihomoFake, strings.Join(mihomoFake, ", ")))
		}
		if len(mihomoNoAnswer) > 0 {
			parts = append(parts, fmt.Sprintf(msgMagicMihomoNone, strings.Join(mihomoNoAnswer, ", ")))
		}
		res.Summary = strings.Join(parts, "；")
		fix := fixMagicMihomo
		if cfg != nil {
			pol := cfg.DNS.NameserverPolicy["+."+strings.Trim(ts.MagicDNSSuffix, ".")]
			if len(pol) == 0 {
				pol = cfg.DNS.NameserverPolicy["+.ts.net"]
			}
			if len(pol) == 1 && pol[0] == "system" {
				fix = fixMagicSystemLoop + fix
			}
		}
		res.Fix = &FixHint{Text: fix, AutoFix: "tailscale-coexist-override"}
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

func addrs(a DNSAnswer) string {
	if len(a.Addrs) == 0 {
		if a.Rcode == "" {
			return evEmpty
		}
		return a.Rcode + " " + evEmpty
	}
	var s []string
	for _, x := range a.Addrs {
		s = append(s, x.String())
	}
	return strings.Join(s, ", ")
}

func yesNo(b bool) string {
	if b {
		return labelYes
	}
	return labelNo
}

// ---------------------------------------------------------------------------

// PeerNames: tailnet short names written in rules match current peers.
type PeerNames struct{}

func (PeerNames) ID() string          { return "peer-short-names" }
func (PeerNames) Title() string       { return titlePeerNames }
func (PeerNames) Platforms() []string { return AllPlatforms }

func (PeerNames) Run(ctx context.Context, env Env) Result {
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	rules, src, err := mihomoRules(ctx, env)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
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
	res := Result{Status: StatusOK, Summary: fmt.Sprintf(msgNamesOK, len(peers))}
	res.Evidence = append(res.Evidence,
		fmt.Sprintf(evNamesPeers, strings.Join(sortedKeys(peers), ", ")),
		fmt.Sprintf(evNamesRules, src, strings.Join(sortedKeys(inRules), ", ")))
	if len(inRules) == 0 {
		res.Status = StatusWarn
		res.Summary = msgNamesNone
		res.Fix = &FixHint{Text: fixNames, AutoFix: "tailscale-coexist-override"}
		return res
	}
	if len(missing) > 0 || len(stale) > 0 {
		res.Status = StatusWarn
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, fmt.Sprintf(msgNamesMissing, strings.Join(missing, ", ")))
		}
		if len(stale) > 0 {
			parts = append(parts, fmt.Sprintf(msgNamesStale, strings.Join(stale, ", ")))
		}
		res.Summary = strings.Join(parts, "；")
		res.Fix = &FixHint{Text: fixNames, AutoFix: "tailscale-coexist-override"}
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

func (SystemDNS) ID() string          { return "system-dns" }
func (SystemDNS) Title() string       { return titleSystemDNS }
func (SystemDNS) Platforms() []string { return AllPlatforms }

func (SystemDNS) Run(ctx context.Context, env Env) Result {
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
	}
	if !cfg.DNS.Enable {
		return skip(msgMihomoDNSOff)
	}
	listen := hostOf(cfg.DNS.Listen)
	want := map[string]bool{listen: true}
	if listen == "0.0.0.0" || listen == "" || listen == "::" {
		want["127.0.0.1"] = true
	}
	tun, _, _ := tunConfig(ctx, env)
	hijack := hijacksAll53(tun)

	svcs, err := env.NetworkServices(ctx)
	if err != nil {
		return Result{Status: StatusFail, Summary: fmt.Sprintf(msgReadFailed, "network services", err)}
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

	res := Result{Status: StatusOK, Summary: fmt.Sprintf(msgDNSOK, cfg.DNS.Listen)}
	var bad, viaHijack []string
	nActive := 0
	for _, s := range svcs {
		if s.Device == "" {
			continue // VPN services are covered by vpn-conflict
		}
		if !s.Enabled || !active[s.Device] {
			res.Evidence = append(res.Evidence, fmt.Sprintf(evSvcInactive, s.Name, s.Device))
			continue
		}
		nActive++
		servers, how := s.DNS, labelManual
		if len(servers) == 0 {
			servers, how = defaultNS, labelDHCP
		}
		ok := len(servers) > 0
		for _, ns := range servers {
			if !want[ns] {
				ok = false
			}
		}
		line := fmt.Sprintf(evSvc, s.Name, s.Device, strings.Join(servers, ", "), how)
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
			res.Evidence = append(res.Evidence, line+"  ← "+evHijackCovers)
			viaHijack = append(viaHijack, s.Name)
		} else {
			if excluded {
				line += "  ← " + evExcludedDNS
			}
			res.Evidence = append(res.Evidence, line)
			bad = append(bad, s.Name)
		}
	}
	if len(defaultNS) > 0 {
		res.Evidence = append(res.Evidence, fmt.Sprintf(evDefaultResolver, strings.Join(defaultNS, ", ")))
	}
	switch {
	case len(bad) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(msgDNSBad, strings.Join(bad, ", "))
		res.Fix = &FixHint{Text: fmt.Sprintf(fixDNS, listen), AutoFix: "system-dns"}
	case len(viaHijack) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(msgDNSHijack, strings.Join(viaHijack, ", "))
		res.Fix = &FixHint{Text: fmt.Sprintf(fixDNS, listen), AutoFix: "system-dns"}
	case nActive == 0:
		res.Status = StatusWarn
		res.Summary = msgDNSNoActive
	}
	return res
}

// ---------------------------------------------------------------------------

// VPNConflict: no other VPN holds the default route, and no exit node.
type VPNConflict struct{}

func (VPNConflict) ID() string          { return "vpn-conflict" }
func (VPNConflict) Title() string       { return titleVPNConflict }
func (VPNConflict) Platforms() []string { return AllPlatforms }

func (VPNConflict) Run(ctx context.Context, env Env) Result {
	res := Result{Status: StatusOK, Summary: msgVPNOK}
	var problems []string
	fail := func(s Status, msg string) {
		res.Status = Worse(res.Status, s)
		problems = append(problems, msg)
	}

	if vpns, err := env.VPNs(ctx); err != nil {
		res.Evidence = append(res.Evidence, fmt.Sprintf(msgReadFailed, "VPN list", err))
		res.Status = Worse(res.Status, StatusWarn)
	} else {
		for _, v := range vpns {
			line := fmt.Sprintf("%s [%s] %s", v.Name, v.BundleID, v.Status)
			isTS := strings.HasPrefix(v.BundleID, "io.tailscale.")
			switch {
			case v.Connected() && isTS:
				fail(StatusWarn, fmt.Sprintf(msgVPNTailscaleNE, v.Name))
				line += "  ← " + evTSNE
			case v.Connected():
				fail(StatusFail, fmt.Sprintf(msgVPNConnected, v.Name))
				line += "  ← " + evConnected
			}
			res.Evidence = append(res.Evidence, line)
		}
		if len(vpns) == 0 {
			res.Evidence = append(res.Evidence, evNoVPN)
		}
	}

	if ts, err := env.Tailscale(ctx); err == nil && ts.BackendState == "Running" {
		if ts.ExitNode != nil {
			msg := fmt.Sprintf(msgExitNode, ts.ExitNode.HostName)
			if env.Settings().AllowExitNode {
				fail(StatusWarn, msg)
			} else {
				fail(StatusFail, msg)
			}
		} else {
			res.Evidence = append(res.Evidence, evNoExitNode)
		}
	}

	// Who holds the default route? Expect mihomo's TUN when it is enabled.
	if rl, err := env.RouteGet(ctx, "1.1.1.1"); err == nil {
		tun, _, terr := tunConfig(ctx, env)
		line := fmt.Sprintf(evDefaultRoute, rl.Interface)
		if terr == nil && tun.Enable && tun.Device != "" && rl.Interface != tun.Device && strings.HasPrefix(rl.Interface, "utun") {
			line += "  ← " + evNotMihomoTUN
			fail(StatusFail, fmt.Sprintf(msgOtherTUN, rl.Interface))
		} else if terr == nil && tun.Enable && tun.Device != "" && rl.Interface != tun.Device {
			line += "  ← " + evNotMihomoTUN
			fail(StatusWarn, fmt.Sprintf(msgTUNNotDefault, tun.Device, rl.Interface))
		}
		res.Evidence = append(res.Evidence, line)
	}

	if len(problems) > 0 {
		res.Summary = strings.Join(problems, "；")
		res.Fix = &FixHint{Text: fixVPN}
	}
	return res
}

// ---------------------------------------------------------------------------

// DirectPeers: preferred peers are connected directly, not through DERP.
type DirectPeers struct{}

func (DirectPeers) ID() string          { return "peer-direct" }
func (DirectPeers) Title() string       { return titleDirectPeers }
func (DirectPeers) Platforms() []string { return AllPlatforms }

func (DirectPeers) Run(ctx context.Context, env Env) Result {
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	pref := env.Settings().PreferredPeers
	explicit := len(pref) > 0
	want := func(p TailscalePeer) bool {
		if !explicit {
			return p.Online
		}
		for _, x := range pref {
			x = strings.ToLower(x)
			if x == strings.ToLower(p.HostName) || x == strings.ToLower(p.ShortName()) ||
				x == strings.ToLower(strings.TrimSuffix(p.DNSName, ".")) {
				return true
			}
			for _, a := range p.TailscaleIPs {
				if a.String() == x {
					return true
				}
			}
		}
		return false
	}

	res := Result{}
	var direct, relayed, idle, offline []string
	for _, p := range ts.Peers {
		if !want(p) {
			continue
		}
		name := p.ShortName()
		if name == "" {
			name = p.HostName
		}
		switch {
		case !p.Online:
			offline = append(offline, name)
			res.Evidence = append(res.Evidence, fmt.Sprintf(evPeerOffline, name))
		case p.CurAddr != "":
			direct = append(direct, name)
			res.Evidence = append(res.Evidence, fmt.Sprintf(evPeerDirect, name, p.CurAddr))
		case p.Active:
			relayed = append(relayed, name)
			via := "DERP " + p.Relay
			if p.PeerRelay != "" {
				via = "peer relay " + p.PeerRelay
			}
			res.Evidence = append(res.Evidence, fmt.Sprintf(evPeerRelayed, name, via))
		default:
			idle = append(idle, name)
			res.Evidence = append(res.Evidence, fmt.Sprintf(evPeerIdle, name, p.Relay))
		}
	}
	// tailscaled advertising mihomo's TUN address as one of its endpoints is
	// a sign that it binds through the proxy's interface.
	if tun, _, err := tunConfig(ctx, env); err == nil && tun.Enable {
		for _, ep := range ts.Self.Addrs {
			if ap, err := netip.ParseAddrPort(ep); err == nil && inAny(tun.Inet4Address, ap.Addr()) {
				res.Evidence = append(res.Evidence, fmt.Sprintf(evSelfEndpointTUN, ep))
			}
		}
	}

	switch {
	case len(relayed) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(msgPeersRelayed, strings.Join(relayed, ", "))
		res.Fix = &FixHint{Text: fixPeers, Command: "tailscale netcheck\ntailscale ping " + relayed[0]}
	case explicit && len(offline) > 0:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(msgPeersOffline, strings.Join(offline, ", "))
	case len(direct)+len(idle) == 0:
		res.Status = StatusSkip
		res.Summary = msgPeersNone
	default:
		res.Status = StatusOK
		res.Summary = fmt.Sprintf(msgPeersOK, len(direct), len(idle))
	}
	return res
}

// ---------------------------------------------------------------------------

// LeakGuard: sensitive proxy groups never end at DIRECT.
type LeakGuard struct{}

func (LeakGuard) ID() string          { return "leak-guard" }
func (LeakGuard) Title() string       { return titleLeakGuard }
func (LeakGuard) Platforms() []string { return AllPlatforms }

func (LeakGuard) Run(ctx context.Context, env Env) Result {
	rt, err := env.MihomoRuntime(ctx)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
	}
	groups := env.Settings().SensitiveGroups
	if len(groups) == 0 {
		return skip(msgNoSensitive)
	}
	res := Result{Status: StatusOK, Summary: fmt.Sprintf(msgLeakOK, strings.Join(groups, ", "))}
	var fails, warns []string
	if rt.Configs.Mode == "global" || rt.Configs.Mode == "direct" {
		fails = append(fails, fmt.Sprintf(msgLeakMode, rt.Configs.Mode))
	}
	for _, g := range groups {
		p, ok := rt.Proxies[g]
		if !ok || !p.IsGroup() {
			warns = append(warns, fmt.Sprintf(msgLeakNoGroup, g))
			res.Evidence = append(res.Evidence, fmt.Sprintf(msgLeakNoGroup, g))
			continue
		}
		// Follow the current selection down to a real proxy.
		chain := followNow(rt.Proxies, g)
		final := chain[len(chain)-1]
		cur := rt.Proxies[final]
		line := fmt.Sprintf(evLeakChain, strings.Join(chain, " → "))
		if final == "DIRECT" || cur.Type == "Direct" {
			line += "  ← " + evLeakDirect
			fails = append(fails, fmt.Sprintf(msgLeakSelected, g))
		}
		res.Evidence = append(res.Evidence, line)

		// Can the group reach DIRECT, automatically or by selection?
		auto, manual := directPaths(rt.Proxies, g, map[string]bool{})
		for _, a := range auto {
			fails = append(fails, fmt.Sprintf(msgLeakAuto, g, a))
			res.Evidence = append(res.Evidence, fmt.Sprintf(evLeakAuto, a))
		}
		for _, m := range manual {
			warns = append(warns, fmt.Sprintf(msgLeakManual, g, m))
			res.Evidence = append(res.Evidence, fmt.Sprintf(evLeakManual, m))
		}
	}
	switch {
	case len(fails) > 0:
		res.Status = StatusFail
		res.Summary = strings.Join(append(fails, warns...), "；")
		res.Fix = &FixHint{Text: fixLeak, AutoFix: "leak-guard-override"}
	case len(warns) > 0:
		res.Status = StatusWarn
		res.Summary = strings.Join(warns, "；")
		res.Fix = &FixHint{Text: fixLeak, AutoFix: "leak-guard-override"}
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

func (Controller) ID() string          { return "controller-security" }
func (Controller) Title() string       { return titleController }
func (Controller) Platforms() []string { return AllPlatforms }

func (Controller) Run(ctx context.Context, env Env) Result {
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skip(msgMihomoUnavailable, err)
	}
	if cfg.ExternalController == "" {
		return Result{Status: StatusOK, Summary: msgCtlDisabled}
	}
	res := Result{Status: StatusOK, Summary: msgCtlOK}
	host := hostOf(cfg.ExternalController)
	res.Evidence = append(res.Evidence,
		fmt.Sprintf("external-controller: %s", cfg.ExternalController),
		fmt.Sprintf("secret: %s", map[bool]string{true: evSecretSet, false: evSecretEmpty}[cfg.SecretSet]))
	var problems []string
	if !isLoopbackHost(host) {
		res.Status = StatusFail
		problems = append(problems, fmt.Sprintf(msgCtlExposed, host))
	}
	if !cfg.SecretSet {
		res.Status = Worse(res.Status, StatusWarn)
		problems = append(problems, msgCtlNoSecret)
	}
	if rt, err := env.MihomoRuntime(ctx); err == nil && !rt.AuthUsed {
		res.Evidence = append(res.Evidence, evCtlNoAuth)
	}
	if len(problems) > 0 {
		res.Summary = strings.Join(problems, "；")
		res.Fix = &FixHint{Text: fixCtl, AutoFix: "controller-secret"}
	}
	return res
}
