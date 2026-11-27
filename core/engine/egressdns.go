package engine

import "net/netip"

// EgressDirectDNS are the DNS servers the system used before Twinnel
// pointed it at mihomo (the DNS backup), for the egress test's direct
// request: it resolves names over the physical interface, and mihomo's
// own listener cannot be used there. Nil when there is no backup or the
// backup recorded only automatic (DHCP) services; the platform then reads
// the DHCP servers itself.
func (e *Engine) EgressDirectDNS() []netip.Addr {
	if e.Store == nil {
		return nil
	}
	bk, err := e.dnsManager().ReadBackup()
	if err != nil || bk == nil {
		return nil
	}
	var out []netip.Addr
	for _, s := range bk.Services {
		for _, v := range s.Servers {
			if a, err := netip.ParseAddr(v); err == nil {
				out = append(out, a)
			}
		}
	}
	return out
}
