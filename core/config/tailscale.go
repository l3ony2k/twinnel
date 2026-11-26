package config

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tailscale defaults (the CGNAT range and the Tailscale ULA prefix).
const (
	DefaultTailnetIPv4 = "100.64.0.0/10"
	DefaultTailnetIPv6 = "fd7a:115c:a1e0::/48"
)

// TailnetInfo describes the tailnet the coexistence layer is generated for.
// Zero values get defaults.
type TailnetInfo struct {
	IPv4CIDR       string   // default DefaultTailnetIPv4
	IPv6CIDR       string   // default DefaultTailnetIPv6
	MagicDNSSuffix string   // e.g. "tailnet-1234.ts.net"; optional
	PeerShortNames []string // MagicDNS short names, self included
	// PeerAddrs maps short names to their tailnet IPs. When set, dns hosts
	// entries are generated for "<short>" and "<short>.<MagicDNSSuffix>", so
	// mihomo answers peer names without any upstream query.
	PeerAddrs    map[string][]string
	ProcessNames []string // default ["tailscaled"]
	// Resolver is the nameserver used for tailnet names (MagicDNS suffix,
	// single-label names, tailnet reverse zones) in dns.nameserver-policy.
	// Default DefaultTailnetResolver. Do not use "system" on a machine whose
	// system DNS is mihomo itself: the query loops back and fails.
	Resolver string
	// Interface is Tailscale's network interface (e.g. "utun9"). When set and
	// Resolver is the default, the resolver becomes
	// "100.100.100.100#<Interface>": with tun.auto-detect-interface (or
	// interface-name) mihomo binds its own sockets to the physical interface,
	// and a query to 100.100.100.100 bound there never reaches tailscaled.
	// The name changes when tailscaled restarts, so regenerate then.
	Interface string
}

// DefaultTailnetResolver is Tailscale's MagicDNS resolver ("quad-100").
const DefaultTailnetResolver = "100.100.100.100"

// tailscaleDomains always go DIRECT and skip fake-ip. ts.net covers every
// MagicDNS suffix of the form <tailnet>.ts.net; tailscale.com covers the
// control plane and all DERP servers.
var tailscaleDomains = []string{"tailscale.com", "tailscale.io", "ts.net", "beta.tailscale.net"}

// tailnetResolverDomains are resolved by Tailscale when the MagicDNS suffix
// is unknown. With a known suffix only that suffix goes to Tailscale: quad-100
// does not forward other names, so public *.ts.net names (Funnel) of other
// tailnets keep using the normal upstream.
var tailnetResolverDomains = []string{"ts.net", "beta.tailscale.net"}

var labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// TailscaleCoexist generates the managed override that lets mihomo's TUN and
// tailscaled run side by side. The output is deterministic.
func TailscaleCoexist(info TailnetInfo) ([]byte, error) {
	v4 := orDefault(info.IPv4CIDR, DefaultTailnetIPv4)
	v6 := orDefault(info.IPv6CIDR, DefaultTailnetIPv6)
	for _, c := range []string{v4, v6} {
		if _, err := netip.ParsePrefix(c); err != nil {
			return nil, fmt.Errorf("tailnet CIDR %q: %w", c, err)
		}
	}
	procs := info.ProcessNames
	if len(procs) == 0 {
		procs = []string{"tailscaled"}
	}
	resolver := orDefault(info.Resolver, DefaultTailnetResolver)
	if iface := strings.TrimSpace(info.Interface); iface != "" && resolver == DefaultTailnetResolver {
		resolver += "#" + iface
	}

	suffix := strings.Trim(strings.ToLower(info.MagicDNSSuffix), ".")
	domains := append([]string(nil), tailscaleDomains...)
	nsDomains := tailnetResolverDomains
	if suffix != "" {
		nsDomains = []string{suffix}
		if !coveredBy(suffix, tailscaleDomains) {
			domains = append(domains, suffix)
		}
	}

	peers, err := normalizePeers(info.PeerShortNames)
	if err != nil {
		return nil, err
	}
	hosts, err := peerHosts(info.PeerAddrs, suffix)
	if err != nil {
		return nil, err
	}
	reverse, err := reverseZones(v4, v6)
	if err != nil {
		return nil, err
	}

	filter := newSeq()
	for _, d := range domains {
		filter.Content = append(filter.Content, newStr("+."+d))
	}
	// "*" matches any single-label name: MagicDNS short names ("nas") get a
	// real answer from Tailscale instead of a fake-ip, without a per-device
	// list.
	filter.Content = append(filter.Content, newStr("*"))
	ns := func() *yaml.Node { n := strSeq(resolver); n.Style = yaml.FlowStyle; return n }
	policy := newMap()
	for _, d := range nsDomains {
		policy.Content = append(policy.Content, newStr("<+."+d+">"), ns())
	}
	policy.Content = append(policy.Content, newStr("*"), ns())
	for _, z := range reverse {
		policy.Content = append(policy.Content, newStr("<+."+z+">"), ns())
	}
	dns := newMap()
	if hosts != nil {
		dns.Content = append(dns.Content, newStr("use-hosts"), newBool(true))
	}
	dns.Content = append(dns.Content,
		newStr("fake-ip-filter+"), filter,
		newStr("nameserver-policy"), policy)

	tun := newMap()
	tun.Content = append(tun.Content, newStr("route-exclude-address+"), strSeq(v4, v6))

	rules := newSeq()
	add := func(s string) { rules.Content = append(rules.Content, newStr(s)) }
	for _, p := range procs {
		add("PROCESS-NAME," + p + ",DIRECT")
	}
	for _, d := range domains {
		add("DOMAIN-SUFFIX," + d + ",DIRECT")
	}
	for _, p := range peers {
		add("DOMAIN," + p + ",DIRECT")
	}
	add("IP-CIDR," + v4 + ",DIRECT,no-resolve")
	add("IP-CIDR6," + v6 + ",DIRECT,no-resolve")

	root := newMap()
	root.HeadComment = "Managed by duet: Tailscale coexistence. Regenerated when the tailnet changes; do not edit."
	root.Content = append(root.Content, newStr("tun"), tun)
	if hosts != nil {
		root.Content = append(root.Content, newStr("hosts"), hosts)
	}
	root.Content = append(root.Content,
		newStr("dns"), dns,
		newStr("+rules"), rules)
	return Encode(root)
}

// TailscaleCoexistLayer wraps TailscaleCoexist in a managed layer.
func TailscaleCoexistLayer(info TailnetInfo) (Layer, error) {
	b, err := TailscaleCoexist(info)
	if err != nil {
		return Layer{}, err
	}
	return Layer{
		ID: "tailscale-coexist", Name: "Tailscale coexist", Kind: KindManaged,
		Stage: StageCoexist, Enabled: true, Override: b,
	}, nil
}

