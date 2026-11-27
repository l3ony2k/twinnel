//go:build !windows

package windows

import "net/netip"

// DHCPDNS reads the Windows registry; elsewhere there is nothing to read.
func DHCPDNS(string) []netip.Addr { return nil }
