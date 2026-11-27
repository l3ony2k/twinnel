package health

import (
	"bufio"
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// ResolverBinding: when mihomo binds its own sockets to the physical
// interface (tun.auto-detect-interface or interface-name), a query to
// 100.100.100.100 leaves through that interface and never reaches
// tailscaled, unless the resolver is pinned with "#<tailscale iface>" or the
// peer names are answered from hosts.
type ResolverBinding struct{}

func (ResolverBinding) ID() string            { return "tailnet-resolver-binding" }
func (c ResolverBinding) Title() string       { return c.title(&zhTexts) }
func (ResolverBinding) title(L *texts) string { return L.titleResolverBind }
func (ResolverBinding) Platforms() []string   { return AllPlatforms }

func (ResolverBinding) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skipMihomo(ctx, err)
	}
	if !cfg.DNS.Enable {
		return skipText(L.msgMihomoDNSOff)
	}
	ts, r := runningTailscale(ctx, env)
	if r != nil {
		return *r
	}
	var binds []string
	if cfg.TUN.Enable && cfg.TUN.AutoDetectInterface {
		binds = append(binds, "tun.auto-detect-interface")
	}
	if cfg.InterfaceName != "" {
		binds = append(binds, "interface-name: "+cfg.InterfaceName)
	}
	if len(binds) == 0 {
		return Result{Status: StatusOK, Summary: L.msgBindNone}
	}
	res := Result{Status: StatusOK, Evidence: []string{fmt.Sprintf(L.evBindIface, strings.Join(binds, ", "))}}

	suffix := strings.Trim(strings.ToLower(ts.MagicDNSSuffix), ".")
	var keys []string
	for k := range cfg.DNS.NameserverPolicy {
		if tailnetPolicyKey(k, suffix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	current := ""
	if rl, err := env.RouteGet(ctx, QuadAddr.String()); err == nil {
		current = rl.Interface
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evBindCurrent, current))
	}
	var unbound, stale []string
	var staleIface string
	quad := 0
	for _, k := range keys {
		for _, ns := range cfg.DNS.NameserverPolicy[k] {
			host, iface := splitResolver(ns)
			if host != QuadAddr.String() && host != QuadAddrV6.String() {
				continue
			}
			quad++
			switch {
			case iface == "":
				unbound = append(unbound, k)
			case current != "" && iface != current:
				stale, staleIface = append(stale, k), iface
			}
		}
	}
	// Keep the evidence short: reverse zones come in dozens.
	shown := 0
	for _, k := range keys {
		if strings.HasSuffix(k, ".arpa") && shown >= 3 {
			continue
		}
		if strings.HasSuffix(k, ".arpa") {
			shown++
		}
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evBindResolver, k, strings.Join(cfg.DNS.NameserverPolicy[k], ", ")))
	}
	if quad == 0 {
		res.Status, res.Summary = StatusSkip, L.msgBindNoQuad
		return res
	}
	hosts := 0
	peers := map[string]bool{}
	for _, p := range ts.Peers {
		peers[p.ShortName()] = true
	}
	peers[strings.ToLower(strings.SplitN(ts.Self.DNSName, ".", 2)[0])] = true
	for _, h := range cfg.HostNames {
		h = strings.ToLower(h)
		if peers[h] || (suffix != "" && strings.HasSuffix(h, "."+suffix)) {
			hosts++
		}
	}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evBindHosts, hosts))
	fix := &FixHint{Text: L.fixBind, AutoFix: FixCoexist}
	switch {
	case len(stale) > 0:
		res.Status, res.Summary, res.Fix = StatusWarn, fmt.Sprintf(L.msgBindStale, staleIface, current), fix
		res.because(ReasonBindStale)
	case len(unbound) > 0 && hosts == 0:
		res.Status, res.Summary, res.Fix = StatusWarn, L.msgBindMissing, fix
		res.because(ReasonBindMissing)
	case len(unbound) > 0:
		res.Summary = L.msgBindHostsOnly
	default:
		res.Summary = fmt.Sprintf(L.msgBindOK, current)
	}
	return res
}

// tailnetPolicyKey reports whether a nameserver-policy key covers tailnet
// names: the MagicDNS suffix, ts.net, single-label names ("*"), and the
// tailnet reverse zones.
func tailnetPolicyKey(k, suffix string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(k, "+."), "*."))
	switch {
	case k == "*":
		return true
	case d == "ts.net" || strings.HasSuffix(d, ".ts.net") || d == "beta.tailscale.net":
		return true
	case suffix != "" && (d == suffix || strings.HasSuffix(d, "."+suffix)):
		return true
	case strings.HasSuffix(d, ".100.in-addr.arpa"):
		return true
	case strings.HasSuffix(d, ".ip6.arpa") && strings.Contains(d, "a.7.d.f.ip6.arpa"):
		return true
	}
	return false
}