func normalizePeers(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if !labelRE.MatchString(p) {
			return nil, fmt.Errorf("peer short name %q is not a DNS label", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// peerHosts builds the hosts map: "<short>" and "<short>.<suffix>" for each
// peer, sorted by short name. Nil when there are no addresses.
func peerHosts(addrs map[string][]string, suffix string) (*yaml.Node, error) {
	byName := map[string][]string{}
	for name, ips := range addrs {
		n, err := normalizePeers([]string{name})
		if err != nil {
			return nil, err
		}
		if len(n) == 0 {
			continue
		}
		for _, ip := range ips {
			a, err := netip.ParseAddr(strings.TrimSpace(ip))
			if err != nil {
				return nil, fmt.Errorf("peer %q address %q: %w", name, ip, err)
			}
			byName[n[0]] = append(byName[n[0]], a.String())
		}
	}
	if len(byName) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(byName))
	for n, ips := range byName {
		names = append(names, n)
		sort.Slice(ips, func(i, j int) bool { // IPv4 first, then by address
			a, b := netip.MustParseAddr(ips[i]), netip.MustParseAddr(ips[j])
			return a.Less(b)
		})
		byName[n] = dedupeSorted(ips)
	}
	sort.Strings(names)
	m := newMap()
	for _, n := range names {
		m.Content = append(m.Content, newStr(n), strSeq(byName[n]...))
		if suffix != "" {
			m.Content = append(m.Content, newStr(n+"."+suffix), strSeq(byName[n]...))
		}
	}
	return m, nil
}

func dedupeSorted(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// maxReverseZones caps the number of reverse zones generated per prefix.
const maxReverseZones = 256

// reverseZones returns the in-addr.arpa / ip6.arpa zones that exactly cover
// the two prefixes: 100.64.0.0/10 gives 64.100.in-addr.arpa ..
// 127.100.in-addr.arpa, fd7a:115c:a1e0::/48 gives one ip6.arpa zone.
func reverseZones(v4, v6 string) ([]string, error) {
	var out []string
	for _, c := range []string{v4, v6} {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, err
		}
		p = p.Masked()
		step := 8 // bits per label
		if p.Addr().Is6() {
			step = 4
		}
		bits := p.Bits()
		zoneBits := (bits + step - 1) / step * step
		if zoneBits == 0 {
			return nil, fmt.Errorf("tailnet CIDR %q: too wide for reverse zones", c)
		}
		n := 1 << (zoneBits - bits)
		if n > maxReverseZones {
			return nil, fmt.Errorf("tailnet CIDR %q: too many reverse zones (%d)", c, n)
		}
		base := p.Addr().AsSlice()
		for i := 0; i < n; i++ {
			out = append(out, reverseName(addBits(base, zoneBits, i), zoneBits, step))
		}
	}
	return out, nil
}

// addBits adds i to the address treated as a zoneBits-wide prefix number.
func addBits(addr []byte, zoneBits, i int) []byte {
	a := append([]byte(nil), addr...)
	carry := uint(i) << ((8 - zoneBits%8) % 8)
	for k := (zoneBits+7)/8 - 1; k >= 0 && carry > 0; k-- {
		sum := uint(a[k]) + carry&0xff
		a[k] = byte(sum)
		carry = carry>>8 + sum>>8
	}
	return a
}

func reverseName(addr []byte, zoneBits, step int) string {
	var labels []string
	if step == 8 {
		for k := 0; k < zoneBits/8; k++ {
			labels = append(labels, fmt.Sprint(addr[k]))
		}
	} else {
		for k := 0; k < zoneBits/4; k++ {
			b := addr[k/2]
			if k%2 == 0 {
				b >>= 4
			}
			labels = append(labels, fmt.Sprintf("%x", b&0xf))
		}
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	if step == 8 {
		return strings.Join(labels, ".") + ".in-addr.arpa"
	}
	return strings.Join(labels, ".") + ".ip6.arpa"
}

func coveredBy(d string, parents []string) bool {
	for _, p := range parents {
		if d == p || strings.HasSuffix(d, "."+p) {
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// TailnetInfoFromStatus builds TailnetInfo from `tailscale status --json`.
// Short names are the first label of each node's MagicDNS name (self
// included); nodes whose name is outside this tailnet's suffix (shared-in
// nodes) are skipped because their short name does not resolve here.
func TailnetInfoFromStatus(statusJSON []byte) (TailnetInfo, error) {
	type node struct {
		DNSName      string
		HostName     string
		TailscaleIPs []string
	}
	var st struct {
		MagicDNSSuffix string
		CurrentTailnet *struct {
			MagicDNSSuffix  string
			MagicDNSEnabled bool
		}
		Self *node
		Peer map[string]*node
	}
	if err := json.Unmarshal(statusJSON, &st); err != nil {
		return TailnetInfo{}, fmt.Errorf("tailscale status: %w", err)
	}
	suffix := st.MagicDNSSuffix
	if suffix == "" && st.CurrentTailnet != nil {
		suffix = st.CurrentTailnet.MagicDNSSuffix
	}
	suffix = strings.Trim(strings.ToLower(suffix), ".")
	info := TailnetInfo{MagicDNSSuffix: suffix}
	nodes := []*node{st.Self}
	for _, p := range st.Peer {
		nodes = append(nodes, p)
	}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		name := strings.Trim(strings.ToLower(n.DNSName), ".")
		short, rest, ok := strings.Cut(name, ".")
		if !ok || short == "" || (suffix != "" && rest != suffix) {
			continue
		}
		info.PeerShortNames = append(info.PeerShortNames, short)
		if len(n.TailscaleIPs) > 0 {
			if info.PeerAddrs == nil {
				info.PeerAddrs = map[string][]string{}
			}
			info.PeerAddrs[short] = append(info.PeerAddrs[short], n.TailscaleIPs...)
		}
	}
	sort.Strings(info.PeerShortNames)
	return info, nil
}

func newBool(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(b)}
}
