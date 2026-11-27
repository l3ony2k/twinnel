package windows

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/sysdns"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// Why Windows points the adapters at mihomo (and does not rely on TUN's
// dns-hijack alone):
//
//   - The DNS client sends a query to the servers of every connected
//     adapter at once (smart multi-homed name resolution) and takes the
//     first answer. The physical adapter's servers get every name, beside
//     whatever goes into the TUN: a leak, and a race in which a real
//     address can beat mihomo's fake-ip.
//   - The usual server is the router (192.168.x.1, 10.x). The base
//     settings keep private ranges out of the TUN (route-exclude-address),
//     so dns-hijack never sees those queries.
//   - An adapter's IPv6 servers (router advertisements) are reached over
//     IPv6, which mihomo's TUN does not carry with ipv6: false.
//
// Pointing the physical adapters at 127.0.0.1 (IPv4) and ::1 (IPv6, where
// nothing listens, so the query fails at once and never leaves the host)
// closes all three. MagicDNS keeps working: Tailscale puts its tailnet
// suffix and split-DNS routes in the NRPT, which Windows consults before
// any adapter's servers, and its search suffix turns short names into
// tailnet names first. The Tailscale adapter, mihomo's TUN and other
// tunnels are never touched.

// DNSTarget is what dns.point sets on Windows: mihomo's listener for IPv4
// and the loopback for IPv6 (see above).
var DNSTarget = []string{"127.0.0.1", "::1"}

// StaticDNS reads the DNS servers set by hand on an adapter (the registry
// NameServer values), per family; none means the servers come from the
// network (DHCP, router advertisements).
type StaticDNS interface {
	StaticDNS(guid string) (v4, v6 []string, err error)
}

// SystemDNS is sysdns.Platform on Windows: the adapters come from Sys, the
// hand-set servers from Static, and changes go through netsh (Run; only
// the elevated helper passes a real one).
type SystemDNS struct {
	Sys    Sys
	Static StaticDNS
	Run    CommandRunner
}

var _ sysdns.Platform = SystemDNS{}

// DNSTarget implements the engine's optional per-platform target.
func (SystemDNS) DNSTarget() []string { return append([]string(nil), DNSTarget...) }

// virtualDescriptions are substrings (lower case) of the descriptions of
// host-only virtual adapters: they reach only VMs on this machine.
var virtualDescriptions = []string{
	"hyper-v", "vmware", "virtualbox", "parallels", "virtual ethernet",
	"virtual adapter", "wi-fi direct", "loopback", "kernel debug",
}

