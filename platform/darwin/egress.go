//go:build darwin

package darwin

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/platform/egress"
)

var _ health.EgressEnv = (*Env)(nil)

// physicalInterface is the first active network service's device with a
// global address, in service order (what mihomo's auto-detect-interface
// picks as well).
func (e *Env) physicalInterface(ctx context.Context) (string, error) {
	svcs, err := e.NetworkServices(ctx)
	if err != nil {
		return "", err
	}
	ifs, err := e.Interfaces(ctx)
	if err != nil {
		return "", err
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
				return s.Device, nil
			}
		}
	}
	return "", errors.New("no active physical interface")
}

// DirectEgress fetches url on a socket bound to the physical interface
// (IP_BOUND_IF), so the request bypasses mihomo's TUN, and returns the IP
// the echo reports.
func (e *Env) DirectEgress(ctx context.Context, rawURL string) (netip.Addr, error) {
	name, err := e.physicalInterface(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, err
	}
	return egress.Direct(ctx, rawURL, egress.Iface{Index: ifi.Index, Name: ifi.Name})
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
