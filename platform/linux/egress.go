package linux

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

var _ health.EgressEnv = (*Env)(nil)

// EgressLink is the link a request to an address of the given family
// leaves by when tunnels are taken out of the picture: the main table's
// default route with the lowest metric whose device is a physical link
// that is up, has carrier and holds a global address of that family.
// mihomo's TUN and tailscale0 are not physical (link_type none), and
// mihomo's own default route lives in its own table.
func EgressLink(routes []health.Route, links []Link, v4 bool) (Link, bool) {
	fam := 6
	if v4 {
		fam = 4
	}
	byName := map[string]Link{}
	for _, l := range links {
		byName[l.Name] = l
	}
	best, ok, bestMetric := Link{}, false, 0
	for _, r := range routes {
		if r.Family != fam || (r.Destination != "default" && r.Destination != "0.0.0.0/0" && r.Destination != "::/0") {
			continue
		}
		metric, table := routeFlags(r.Flags)
		if table != "" {
			continue
		}
		l, known := byName[r.Interface]
		if !known || !l.Physical() || !l.Up || !l.Active || !hasGlobal(l.Addrs, v4) {
			continue
		}
		if !ok || metric < bestMetric {
			best, ok, bestMetric = l, true, metric
		}
	}
	return best, ok
}

// routeFlags reads "metric N" and "table T" from health.Route.Flags as
// ParseIPRoutes writes them.
func routeFlags(flags string) (metric int, table string) {
	for _, f := range strings.Split(flags, ",") {
		switch {
		case strings.HasPrefix(f, "metric "):
			metric, _ = strconv.Atoi(strings.TrimPrefix(f, "metric "))
		case strings.HasPrefix(f, "table "):
			table = strings.TrimPrefix(f, "table ")
		}
	}
	return metric, table
}

func hasGlobal(addrs []netip.Addr, v4 bool) bool {
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is4() == v4 && a.IsGlobalUnicast() {
			return true
		}
	}
	return false
}

// DirectEgress fetches url on a socket bound to the physical link
// (SO_BINDTODEVICE), so the request bypasses mihomo's TUN, and returns the
// IP the echo reports. A hostname is resolved over the same link
// (directDNS), never through the system resolver.
func (e *Env) DirectEgress(ctx context.Context, rawURL string) (netip.Addr, error) {
	routes, err := e.Routes(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	links, err := e.links(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	l, ok := EgressLink(routes, links, egress.DestFor(rawURL).Is4())
	if !ok {
		return netip.Addr{}, errors.New("no active physical interface")
	}
	p := egress.Path{Iface: egress.Iface{Index: l.Index, Name: l.Name}, DNS: e.directDNS(ctx, l, routes)}
	if e.opt.Direct != nil {
		return e.opt.Direct(ctx, rawURL, p)
	}
	return egress.Direct(ctx, rawURL, p)
}

// UpstreamResolvConf is systemd-resolved's list of its upstream servers
// (what it forwards to, not the 127.0.0.53 stub).
const UpstreamResolvConf = "/run/systemd/resolve/resolv.conf"

// directDNS are the DNS servers for the direct request over link l, in
// order: the ones the system used before Twinnel (the helper's backup),
// the link's own (systemd-resolved per link, else resolv.conf), resolved's
// upstream list, and the link's default gateway. egress.DNSCandidates
// drops loopback ones (mihomo's listener, the resolved stub).
func (e *Env) directDNS(ctx context.Context, l Link, routes []health.Route) []netip.Addr {
	var out []netip.Addr
	add := func(ss ...string) {
		for _, s := range ss {
			if a, err := netip.ParseAddr(s); err == nil {
				out = append(out, a)
			}
		}
	}
	out = append(out, e.Settings().DirectDNS...)
	if svcs, err := e.NetworkServices(ctx); err == nil {
		for _, s := range svcs {
			if s.Device == l.Name {
				add(s.DNS...)
			}
		}
	}
	if b, err := e.opt.ReadFile(e.opt.UpstreamResolvConf); err == nil {
		add(ParseResolvConf(string(b)).Nameservers...)
	}
	for _, r := range routes {
		if r.Interface == l.Name && r.Gateway != "" && (r.Destination == "default" || r.Destination == "0.0.0.0/0") {
			add(r.Gateway)
		}
	}
	return out
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
	addr, secret := e.controller(ctx)
	return egress.Proxied(ctx, rawURL, port, egress.ControllerChain(func(ctx context.Context, path string) ([]byte, error) {
		return e.get(ctx, addr, secret, path)
	}))
}
