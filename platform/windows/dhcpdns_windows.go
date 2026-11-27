//go:build windows

package windows

import (
	"net/netip"

	"golang.org/x/sys/windows/registry"
)

// DHCPDNS reads the DNS servers DHCP gave the adapter with this registry
// GUID (DhcpNameServer, IPv4), whatever the adapter is set to use now.
// Read-only; nil when there are none.
func DHCPDNS(guid string) []netip.Addr {
	if guid == "" {
		return nil
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, tcpipInterfaces+guid, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	s, _, err := k.GetStringValue("DhcpNameServer")
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, v := range ParseNameServer(s) {
		if a, err := netip.ParseAddr(v); err == nil {
			out = append(out, a)
		}
	}
	return out
}
