package windows

import (
	"context"
	"net/netip"
	"sort"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
)

// Interface types (IANA ifType) the Windows facts look at.
const (
	IfTypeEthernet    = 6
	IfTypePPP         = 23
	IfTypeLoopback    = 24
	IfTypePropVirtual = 53 // wintun, wireguard-nt and other virtual adapters
	IfTypeWiFi        = 71
	IfTypeTunnel      = 131
)

// Adapter is one network adapter as GetAdaptersAddresses reports it.
type Adapter struct {
	Index uint32
	// GUID is the adapter's name in the registry ("{...}"), used to read
	// where its DNS servers come from.
	GUID string
	// Name is the friendly name ("Wi-Fi", "Ethernet", "Tailscale", "Meta"),
	// which Go's net.Interfaces and mihomo's interface binding use too.
	Name        string
	Description string
	IfType      uint32
	Up          bool // OperStatus == IfOperStatusUp
	Addrs       []netip.Addr
	DNS         []netip.Addr
	DNSSuffix   string
	Gateways    []netip.Addr
	// Metric4 and Metric6 are the interface metrics added to a route's own
	// metric when Windows picks a route.
	Metric4, Metric6 uint32
}

// tunnelDescriptions are substrings (lower case) of the descriptions of VPN
// and tunnel drivers that register as ordinary Ethernet adapters.
var tunnelDescriptions = []string{
	"wintun", "wireguard", "tap-windows", "openvpn", "tailscale", "tunnel",
	"anyconnect", "fortinet", "fortissl", "globalprotect", "pangp", "nordlynx",
	"mullvad", "proton", "zerotier", "juniper", "pulse secure", "vpn",
}

// Tunnel reports whether a is a tunnel / VPN adapter rather than a link to
// a real network.
func (a Adapter) Tunnel() bool {
	switch a.IfType {
	case IfTypeTunnel, IfTypePPP, IfTypePropVirtual:
		return true
	}
	d := strings.ToLower(a.Description)
	for _, s := range tunnelDescriptions {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}

// Loopback reports whether a is the loopback pseudo-interface.
func (a Adapter) Loopback() bool { return a.IfType == IfTypeLoopback }

// IsTailscale reports whether a is the official client's wintun adapter.
func (a Adapter) IsTailscale() bool {
	return a.Name == "Tailscale" || strings.Contains(strings.ToLower(a.Description), "tailscale")
}

func (a Adapter) metric(is4 bool) uint32 {
	if is4 {
		return a.Metric4
	}
	return a.Metric6
}

// RouteRow is one entry of GetIpForwardTable2.
type RouteRow struct {
	Prefix  netip.Prefix
	NextHop netip.Addr // invalid or unspecified: on-link
	IfIndex uint32
	Metric  uint32
}

// NRPTRule is one Name Resolution Policy Table rule: queries for names in
// Namespaces (".example.ts.net", or a single name) go to Servers.
type NRPTRule struct {
	Namespaces []string
	Servers    []string
}

// Sys reads the machine's network facts. The real one (NewSys) uses
// iphlpapi and the registry on Windows; tests use a fake.
type Sys interface {
	Adapters(ctx context.Context) ([]Adapter, error)
	Routes(ctx context.Context) ([]RouteRow, error)
	NRPT(ctx context.Context) ([]NRPTRule, error)
	// DNSSource says where the DNS servers of the adapter with this GUID
	// come from: health.DNSSourceManual, health.DNSSourceAuto, or "" when
	// unknown.
	DNSSource(guid string) string
}

// BestRoute picks the route Windows uses for dest: among routes whose
// prefix holds dest and whose adapter is up, the longest prefix, then the
// lowest route metric + interface metric. ok is false when none matches.
func BestRoute(routes []RouteRow, adapters []Adapter, dest netip.Addr) (r RouteRow, a Adapter, ok bool) {
	dest = dest.Unmap()
	byIndex := map[uint32]Adapter{}
	for _, ad := range adapters {
		byIndex[ad.Index] = ad
	}
	bestBits, bestCost := -1, uint64(0)
	for _, rt := range routes {
		if !rt.Prefix.IsValid() || rt.Prefix.Addr().Is4() != dest.Is4() || !rt.Prefix.Contains(dest) {
			continue
		}
		ad, known := byIndex[rt.IfIndex]
		if !known || !ad.Up {
			continue
		}
		cost := uint64(rt.Metric) + uint64(ad.metric(dest.Is4()))
		bits := rt.Prefix.Bits()
		if bits > bestBits || bits == bestBits && cost < bestCost {
			r, a, ok, bestBits, bestCost = rt, ad, true, bits, cost
		}
	}
	return r, a, ok
}

// routeLookup turns a chosen route into the health view.
func routeLookup(dest string, r RouteRow, a Adapter) health.RouteLookup {
	rl := health.RouteLookup{Destination: dest, Interface: a.Name}
	if r.NextHop.IsValid() && !r.NextHop.IsUnspecified() {
		rl.Gateway = r.NextHop.String()
	}
	if a.Tunnel() {
		rl.Flags = append(rl.Flags, health.RouteFlagTunnel)
	}
	return rl
}

// isDefault reports whether p is a default route (0.0.0.0/0, ::/0).
func isDefault(p netip.Prefix) bool { return p.Bits() == 0 }

// defaultAdapter is the up, non-tunnel adapter with an IPv4 default route
// and the lowest total metric: whose DNS servers the system uses for names
// no NRPT rule or tunnel adapter claims.
func defaultAdapter(routes []RouteRow, adapters []Adapter) (Adapter, bool) {
	var phys []Adapter
	for _, a := range adapters {
		if a.Up && !a.Tunnel() && !a.Loopback() {
			phys = append(phys, a)
		}
	}
	best, ok := Adapter{}, false
	bestCost := uint64(0)
	for _, rt := range routes {
		if !rt.Prefix.IsValid() || !rt.Prefix.Addr().Is4() || !isDefault(rt.Prefix) {
			continue
		}
		for _, a := range phys {
			if a.Index != rt.IfIndex {
				continue
			}
			cost := uint64(rt.Metric) + uint64(a.Metric4)
			if !ok || cost < bestCost {
				best, ok, bestCost = a, true, cost
			}
		}
	}
	return best, ok
}

func addrStrings(as []netip.Addr) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Unmap().WithZone("").String())
	}
	return out
}

