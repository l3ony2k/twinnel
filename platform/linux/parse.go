package linux

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
)

// ---- iproute2 (ip -j) -----------------------------------------------------

type ipRouteJSON struct {
	Type     string   `json:"type"`
	Dst      string   `json:"dst"`
	Gateway  string   `json:"gateway"`
	Dev      string   `json:"dev"`
	Table    string   `json:"table"`
	Protocol string   `json:"protocol"`
	Metric   *int     `json:"metric"`
	Prefsrc  string   `json:"prefsrc"`
	Flags    []string `json:"flags"`
}

// ParseIPRouteGet parses `ip -j route get <dest>`.
func ParseIPRouteGet(data []byte) (health.RouteLookup, error) {
	var rs []ipRouteJSON
	if err := json.Unmarshal(data, &rs); err != nil {
		return health.RouteLookup{}, fmt.Errorf("ip route get: %w", err)
	}
	if len(rs) == 0 {
		return health.RouteLookup{}, fmt.Errorf("ip route get: empty answer")
	}
	r := rs[0]
	l := health.RouteLookup{Destination: r.Dst, Gateway: r.Gateway, Interface: r.Dev, Flags: r.Flags}
	if r.Table != "" {
		l.Flags = append(l.Flags, "table "+r.Table)
	}
	return l, nil
}

// ParseIPRoutes parses `ip -j -4|-6 route show table all`. Only unicast
// routes are kept (local, broadcast, multicast and the like are dropped).
// Flags carries "table <t>" for routes outside the main table, e.g. the
// tailnet routes tailscaled puts in table 52.
func ParseIPRoutes(data []byte, family int) ([]health.Route, error) {
	var rs []ipRouteJSON
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("ip route show: %w", err)
	}
	var out []health.Route
	for _, r := range rs {
		if r.Type != "" && r.Type != "unicast" {
			continue
		}
		var flags []string
		if r.Table != "" && r.Table != "main" {
			flags = append(flags, "table "+r.Table)
		}
		if r.Metric != nil {
			flags = append(flags, "metric "+strconv.Itoa(*r.Metric))
		}
		flags = append(flags, r.Flags...)
		out = append(out, health.Route{Family: family, Destination: r.Dst, Gateway: r.Gateway,
			Interface: r.Dev, Flags: strings.Join(flags, ",")})
	}
	return out, nil
}

// Link is an interface from `ip -j addr` with the facts Linux code needs
// beyond health.Interface.
type Link struct {
	health.Interface
	Index    int
	LinkType string // "ether", "loopback", "none" (tun), ...
}

// Physical reports whether the link is a candidate "network service": not
// loopback and not a point-to-point tunnel (tailscale0, mihomo's TUN, VPNs).
func (l Link) Physical() bool {
	return l.LinkType != "loopback" && l.LinkType != "none"
}