// Virtual reports whether a is a virtual switch / host-only adapter
// (Hyper-V's vEthernet, WSL's, VMware's, VirtualBox's).
func (a Adapter) Virtual() bool {
	if strings.HasPrefix(strings.ToLower(a.Name), "vethernet") {
		return true
	}
	d := strings.ToLower(a.Description)
	for _, s := range virtualDescriptions {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}

// DNSManaged reports whether dns.point manages a's DNS and the system-dns
// check looks at it: the physical adapters, connected or not (so a cable
// plugged in later is already pointed). Tunnels (Tailscale, mihomo's TUN,
// other VPNs) and the loopback are left alone. A virtual adapter counts
// only while it holds a default gateway: then it is a Hyper-V external
// switch that carries the host's own traffic.
func DNSManaged(a Adapter) bool {
	if a.Loopback() || a.Tunnel() {
		return false
	}
	if a.Virtual() {
		return a.Up && len(a.Gateways) > 0
	}
	return true
}

// Services lists the managed adapters with their hand-set servers, IPv4
// first.
func (d SystemDNS) Services(ctx context.Context) ([]sysdns.Service, error) {
	ads, err := d.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	var out []sysdns.Service
	for _, a := range physicalAdapters(ads) {
		v4, v6, err := d.Static.StaticDNS(a.GUID)
		if err != nil {
			return nil, fmt.Errorf("read the DNS of %s: %w", a.Name, err)
		}
		out = append(out, sysdns.Service{Name: a.Name, Servers: append(append([]string{}, v4...), v6...)})
	}
	return out, nil
}

// Set gives the adapter named name exactly these hand-set servers: the
// IPv4 ones for IPv4 and the IPv6 ones for IPv6; a family with none goes
// back to the network's servers.
func (d SystemDNS) Set(ctx context.Context, name string, servers []string) error {
	ads, err := d.Sys.Adapters(ctx)
	if err != nil {
		return err
	}
	var ad *Adapter
	for i := range ads {
		if ads[i].Name == name && DNSManaged(ads[i]) {
			ad = &ads[i]
			break
		}
	}
	if ad == nil {
		return fmt.Errorf("no network adapter named %q", name)
	}
	var v4, v6 []string
	for _, s := range servers {
		a, err := netip.ParseAddr(s)
		if err != nil || a.Zone() != "" {
			return fmt.Errorf("not an IP address: %q", s)
		}
		if a.Is4() || a.Is4In6() {
			v4 = append(v4, a.Unmap().String())
		} else {
			v6 = append(v6, a.String())
		}
	}
	cur4, cur6, err := d.Static.StaticDNS(ad.GUID)
	if err != nil {
		return err
	}
	if !slices.Equal(cur4, v4) {
		if err := d.setFamily(ctx, "ipv4", ad.Index, v4); err != nil {
			return err
		}
	}
	if !slices.Equal(cur6, v6) {
		if err := d.setFamily(ctx, "ipv6", ad.Index, v6); err != nil {
			return err
		}
	}
	return nil
}

// setFamily runs netsh for one family: `set dnsservers source=dhcp` for
// none, else `set ... source=static` with the first server and `add` for
// the rest. The adapter is named by its index (no quoting of names with
// spaces or non-ASCII letters); validate=no skips netsh's slow test
// query.
func (d SystemDNS) setFamily(ctx context.Context, family string, index uint32, servers []string) error {
	if d.Run == nil {
		return errors.New("setting the system DNS is not available here")
	}
	netsh := winpath.Netsh()
	name := "name=" + strconv.FormatUint(uint64(index), 10)
	var cmds [][]string
	if len(servers) == 0 {
		cmds = append(cmds, []string{"interface", family, "set", "dnsservers", name, "source=dhcp"})
	}
	for i, s := range servers {
		if i == 0 {
			cmds = append(cmds, []string{"interface", family, "set", "dnsservers", name, "source=static", "address=" + s, "validate=no"})
			continue
		}
		cmds = append(cmds, []string{"interface", family, "add", "dnsservers", name, "address=" + s, "index=" + strconv.Itoa(i+1), "validate=no"})
	}
	for _, c := range cmds {
		out, err := d.Run(ctx, netsh, c...)
		if err != nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return fmt.Errorf("%w: %s", err, s)
			}
			return err
		}
	}
	return nil
}

// ParseNameServer splits a registry NameServer value ("a,b" or "a b")
// into its addresses, dropping anything that is not one.
func ParseNameServer(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if a, err := netip.ParseAddr(f); err == nil {
			out = append(out, a.Unmap().String())
		}
	}
	return out
}

// defaultV6DNS are the placeholders Windows lists as an adapter's IPv6
// servers when it has none. Nothing routes them, so nothing leaks there.
var defaultV6DNS = []netip.Addr{
	netip.MustParseAddr("fec0:0:0:ffff::1"),
	netip.MustParseAddr("fec0:0:0:ffff::2"),
	netip.MustParseAddr("fec0:0:0:ffff::3"),
}

// withoutDefaultV6DNS drops the defaultV6DNS placeholders.
func withoutDefaultV6DNS(as []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range as {
		if !slices.Contains(defaultV6DNS, a.WithZone("")) {
			out = append(out, a)
		}
	}
	return out
}
