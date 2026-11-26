package health

import (
	"net"
	"net/netip"
	"strings"
)

// Tailnet address ranges (fixed by Tailscale).
var (
	TailnetV4 = netip.MustParsePrefix("100.64.0.0/10")
	TailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	// QuadAddr is Tailscale's MagicDNS resolver.
	QuadAddr = netip.MustParseAddr("100.100.100.100")
	// QuadAddrV6 is the IPv6 MagicDNS resolver.
	QuadAddrV6 = netip.MustParseAddr("fd7a:115c:a1e0::53")
	// DefaultFakeIPRange is mihomo's default fake-ip range.
	DefaultFakeIPRange = netip.MustParsePrefix("198.18.0.0/16")
)

// NormRuleType normalizes a mihomo rule type so that the config spelling
// ("DOMAIN-SUFFIX") and the API spelling ("DomainSuffix") compare equal.
func NormRuleType(t string) string {
	t = strings.ToUpper(t)
	t = strings.ReplaceAll(t, "-", "")
	return strings.ReplaceAll(t, "_", "")
}

// ParsePrefixLoose parses "1.2.3.4/16" (masking host bits) or a bare IP.
func ParsePrefixLoose(s string) (netip.Prefix, bool) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen()), true
	}
	return netip.Prefix{}, false
}

// covers reports whether one of list covers all of target.
// It returns the most specific covering entry.
func covers(list []string, target netip.Prefix) (string, bool) {
	best, bestBits := "", -1
	for _, s := range list {
		p, ok := ParsePrefixLoose(s)
		if !ok {
			continue
		}
		if p.Bits() <= target.Bits() && p.Contains(target.Addr()) && p.Bits() > bestBits {
			best, bestBits = s, p.Bits()
		}
	}
	return best, bestBits >= 0
}

// inAny reports whether a is inside one of list.
func inAny(list []string, a netip.Addr) bool {
	for _, s := range list {
		if p, ok := ParsePrefixLoose(s); ok && p.Contains(a) {
			return true
		}
	}
	return false
}

// fakeIPRange returns the fake-ip range of cfg, or the default.
func fakeIPRange(cfg *MihomoConfig) netip.Prefix {
	if cfg != nil && cfg.DNS.FakeIPRange != "" {
		if p, ok := ParsePrefixLoose(cfg.DNS.FakeIPRange); ok {
			return p
		}
	}
	return DefaultFakeIPRange
}

// hostOf returns the host part of "host:port" (or s itself).
func hostOf(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.IsLoopback()
}

// hijacksAll53 reports whether dns-hijack catches DNS to any address.
func hijacksAll53(t TUNConfig) bool {
	if !t.Enable {
		return false
	}
	for _, h := range t.DNSHijack {
		h = strings.TrimPrefix(strings.TrimPrefix(h, "udp://"), "tcp://")
		if h == "any:53" || h == "0.0.0.0:53" || h == ":53" {
			return true
		}
	}
	return false
}

// interfaceWith returns the interface that owns addr.
func interfaceWith(ifs []Interface, addr netip.Addr) string {
	for _, i := range ifs {
		for _, a := range i.Addrs {
			if a == addr {
				return i.Name
			}
		}
	}
	return ""
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
