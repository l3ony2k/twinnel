package health

import (
	"context"
	"strings"
	"testing"
)

func TestVPNConflictWindowsAdapters(t *testing.T) {
	winEnv := func(defIface string, tunnel bool, tunDev string, tunOn bool) *fakeEnv {
		e := healthyEnv()
		e.platform = Windows
		e.rt.Configs.TUN.Device, e.rt.Configs.TUN.Enable = tunDev, tunOn
		rl := RouteLookup{Interface: defIface}
		if tunnel {
			rl.Flags = []string{RouteFlagTunnel}
		}
		e.routes["1.1.1.1"] = rl
		return e
	}
	// Another tunnel adapter (WireGuard, OpenVPN...) fails, TUN on or off,
	// whatever its friendly name.
	want(t, VPNConflict{}, winEnv("Office VPN", true, "Meta", true), StatusFail, "Office VPN")
	want(t, VPNConflict{}, winEnv("wg-home", true, "", false), StatusFail, "wg-home")
	// mihomo's TUN (named, or the default Meta*) and Tailscale's adapter are fine.
	want(t, VPNConflict{}, winEnv("Meta", true, "", true), StatusOK, "")
	want(t, VPNConflict{}, winEnv("Meta1", true, "", true), StatusOK, "")
	want(t, VPNConflict{}, winEnv("mihomo", true, "mihomo", true), StatusOK, "")
	want(t, VPNConflict{}, winEnv("Tailscale", true, "Meta", false), StatusOK, "")
	// mihomo's own TUN while its TUN reads as off is not another VPN.
	want(t, VPNConflict{}, winEnv("Meta", true, "", false), StatusOK, "")
	// A physical adapter: warn only when mihomo's TUN should hold it.
	want(t, VPNConflict{}, winEnv("Wi-Fi", false, "Meta", true), StatusWarn, "Wi-Fi")
	want(t, VPNConflict{}, winEnv("Ethernet", false, "", false), StatusOK, "")
}

func TestDNSFixTextWindows(t *testing.T) {
	for _, L := range []*texts{&zhTexts, &enTexts} {
		s := dnsFixText(L, Windows, "127.0.0.1")
		if !strings.Contains(s, "Set-DnsClientServerAddress") || strings.Count(s, "127.0.0.1") != 2 {
			t.Errorf("windows fix hint: %q", s)
		}
		if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "。") {
			t.Errorf("one-sentence fix hint ends with a period: %q", s)
		}
	}
	e := healthyEnv()
	e.platform = Windows
	e.svcs[1].DNS = []string{"192.168.1.1"}
	e.cfg.TUN.DNSHijack, e.rt.Configs.TUN.DNSHijack = nil, nil
	r := SystemDNS{}.Run(context.Background(), e)
	if r.Fix == nil || !strings.Contains(r.Fix.Text, "Set-DnsClientServerAddress") {
		t.Errorf("system-dns on windows: %+v", r)
	}
}

// dns.point sets ::1 as the IPv6 server on Windows (nothing listens there,
// so nothing leaks): the check takes it as pointed there and nowhere else.
func TestSystemDNSWindowsLoopbackV6(t *testing.T) {
	for _, plat := range []string{Windows, Darwin, Linux} {
		e := healthyEnv()
		e.platform = plat
		e.cfg.TUN.DNSHijack, e.rt.Configs.TUN.DNSHijack = nil, nil
		for i := range e.svcs {
			e.svcs[i].DNS = []string{"127.0.0.1", "::1"}
		}
		r := SystemDNS{}.Run(context.Background(), e)
		if wantOK := plat == Windows; (r.Status == StatusOK) != wantOK {
			t.Errorf("%s: %s %q", plat, r.Status, r.Summary)
		}
	}
}
