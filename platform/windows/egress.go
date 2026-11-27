package windows

import (
	"context"
	"errors"
	"net/netip"
	"slices"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

var _ health.EgressEnv = (*Env)(nil)

// EgressAdapter is the adapter a request to dest leaves by when tunnels
// are taken out of the picture: among the up adapters that are not tunnels
// (mihomo's wintun, Tailscale, other VPNs) or loopback, the one Windows
// would pick by its own routes (longest prefix, lowest metric).
func EgressAdapter(routes []RouteRow, adapters []Adapter, dest netip.Addr) (Adapter, bool) {
	phys := map[uint32]bool{}
	var ads []Adapter
	for _, a := range adapters {
		if a.Up && !a.Tunnel() && !a.Loopback() && hasFamily(a.Addrs, dest.Is4()) {
			phys[a.Index] = true
			ads = append(ads, a)
		}
	}
	var rs []RouteRow
	for _, r := range routes {
		if phys[r.IfIndex] {
			rs = append(rs, r)
		}
	}
	_, a, ok := BestRoute(rs, ads, dest)
	return a, ok
}

func hasFamily(addrs []netip.Addr, v4 bool) bool {
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is4() == v4 && !a.IsLoopback() && !a.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// DirectEgress fetches url on a socket pinned to the physical adapter
// (IP_UNICAST_IF), so the request bypasses mihomo's TUN even though the
// TUN holds the default routes, and returns the IP the echo reports. A
// hostname is resolved over the same adapter (DirectPath).
func (e *Env) DirectEgress(ctx context.Context, rawURL string) (netip.Addr, error) {
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	routes, err := e.opt.Sys.Routes(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	a, ok := EgressAdapter(routes, ads, egress.DestFor(rawURL))
	if !ok {
		return netip.Addr{}, errors.New("no active physical adapter")
	}
	p := DirectPath(a, e.Settings().DirectDNS, e.opt.DHCPDNS(a.GUID))
	if e.opt.Direct != nil {
		return e.opt.Direct(ctx, rawURL, p)
	}
	return egress.Direct(ctx, rawURL, p)
}

// DirectPath is the direct request's path over adapter a: pinned to it,
// resolving with the servers the system used before Twinnel (backup), the
// ones DHCP gave a, the ones a is set to now, then its gateways
// (egress.DNSCandidates drops mihomo's and Tailscale's addresses).
func DirectPath(a Adapter, backup, dhcp []netip.Addr) egress.Path {
	dns := slices.Concat(backup, dhcp, a.DNS, a.Gateways)
	return egress.Path{Iface: egress.Iface{Index: int(a.Index), Name: a.Name}, DNS: dns}
}

// ProxiedEgress fetches url through mihomo's mixed (or HTTP) port and
// returns the IP the echo reports and the proxy chain from GET
// /connections.
func (e *Env) ProxiedEgress(ctx context.Context, rawURL string) (netip.Addr, []string, error) {
	rt, err := e.MihomoRuntime(ctx)
	if err != nil {
		return netip.Addr{}, nil, err
	}
	port := rt.Configs.MixedPort
	if port == 0 {
		port = rt.Configs.Port
	}
	addr, secret := e.controller()
	return egress.Proxied(ctx, rawURL, port, egress.ControllerChain(func(ctx context.Context, path string) ([]byte, error) {
		return e.get(ctx, addr, secret, path)
	}))
}
