//go:build darwin

package darwin

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

var _ health.EgressEnv = (*Env)(nil)

// physicalInterface is the first active network service's device with a
// global address, in service order (what mihomo's auto-detect-interface
// picks as well), and the DNS servers set on that service.
func (e *Env) physicalInterface(ctx context.Context) (string, []netip.Addr, error) {
	svcs, err := e.NetworkServices(ctx)
	if err != nil {
		return "", nil, err
	}
	ifs, err := e.Interfaces(ctx)
	if err != nil {
		return "", nil, err
	}
	byName := map[string]health.Interface{}
	for _, i := range ifs {
		byName[i.Name] = i
	}
	for _, s := range svcs {
		i, ok := byName[s.Device]
		if s.Device == "" || !ok || !i.Active || strings.HasPrefix(s.Device, "utun") {
			continue
		}
		for _, a := range i.Addrs {
			if a.Is4() && !a.IsLinkLocalUnicast() && !a.IsLoopback() {
				var dns []netip.Addr
				for _, d := range s.DNS {
					if a, err := netip.ParseAddr(d); err == nil {
						dns = append(dns, a)
					}
				}
				return s.Device, dns, nil
			}
		}
	}
	return "", nil, errors.New("no active physical interface")
}

// DirectEgress fetches url on a socket bound to the physical interface
// (IP_BOUND_IF), so the request bypasses mihomo's TUN, and returns the IP
// the echo reports. A hostname is resolved over the same interface with,
// in order: the servers the system used before Twinnel (the helper's
// backup), the ones DHCP gave the interface, the ones its service is set
// to now, and its router (egress.DNSCandidates drops mihomo's 127.0.0.1).
func (e *Env) DirectEgress(ctx context.Context, rawURL string) (netip.Addr, error) {
	name, svcDNS, err := e.physicalInterface(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, err
	}
	var dhcp, routers []netip.Addr
	if out, err := e.run(ctx, "/usr/sbin/ipconfig", "getpacket", name); err == nil {
		dhcp, routers = ParseDHCPPacket(out)
	}
	dns := slices.Concat(e.Settings().DirectDNS, dhcp, svcDNS, routers)
	return egress.Direct(ctx, rawURL, egress.Path{Iface: egress.Iface{Index: ifi.Index, Name: ifi.Name}, DNS: dns})
}

// ParseDHCPPacket reads the DNS servers and routers from `ipconfig
// getpacket <dev>` ("domain_name_server (ip_mult): {a, b}").
func ParseDHCPPacket(out string) (dns, routers []netip.Addr) {
	for _, l := range lines(out) {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		var dst *[]netip.Addr
		switch strings.TrimSpace(strings.Split(k, "(")[0]) {
		case "domain_name_server":
			dst = &dns
		case "router":
			dst = &routers
		default:
			continue
		}
		for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == '{' || r == '}' || r == ',' || r == ' ' }) {
			if a, err := netip.ParseAddr(f); err == nil {
				*dst = append(*dst, a)
			}
		}
	}
	return dns, routers
}

// ProxiedEgress fetches url through mihomo's mixed (or HTTP) port with a
// CONNECT tunnel, looks up the tunnel in GET /connections to learn the
// proxy chain, and returns the IP the echo reports.
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

// ParseConnectionChain finds the chain of the connection from srcPort in a
// GET /connections body.
func ParseConnectionChain(body []byte, srcPort int) ([]string, bool) {
	return egress.ParseConnectionChain(body, srcPort)
}