// ParseIPAddr parses `ip -j addr show`.
func ParseIPAddr(data []byte) ([]Link, error) {
	var js []struct {
		Ifindex   int      `json:"ifindex"`
		Ifname    string   `json:"ifname"`
		Flags     []string `json:"flags"`
		Operstate string   `json:"operstate"`
		LinkType  string   `json:"link_type"`
		AddrInfo  []struct {
			Family string `json:"family"`
			Local  string `json:"local"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(data, &js); err != nil {
		return nil, fmt.Errorf("ip addr: %w", err)
	}
	var out []Link
	for _, j := range js {
		l := Link{Index: j.Ifindex, LinkType: j.LinkType}
		l.Name = j.Ifname
		for _, f := range j.Flags {
			switch f {
			case "UP":
				l.Up = true
			case "LOWER_UP":
				l.Active = true
			}
		}
		if j.Operstate == "DOWN" {
			l.Active = false
		}
		for _, a := range j.AddrInfo {
			if ip, err := netip.ParseAddr(a.Local); err == nil {
				l.Addrs = append(l.Addrs, ip)
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// ---- DNS ------------------------------------------------------------------

// ResolvConf is /etc/resolv.conf.
type ResolvConf struct {
	Nameservers []string
	Search      []string
	Options     []string
}

// StubAddr is systemd-resolved's stub listener.
const StubAddr = "127.0.0.53"

// Stub reports whether resolv.conf points at systemd-resolved's stub only.
func (r ResolvConf) Stub() bool {
	if len(r.Nameservers) == 0 {
		return false
	}
	for _, n := range r.Nameservers {
		if n != StubAddr && n != "127.0.0.54" {
			return false
		}
	}
	return true
}

// ParseResolvConf parses resolv.conf.
func ParseResolvConf(data string) ResolvConf {
	var r ResolvConf
	for _, l := range strings.Split(data, "\n") {
		if i := strings.IndexAny(l, "#;"); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "nameserver":
			r.Nameservers = append(r.Nameservers, f[1])
		case "search", "domain":
			r.Search = f[1:]
		case "options":
			r.Options = append(r.Options, f[1:]...)
		}
	}
	return r
}

// ResolvedLink is one section of `resolvectl status`: the global section
// (Index 0) or one link.
type ResolvedLink struct {
	Index        int
	Name         string
	DefaultRoute bool // +DefaultRoute (links) or no routing domains (global)
	Servers      []string
	Domains      []string // search domains
	RouteDomains []string // routing-only domains (~x), without the ~
}

var (
	linkHeaderRE = regexp.MustCompile(`^Link (\d+) \(([^)]*)\)$`)
	keyRE        = regexp.MustCompile(`^([A-Za-z][A-Za-z .]*):(?:\s+(.*))?$`)
)

// ParseResolvectl parses `resolvectl status` (systemd ≥ 239 layout).
// Multi-line values continue on lines without a "Key:" prefix.
func ParseResolvectl(out string) []ResolvedLink {
	var res []ResolvedLink
	var cur *ResolvedLink
	key := ""
	add := func(k, v string) {
		if cur == nil {
			return
		}
		for _, w := range strings.Fields(v) {
			switch k {
			case "DNS Servers", "Fallback DNS Servers":
				if k == "DNS Servers" {
					// Drop "#server-name" (DoT) and "%iface" suffixes.
					if i := strings.IndexAny(w, "#"); i > 0 {
						w = w[:i]
					}
					cur.Servers = append(cur.Servers, w)
				}
			case "DNS Domain":
				if strings.HasPrefix(w, "~") {
					cur.RouteDomains = append(cur.RouteDomains, strings.TrimPrefix(w, "~"))
				} else {
					cur.Domains = append(cur.Domains, w)
				}
			case "Protocols":
				switch w {
				case "+DefaultRoute":
					cur.DefaultRoute = true
				case "-DefaultRoute":
					cur.DefaultRoute = false
				}
			case "DefaultRoute setting":
				cur.DefaultRoute = w == "yes"
			}
		}
	}
	for _, raw := range strings.Split(out, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			key = ""
			continue
		}
		if t == "Global" {
			res = append(res, ResolvedLink{Name: "Global", DefaultRoute: true})
			cur, key = &res[len(res)-1], ""
			continue
		}
		if m := linkHeaderRE.FindStringSubmatch(t); m != nil {
			idx, _ := strconv.Atoi(m[1])
			res = append(res, ResolvedLink{Index: idx, Name: m[2]})
			cur, key = &res[len(res)-1], ""
			continue
		}
		if m := keyRE.FindStringSubmatch(t); m != nil {
			key = m[1]
			add(key, m[2])
			continue
		}
		if key == "DNS Servers" || key == "DNS Domain" {
			add(key, t)
		}
	}
	// The global section routes everything unless it has only ~domains.
	for i := range res {
		if res[i].Index == 0 && len(res[i].RouteDomains) > 0 && len(res[i].Domains) == 0 {
			res[i].DefaultRoute = false
			for _, d := range res[i].RouteDomains {
				if d == "." {
					res[i].DefaultRoute = true
				}
			}
		}
	}
	return res
}

// ResolversFrom turns the DNS configuration into health.Resolvers, in the
// shape the checks expect from macOS's `scutil --dns`:
//
//   - one unscoped default resolver (Domain "") with the servers that answer
//     names no domain routes: resolv.conf's nameservers, or with the
//     systemd-resolved stub the global servers, else those of the first
//     default-route link;
//   - one resolver per (routing or search) domain with that link's servers,
//     e.g. the tailnet suffix and the 100.in-addr.arpa zones tailscaled
//     registers on tailscale0 (Scoped, IfIndex set);
//   - without resolved, each search domain gets the resolv.conf servers,
//     which is how queries for it are actually answered.
func ResolversFrom(rc ResolvConf, links []ResolvedLink) []health.Resolver {
	var out []health.Resolver
	if !rc.Stub() || len(links) == 0 {
		if len(rc.Nameservers) > 0 {
			out = append(out, health.Resolver{Nameservers: rc.Nameservers, SearchDomains: rc.Search,
				Options: strings.Join(rc.Options, " ")})
			for _, d := range rc.Search {
				out = append(out, health.Resolver{Domain: d, Nameservers: rc.Nameservers})
			}
		}
		return out
	}
	var def []string
	for _, l := range links {
		if l.Index == 0 && len(l.Servers) > 0 && l.DefaultRoute {
			def = l.Servers
		}
	}
	if def == nil {
		for _, l := range links {
			if l.Index != 0 && l.DefaultRoute && len(l.Servers) > 0 {
				def = l.Servers
				break
			}
		}
	}
	if def != nil {
		out = append(out, health.Resolver{Nameservers: def, SearchDomains: rc.Search})
	}
	for _, l := range links {
		if len(l.Servers) == 0 {
			continue
		}
		idx := ""
		if l.Index > 0 {
			idx = strconv.Itoa(l.Index)
		}
		for _, d := range append(append([]string(nil), l.Domains...), l.RouteDomains...) {
			if d == "." {
				continue
			}
			out = append(out, health.Resolver{Domain: d, Nameservers: l.Servers, IfIndex: idx, Scoped: l.Index > 0})
		}
		if l.Index > 0 && l.DefaultRoute {
			out = append(out, health.Resolver{Nameservers: l.Servers, IfIndex: idx, Scoped: true})
		}
	}
	return out
}

// ---- NetworkManager -------------------------------------------------------

// NMConnection is one line of `nmcli -t -f NAME,UUID,TYPE,DEVICE,STATE con show`.
type NMConnection struct {
	Name, UUID, Type, Device, State string
}

// SplitNmcliTerse splits one line of nmcli's terse output: fields are
// separated by ':', with '\:' and '\\' escaping literal characters.
func SplitNmcliTerse(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == ':':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, cur.String())
}

// ParseNmcliConnections parses `nmcli -t -f NAME,UUID,TYPE,DEVICE,STATE con show`.
func ParseNmcliConnections(out string) []NMConnection {
	var cs []NMConnection
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := SplitNmcliTerse(l)
		if len(f) < 5 {
			continue
		}
		cs = append(cs, NMConnection{Name: f[0], UUID: f[1], Type: f[2], Device: f[3], State: f[4]})
	}
	return cs
}

// VPNsFromNM returns VPN-like NetworkManager connections (vpn, wireguard).
// tailscaled is not one of them: it manages its own tun outside NM.
func VPNsFromNM(cs []NMConnection) []health.VPN {
	var out []health.VPN
	for _, c := range cs {
		t := strings.ToLower(c.Type)
		if t != "vpn" && t != "wireguard" && !strings.HasSuffix(t, "-vpn") {
			continue
		}
		v := health.VPN{Name: c.Name, Kind: "NetworkManager " + c.Type, BundleID: "nm." + t, Enabled: true}
		switch c.State {
		case "activated":
			v.Status = "Connected"
		case "activating":
			v.Status = "Connecting"
		default:
			v.Status = "Disconnected"
		}
		out = append(out, v)
	}
	return out
}

// NMCLIDNSFields are the fields nmDNSSource asks nmcli for, in order.
const NMCLIDNSFields = "ipv4.method,ipv4.dns,ipv4.ignore-auto-dns,ipv6.dns,ipv6.ignore-auto-dns"

// ParseNmcliDNSSource classifies where a connection's DNS servers come from,
// from `nmcli -g <NMCLIDNSFields> connection show uuid <UUID>` (one value
// per line; lists comma separated, ':' escaped as '\:'):
//   - static servers, and DHCP ones ignored (or no DHCP at all): manual;
//   - static servers next to the DHCP ones: mixed;
//   - no static servers and DHCP/auto addressing: auto;
//   - no static servers and manual addressing: global (resolved's or
//     resolv.conf's servers apply).
//
// It returns "" when the output has an unexpected shape.
func ParseNmcliDNSSource(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 5 {
		return ""
	}
	val := func(i int) string { return strings.TrimSpace(SplitNmcliTerse(lines[i])[0]) }
	// SplitNmcliTerse unescapes and splits at unescaped ':'; -g output has
	// one field per line, so re-join the pieces of IPv6 lists.
	list := func(i int) string { return strings.TrimSpace(strings.Join(SplitNmcliTerse(lines[i]), ":")) }
	method := val(0)
	static := list(1) != "" || list(3) != ""
	ignore4, ignore6 := val(2) == "yes", val(4) == "yes"
	noDHCP := method == "manual" || method == "disabled" || method == "shared"
	switch {
	case static && (noDHCP || ignore4 || ignore6):
		return health.DNSSourceManual
	case static:
		return health.DNSSourceMixed
	case noDHCP:
		return health.DNSSourceGlobal
	}
	return health.DNSSourceAuto
}

// ---- getent ---------------------------------------------------------------

// ParseGetent parses `getent ahostsv4 <name>` ("ADDR STREAM name" lines).
// An empty output means the name did not resolve.
func ParseGetent(name, out string) health.DNSAnswer {
	a := health.DNSAnswer{Name: name, Rcode: "NOERROR"}
	seen := map[netip.Addr]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		if ip, err := netip.ParseAddr(f[0]); err == nil && !seen[ip] {
			seen[ip] = true
			a.Addrs = append(a.Addrs, ip)
		}
	}
	if len(a.Addrs) == 0 {
		a.Rcode = "NXDOMAIN"
	}
	return a
}