// splitResolver parses "100.100.100.100#utun9", "udp://100.100.100.100:53"
// or "tls://x#iface" into host and the "#" interface.
func splitResolver(ns string) (host, iface string) {
	ns = strings.TrimSpace(ns)
	if i := strings.IndexByte(ns, '#'); i >= 0 {
		ns, iface = ns[:i], ns[i+1:]
		// "#iface&h3=..." style parameters: keep the interface part only.
		if j := strings.IndexAny(iface, "&"); j >= 0 {
			iface = iface[:j]
		}
		if strings.Contains(iface, "=") || iface == "DIRECT" || strings.Contains(iface, " ") {
			iface = "" // a proxy name or params, not an interface
		}
	}
	if i := strings.Index(ns, "://"); i >= 0 {
		ns = ns[i+3:]
	}
	if ap, err := netip.ParseAddrPort(ns); err == nil {
		return ap.Addr().String(), iface
	}
	ns = strings.TrimSuffix(strings.TrimPrefix(ns, "["), "]")
	if a, err := netip.ParseAddr(ns); err == nil {
		return a.String(), iface
	}
	return ns, iface
}

// ---------------------------------------------------------------------------

// FakeIPMode: "*" in fake-ip-filter means "single-label names get real
// answers" only in blacklist mode.
type FakeIPMode struct{}

func (FakeIPMode) ID() string            { return "fake-ip-filter-mode" }
func (c FakeIPMode) Title() string       { return c.title(&zhTexts) }
func (FakeIPMode) title(L *texts) string { return L.titleFakeIPMode }
func (FakeIPMode) Platforms() []string   { return AllPlatforms }

func (FakeIPMode) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	cfg, err := env.MihomoConfig(ctx)
	if err != nil {
		return skipMihomo(ctx, err)
	}
	if !cfg.DNS.Enable || !strings.EqualFold(cfg.DNS.EnhancedMode, "fake-ip") {
		return skipText(L.msgFakeIPNotUsed)
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.DNS.FakeIPFilterMode))
	label := mode
	if mode == "" {
		mode, label = "blacklist", L.labelDefaultBlack
	}
	res := Result{Status: StatusOK, Evidence: []string{fmt.Sprintf(L.evFakeIPMode, label)}}
	has := false
	for _, f := range cfg.DNS.FakeIPFilter {
		if strings.TrimSpace(f) == "*" {
			has = true
		}
	}
	switch {
	case !has:
		res.Summary = L.msgWildcardNone
	case mode == "blacklist":
		res.Summary = L.msgWildcardOK
	default:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf(L.msgWildcardBad, mode)
		res.Fix = &FixHint{Text: L.fixWildcard}
		res.because(ReasonFakeIPWildcard)
	}
	return res
}

// ---------------------------------------------------------------------------

// LeakEgress (opt-in): the public IP seen through mihomo for the sensitive
// groups' current node differs from the direct one. It makes two HTTPS GETs
// to a neutral IP-echo endpoint and nothing else.
type LeakEgress struct{}

func (LeakEgress) ID() string            { return "leak-guard-egress" }
func (c LeakEgress) Title() string       { return c.title(&zhTexts) }
func (LeakEgress) title(L *texts) string { return L.titleEgress }
func (LeakEgress) Platforms() []string   { return AllPlatforms }