func hasAddr(as []netip.Addr, want netip.Addr) bool {
	for _, a := range as {
		if a.Unmap() == want {
			return true
		}
	}
	return false
}

// resolversFrom builds the resolver view: NRPT rules first (they win over
// everything on Windows), then one scoped resolver per up adapter with DNS
// servers, then the default resolver.
func resolversFrom(nrpt []NRPTRule, routes []RouteRow, adapters []Adapter) []health.Resolver {
	var out []health.Resolver
	for _, r := range nrpt {
		for _, ns := range r.Namespaces {
			d := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ns), "."), ".")
			if d == "" {
				continue
			}
			out = append(out, health.Resolver{Domain: d, Nameservers: append([]string(nil), r.Servers...)})
		}
	}
	quad := netip.MustParseAddr("100.100.100.100")
	for _, a := range adapters {
		if !a.Up || len(a.DNS) == 0 || a.Loopback() {
			continue
		}
		var search []string
		if a.DNSSuffix != "" {
			search = []string{a.DNSSuffix}
		}
		ns := addrStrings(a.DNS)
		idx := itoa(a.Index)
		out = append(out, health.Resolver{Nameservers: ns, SearchDomains: search, IfIndex: idx, Scoped: true})
		if a.DNSSuffix != "" && hasAddr(a.DNS, quad) {
			// Tailscale with "override local DNS": its adapter's suffix is
			// the MagicDNS suffix and its server quad-100.
			out = append(out, health.Resolver{Domain: strings.TrimSuffix(a.DNSSuffix, "."), Nameservers: ns, IfIndex: idx, Scoped: true})
		}
	}
	if d, ok := defaultAdapter(routes, adapters); ok && len(d.DNS) > 0 {
		out = append(out, health.Resolver{Nameservers: addrStrings(d.DNS), IfIndex: itoa(d.Index)})
	}
	return out
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// physicalAdapters are the adapters the system-dns check looks at (not
// loopback, not tunnels), ordered by interface metric, then name.
func physicalAdapters(adapters []Adapter) []Adapter {
	var out []Adapter
	for _, a := range adapters {
		if !a.Loopback() && !a.Tunnel() {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Up != out[j].Up {
			return out[i].Up
		}
		if out[i].Metric4 != out[j].Metric4 {
			return out[i].Metric4 < out[j].Metric4
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// isMihomoTUN reports whether name is mihomo's TUN adapter: tun.device,
// else the default "Meta" (Meta1, ... when taken).
func isMihomoTUN(name, device string) bool {
	if device != "" {
		return name == device
	}
	return strings.HasPrefix(name, "Meta")
}

// vpnsFrom lists the up tunnel adapters that are neither Tailscale's nor
// mihomo's TUN: a VPN that carries traffic beside the two.
func vpnsFrom(adapters []Adapter, mihomoDevice string) []health.VPN {
	var out []health.VPN
	for _, a := range adapters {
		if !a.Up || !a.Tunnel() || a.IsTailscale() || isMihomoTUN(a.Name, mihomoDevice) {
			continue
		}
		kind := "VPN adapter"
		if a.Description != "" {
			kind += " (" + a.Description + ")"
		}
		out = append(out, health.VPN{Name: a.Name, Kind: kind, BundleID: a.Description, Status: "Connected", Enabled: true})
	}
	return out
}
