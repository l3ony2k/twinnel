package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Path is how the direct request leaves the machine: the physical
// interface its sockets are pinned to and the DNS servers that resolve a
// hostname for it over that interface.
//
// The system resolver cannot be used for the direct request: when mihomo
// is the system DNS it answers with fake-ip addresses (198.18.0.0/15) that
// only exist inside mihomo's TUN, and a socket pinned to the physical
// interface cannot reach them.
type Path struct {
	Iface Iface
	// DNS are the network's own servers, best first: the ones the system
	// used before Twinnel pointed it at mihomo, the DHCP ones, the
	// gateway. DNSCandidates drops the unusable ones and appends the
	// public fallbacks.
	DNS []netip.Addr
}

// PublicDNS are tried after the network's own servers, in order. The first
// two answer directly from mainland China (where 1.1.1.1 and 8.8.8.8 are
// often unreachable on a direct path); the last two everywhere else.
//
//	223.5.5.5     AliDNS (Alibaba)
//	119.29.29.29  DNSPod (Tencent)
//	1.1.1.1       Cloudflare
//	8.8.8.8       Google
var PublicDNS = []netip.Addr{
	netip.MustParseAddr("223.5.5.5"),
	netip.MustParseAddr("119.29.29.29"),
	netip.MustParseAddr("1.1.1.1"),
	netip.MustParseAddr("8.8.8.8"),
}

// notPhysicalDNS are resolvers that are not on the physical network:
// mihomo's fake-ip range, Tailscale's MagicDNS, and the site-local
// placeholders Windows lists for an adapter without IPv6 servers.
var notPhysicalDNS = []netip.Prefix{
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("100.100.100.100/32"),
	netip.MustParsePrefix("fd7a:115c:a1e0::53/128"),
	netip.MustParsePrefix("fec0::/10"),
}

// DNSCandidates returns the servers to try, in order: the usable ones of
// own (no loopback, unspecified, link-local, fake-ip or MagicDNS address,
// no duplicates), then PublicDNS.
func DNSCandidates(own []netip.Addr) []netip.Addr {
	var out []netip.Addr
	add := func(a netip.Addr) {
		a = a.Unmap()
		if !a.IsValid() || a.IsLoopback() || a.IsUnspecified() || a.IsLinkLocalUnicast() || a.IsMulticast() {
			return
		}
		for _, p := range notPhysicalDNS {
			if p.Contains(a) {
				return
			}
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	for _, a := range own {
		add(a)
	}
	for _, a := range PublicDNS {
		add(a)
	}
	return out
}

// dnsAttempt bounds one server's answer.
const dnsAttempt = 2 * time.Second

// dnsPort is the servers' port (a variable for tests).
var dnsPort = "53"

// LookupFunc resolves host with the DNS server at server.
type LookupFunc func(ctx context.Context, server netip.Addr, host string) ([]netip.Addr, error)

// Resolve resolves host to IPv4 addresses (the direct request goes out
// on the interface picked for IPv4) by asking the servers in order, each
// for at most dnsAttempt, with lookup (nil: PinnedLookup over p.Iface).
// A host that is already an address is returned as is.
func Resolve(ctx context.Context, host string, p Path, lookup LookupFunc) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a.Unmap()}, nil
	}
	if lookup == nil {
		lookup = PinnedLookup(p.Iface)
	}
	var errs []string
	for _, s := range DNSCandidates(p.DNS) {
		if ctx.Err() != nil {
			break
		}
		actx, cancel := context.WithTimeout(ctx, dnsAttempt)
		as, err := lookup(actx, s, host)
		cancel()
		if err == nil && len(as) > 0 {
			return as, nil
		}
		if err == nil {
			err = errors.New("no IPv4 address")
		}
		errs = append(errs, fmt.Sprintf("%s: %v", s, shortErr(err)))
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("resolve %s: %w", host, context.Cause(ctx))
	}
	return nil, fmt.Errorf("resolve %s on the physical interface: %s", host, strings.Join(errs, "; "))
}

// PinnedLookup asks one server for A records over a UDP (then TCP)
// socket pinned to ifc, the same way the request itself is pinned.
func PinnedLookup(ifc Iface) LookupFunc {
	d := Dialer(ifc)
	return func(ctx context.Context, server netip.Addr, host string) ([]netip.Addr, error) {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return d.DialContext(ctx, network, net.JoinHostPort(server.String(), dnsPort))
		}}
		as, err := r.LookupNetIP(ctx, "ip4", host)
		for i := range as {
			as[i] = as[i].Unmap()
		}
		return as, err
	}
}

// shortErr drops the "lookup x on y:" prefix Go's resolver adds; the
// caller names the server.
func shortErr(err error) string {
	var de *net.DNSError
	if errors.As(err, &de) {
		if de.IsTimeout {
			return "timeout"
		}
		return de.Err
	}
	return err.Error()
}

// hostPort is the host and port (default 443) of an https URL.
func hostPort(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("egress URL must be https: %q", rawURL)
	}
	port := portOr(u.Port(), "443")
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("egress URL port: %q", rawURL)
	}
	return u.Hostname(), port, nil
}