func (LeakEgress) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	s := env.Settings()
	if !egressSupported(env) {
		return unsupported(L, env.Platform())
	}
	if !s.Egress {
		r := skipText(L.msgEgressOff)
		r.Fix = &FixHint{Text: L.fixEgressProbe, AutoFix: FixEgressProbe}
		return r
	}
	ee, ok := env.(EgressEnv)
	if !ok {
		return unsupported(L, env.Platform())
	}
	url := s.EgressURL
	if url == "" {
		url = DefaultEgressURL
	}
	res := Result{Status: StatusOK, Evidence: []string{fmt.Sprintf(L.evEgressURL, url)}}
	// Direct baseline: the echo fetched on the physical interface, plus the
	// public endpoints tailscaled found by STUN (tailscaled goes DIRECT, and
	// this costs no traffic). Either is enough.
	direct := map[netip.Addr]bool{}
	ips := &EgressIPs{}
	res.Egress = ips
	d, derr := ee.DirectEgress(ctx, url)
	if derr == nil {
		direct[d] = true
		ips.Direct = d
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evEgressDirect, MaskIP(d)))
	} else {
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evEgressDirectErr, derr))
	}
	if ts, err := env.Tailscale(ctx); err == nil {
		for _, ep := range ts.Self.Addrs {
			ap, err := netip.ParseAddrPort(ep)
			if err != nil || !publicAddr(ap.Addr()) || direct[ap.Addr()] {
				continue
			}
			direct[ap.Addr()] = true
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evEgressSTUN, MaskIP(ap.Addr())))
		}
	}
	// No baseline means the network itself could not reach the echo:
	// nothing to compare and nothing Twinnel or the config can fix.
	if len(direct) == 0 {
		if r, ok := unsupportedErr(ctx, env, derr); ok {
			r.Evidence = res.Evidence
			return r
		}
		res.Status, res.Summary = StatusSkip, fmt.Sprintf(L.msgEgressErr, derr)
		return res
	}
	prox, chain, err := ee.ProxiedEgress(ctx, url)
	if r, ok := unsupportedErr(ctx, env, err); ok {
		r.Evidence = res.Evidence
		return r
	}
	if err != nil {
		res.Status, res.Summary = StatusWarn, fmt.Sprintf(L.msgEgressErr, err)
		res.Fix = &FixHint{Text: L.fixEgressProbe, AutoFix: FixEgressProbe}
		res.because(ReasonEgressFailed)
		return res
	}
	leaf := "?"
	if len(chain) > 0 {
		leaf = chain[0]
	}
	ips.Proxied, ips.Node = prox, leaf
	if len(chain) == 0 {
		ips.Node = ""
	}
	path := make([]string, len(chain))
	for i, c := range chain {
		path[len(chain)-1-i] = c
	}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evEgressProxied, MaskIP(prox), strings.Join(path, " → ")))
	if leaf == "DIRECT" {
		res.Status, res.Summary = StatusSkip, L.msgEgressDirectPath
		return res
	}
	if direct[prox] {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(L.msgEgressSame, leaf)
		res.Fix = &FixHint{Text: L.fixEgress, Param: egressGroup(ctx, env, leaf)}
		res.because(ReasonEgressSame)
		return res
	}
	rt, err := env.MihomoRuntime(ctx)
	if err != nil {
		r := skipMihomo(ctx, err)
		r.Egress = ips
		return r
	}
	var measured []string
	for _, g := range s.SensitiveGroups {
		ch := followNow(rt.Proxies, g)
		final := ch[len(ch)-1]
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evEgressGroup, g, final))
		if final == leaf {
			measured = append(measured, g)
		}
	}
	if len(measured) == 0 {
		res.Status, res.Summary = StatusSkip, fmt.Sprintf(L.msgEgressNotMeasured, leaf)
		return res
	}
	res.Summary = fmt.Sprintf(L.msgEgressOK, strings.Join(measured, ", "))
	return res
}

// publicAddr reports whether a is a global address that can be a public
// egress IP (not private, CGNAT/tailnet, mihomo's 198.18/15, link-local).
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// followNow follows the current selection of group g down to a real proxy
// and returns the chain, g first.
func followNow(px map[string]MihomoProxy, g string) []string {
	chain := []string{g}
	cur, ok := px[g]
	for i := 0; ok && i < 10 && cur.IsGroup() && cur.Now != ""; i++ {
		chain = append(chain, cur.Now)
		cur, ok = px[cur.Now]
	}
	return chain
}

// MaskIP hides the host part of a public address in reports: 203.0.113.7 →
// 203.0.x.x, 2001:db8:1:2::5 → 2001:db8::x.
func MaskIP(a netip.Addr) string {
	if !a.IsValid() {
		return "-"
	}
	if a.Is4() || a.Is4In6() {
		b := a.Unmap().As4()
		return fmt.Sprintf("%d.%d.x.x", b[0], b[1])
	}
	b := a.As16()
	return fmt.Sprintf("%x:%x::x", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3]))
}

// ParseEgressBody extracts the client IP from an IP-echo response: either a
// Cloudflare trace ("ip=203.0.113.7" line) or a body that is just the IP.
func ParseEgressBody(body string) (netip.Addr, error) {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(l, "ip="); ok {
			return netip.ParseAddr(v)
		}
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(body)); err == nil {
		return a, nil
	}
	return netip.Addr{}, fmt.Errorf("no IP in the echo response")
}

// egressGroup is the sensitive group whose current node is leaf (the one
// to pick another node for), else the first sensitive group.
func egressGroup(ctx context.Context, env Env, leaf string) string {
	groups := env.Settings().SensitiveGroups
	if rt, err := env.MihomoRuntime(ctx); err == nil {
		for _, g := range groups {
			if ch := followNow(rt.Proxies, g); ch[len(ch)-1] == leaf {
				return g
			}
		}
	}
	if len(groups) > 0 {
		return groups[0]
	}
	return ""
}
