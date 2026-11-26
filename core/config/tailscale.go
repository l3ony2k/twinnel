package config

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
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
	ProcessNames   []string // default ["tailscaled"]
	// Resolver is the nameserver used for tailnet names in
	// dns.nameserver-policy. Default "system" (macOS routes *.ts.net to
	// 100.100.100.100 through its resolver config); "100.100.100.100" also
	// works.
	Resolver string
}

// tailscaleDomains always go DIRECT and skip fake-ip. ts.net covers every
// MagicDNS suffix of the form <tailnet>.ts.net; tailscale.com covers the
// control plane and all DERP servers.
var tailscaleDomains = []string{"tailscale.com", "tailscale.io", "ts.net", "beta.tailscale.net"}

// tailnetResolverDomains are resolved by Tailscale, not by the upstream DNS.
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
	resolver := orDefault(info.Resolver, "system")

	suffix := strings.Trim(strings.ToLower(info.MagicDNSSuffix), ".")
	domains := append([]string(nil), tailscaleDomains...)
	nsDomains := append([]string(nil), tailnetResolverDomains...)
	if suffix != "" && !coveredBy(suffix, tailscaleDomains) {
		domains = append(domains, suffix)
		nsDomains = append(nsDomains, suffix)
	}

	peers, err := normalizePeers(info.PeerShortNames)
	if err != nil {
		return nil, err
	}

	filter := newSeq()
	for _, d := range domains {
		filter.Content = append(filter.Content, newStr("+."+d))
	}
	policy := newMap()
	for _, d := range nsDomains {
		policy.Content = append(policy.Content, newStr("<+."+d+">"), strSeq(resolver))
	}
	dns := newMap()
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
	root.Content = append(root.Content,
		newStr("tun"), tun,
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
		DNSName  string
		HostName string
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
	}
	sort.Strings(info.PeerShortNames)
	return info, nil
}
