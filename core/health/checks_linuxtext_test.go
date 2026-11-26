package health

import (
	"strings"
	"testing"
)

// Linux text and heuristics of system-dns and vpn-conflict. The file name
// avoids the _linux suffix so the tests run on every OS.

func TestSystemDNSFixHintPerOS(t *testing.T) {
	e := healthyEnv()
	e.svcs[1].DNS = []string{"1.1.1.1"}
	e.rt.Configs.TUN.DNSHijack = nil
	r := want(t, SystemDNS{}, e, StatusFail, "Wi-Fi")
	if r.Fix == nil || !strings.Contains(r.Fix.Text, "networksetup") {
		t.Fatalf("darwin fix: %+v", r.Fix)
	}

	e.platform = Linux
	r = want(t, SystemDNS{}, e, StatusFail, "Wi-Fi")
	if r.Fix == nil || strings.Contains(r.Fix.Text, "networksetup") ||
		!strings.Contains(r.Fix.Text, "nmcli connection modify") || !strings.Contains(r.Fix.Text, "resolved.conf") {
		t.Fatalf("linux fix: %+v", r.Fix)
	}
	if r.Fix.AutoFix != "system-dns" {
		t.Fatalf("auto fix id changed: %q", r.Fix.AutoFix)
	}
}

func TestSystemDNSSourceLabels(t *testing.T) {
	cases := []struct {
		src   string
		dns   []string
		label string
	}{
		{"", []string{"127.0.0.1"}, labelManual},
		{"", nil, labelDHCP},
		{DNSSourceManual, []string{"127.0.0.1"}, labelManual},
		{DNSSourceAuto, []string{"127.0.0.1"}, labelDHCP},
		{DNSSourceMixed, []string{"127.0.0.1"}, labelMixed},
		{DNSSourceUnknown, []string{"127.0.0.1"}, labelLinkDNS},
		{DNSSourceGlobal, nil, labelGlobalDNS},
	}
	for _, c := range cases {
		e := healthyEnv()
		e.platform = Linux
		e.svcs[1].DNS, e.svcs[1].DNSSource = c.dns, c.src
		r := want(t, SystemDNS{}, e, StatusOK, "")
		if !strings.Contains(strings.Join(r.Evidence, "\n"), "［"+c.label+"］") {
			t.Errorf("source %q: label %q not in %q", c.src, c.label, r.Evidence)
		}
	}
}

func TestVPNConflictLinuxTunnels(t *testing.T) {
	linuxEnv := func(defIface, tunDev string, tunOn bool) *fakeEnv {
		e := healthyEnv()
		e.platform = Linux
		e.rt.Configs.TUN.Device, e.rt.Configs.TUN.Enable = tunDev, tunOn
		e.routes["1.1.1.1"] = RouteLookup{Interface: defIface}
		return e
	}
	// Another tunnel holding the default route fails, TUN on or off.
	want(t, VPNConflict{}, linuxEnv("wg0", "Meta", true), StatusFail, "wg0")
	want(t, VPNConflict{}, linuxEnv("tun0", "Meta", true), StatusFail, "tun0")
	want(t, VPNConflict{}, linuxEnv("wg0", "", false), StatusFail, "wg0")
	want(t, VPNConflict{}, linuxEnv("ppp0", "mihomo", true), StatusFail, "ppp0")
	// mihomo's own TUN (named, or the default Meta*) and tailscale0 are fine.
	want(t, VPNConflict{}, linuxEnv("mihomo", "mihomo", true), StatusOK, "")
	want(t, VPNConflict{}, linuxEnv("Meta", "", true), StatusOK, "")
	want(t, VPNConflict{}, linuxEnv("Meta1", "", true), StatusOK, "")
	want(t, VPNConflict{}, linuxEnv("tailscale0", "Meta", false), StatusOK, "")
	// A physical link: warn only when mihomo's TUN should hold it.
	want(t, VPNConflict{}, linuxEnv("eth0", "Meta", true), StatusWarn, "eth0")
	want(t, VPNConflict{}, linuxEnv("eth0", "", false), StatusOK, "")
	// mihomo's TUN named tun0 is still mihomo's.
	want(t, VPNConflict{}, linuxEnv("tun0", "tun0", true), StatusOK, "")

	// darwin unchanged: a non-utun default with the TUN on only warns.
	e := healthyEnv()
	e.routes["1.1.1.1"] = RouteLookup{Interface: "wg0"}
	want(t, VPNConflict{}, e, StatusWarn, "wg0")
}
