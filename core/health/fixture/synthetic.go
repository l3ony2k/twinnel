package fixture

import (
	"net/netip"

	"github.com/l3ony2k/twinnel/core/health"
)

// Synthetic is a healthy invented Mac: Wi-Fi on en0, mihomo in TUN mode on
// utun5 with fake-ip DNS, Tailscale on utun9 excluded from the TUN, and a
// tailnet "example-tailnet.ts.net" whose nodes match
// cmd/app/testdata/shots-tailscale-status.json. Addresses are from the
// documentation ranges.
func Synthetic() Machine {
	a := netip.MustParseAddr
	tun := health.TUNConfig{Enable: true, Device: "utun5", Stack: "mixed", AutoRoute: true,
		DNSHijack:           []string{"any:53"},
		RouteExcludeAddress: []string{"192.168.0.0/16", "100.64.0.0/10", "fd7a:115c:a1e0::/48"},
		Inet4Address:        []string{"198.18.0.1/30"}}
	rules := []health.MihomoRule{
		{Index: 0, Type: "PROCESSNAME", Payload: "tailscaled", Target: "DIRECT"},
		{Index: 1, Type: "DOMAINSUFFIX", Payload: "ts.net", Target: "DIRECT"},
		{Index: 2, Type: "DOMAINSUFFIX", Payload: "tailscale.com", Target: "DIRECT"},
		{Index: 3, Type: "IPCIDR", Payload: "100.64.0.0/10", Target: "DIRECT"},
		{Index: 4, Type: "IPCIDR6", Payload: "fd7a:115c:a1e0::/48", Target: "DIRECT"},
	}
	// The tailnet's short names go DIRECT too (the nodes of the shots'
	// status file).
	for _, n := range []string{"laptop", "nas", "exit-sg", "exit-jp", "phone", "tablet", "desktop", "raspberry"} {
		rules = append(rules, health.MihomoRule{Index: len(rules), Type: "DOMAIN", Payload: n, Target: "DIRECT"})
	}
	rules = append(rules,
		health.MihomoRule{Index: len(rules), Type: "GEOSITE", Payload: "google", Target: "Google"},
		health.MihomoRule{Index: len(rules) + 1, Type: "GEOSITE", Payload: "facebook", Target: "Meta"},
		health.MihomoRule{Index: len(rules) + 2, Type: "MATCH", Payload: "", Target: "PROXY"})
	ts := &health.TailscaleStatus{
		BackendState: "Running", TUN: true, MagicDNS: true, MagicDNSSuffix: "example-tailnet.ts.net",
		TailscaleIPs: []netip.Addr{a("100.64.0.1"), a("fd7a:115c:a1e0::1")},
		Self:         health.TailscalePeer{HostName: "laptop", DNSName: "laptop.example-tailnet.ts.net.", OS: "macOS"},
		Peers: []health.TailscalePeer{
			{HostName: "nas", DNSName: "nas.example-tailnet.ts.net.", OS: "linux", TailscaleIPs: []netip.Addr{a("100.64.0.2")}, Online: true, Active: true, CurAddr: "192.0.2.20:41641", Relay: "hkg"},
			{HostName: "exit-sg", DNSName: "exit-sg.example-tailnet.ts.net.", OS: "linux", TailscaleIPs: []netip.Addr{a("100.64.0.3")}, Online: true, Relay: "sin"},
			{HostName: "exit-jp", DNSName: "exit-jp.example-tailnet.ts.net.", OS: "linux", TailscaleIPs: []netip.Addr{a("100.64.0.4")}, Online: true, Relay: "tok"},
			{HostName: "phone", DNSName: "phone.example-tailnet.ts.net.", OS: "iOS", TailscaleIPs: []netip.Addr{a("100.64.0.5")}, Relay: "hkg"},
			{HostName: "tablet", DNSName: "tablet.example-tailnet.ts.net.", OS: "iOS", TailscaleIPs: []netip.Addr{a("100.64.0.6")}, Online: true, Relay: "hkg"},
			{HostName: "desktop", DNSName: "desktop.example-tailnet.ts.net.", OS: "windows", TailscaleIPs: []netip.Addr{a("100.64.0.7")}, Relay: "hkg"},
			{HostName: "raspberry", DNSName: "raspberry.example-tailnet.ts.net.", OS: "linux", TailscaleIPs: []netip.Addr{a("100.64.0.8")}, Online: true, CurAddr: "192.0.2.8:41641", Relay: "hkg"},
		},
	}
	return Machine{
		Routes: map[string]health.RouteLookup{
			"1.1.1.1": {Destination: "1.1.1.1", Interface: "utun5", Gateway: "198.18.0.1"},
		},
		TailnetIface: "utun9",
		TunIface:     "utun5",
		Table: []health.Route{
			{Family: 4, Destination: "default", Gateway: "198.18.0.1", Interface: "utun5"},
			{Family: 4, Destination: "100.64/10", Interface: "utun9"},
			{Family: 6, Destination: "fd7a:115c:a1e0::/48", Interface: "utun9"},
		},
		Interfaces: []health.Interface{
			{Name: "en0", Up: true, Active: true, Addrs: []netip.Addr{a("192.168.1.20")}},
			{Name: "utun5", Up: true, Active: true, Addrs: []netip.Addr{a("198.18.0.1")}},
			{Name: "utun9", Up: true, Active: true, Addrs: []netip.Addr{a("100.64.0.1"), a("fd7a:115c:a1e0::1")}},
		},
		Resolvers: []health.Resolver{
			{Nameservers: []string{"127.0.0.1"}},
			{Domain: "example-tailnet.ts.net", Nameservers: []string{"100.100.100.100"}},
			{Domain: "64.100.in-addr.arpa", Nameservers: []string{"100.100.100.100"}},
		},
		Services: []health.NetworkService{
			{Name: "Wi-Fi", Device: "en0", Enabled: true, Order: 1, DNS: []string{"127.0.0.1"}},
		},
		Tailscale: ts,
		Runtime: &health.MihomoRuntime{
			Version: "v1.19.0", Meta: true, AuthUsed: true,
			Configs: health.MihomoRuntimeConfig{Mode: "rule", MixedPort: 7890, BindAddress: "127.0.0.1", TUN: tun},
			Proxies: map[string]health.MihomoProxy{
				"DIRECT":    {Name: "DIRECT", Type: "Direct"},
				"REJECT":    {Name: "REJECT", Type: "Reject"},
				"node-hk-1": {Name: "node-hk-1", Type: "Shadowsocks"},
				"node-jp-1": {Name: "node-jp-1", Type: "Trojan"},
				"PROXY":     {Name: "PROXY", Type: "Selector", Now: "node-hk-1", All: []string{"node-hk-1", "node-jp-1"}},
				"Google":    {Name: "Google", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "node-jp-1"}},
				"Meta":      {Name: "Meta", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "node-jp-1"}},
			},
			Rules: rules,
		},
		Config: &health.MihomoConfig{
			Path: "/synthetic/mihomo/config.yaml", ExternalController: "127.0.0.1:19894", SecretSet: true,
			DNS: health.MihomoDNSConfig{Enable: true, Listen: "127.0.0.1:53", EnhancedMode: "fake-ip", FakeIPRange: "198.18.0.1/16",
				FakeIPFilter:     []string{"+.ts.net", "+.lan"},
				NameserverPolicy: map[string][]string{"+.ts.net": {"100.100.100.100"}}},
			TUN:   tun,
			Rules: rules,
			ProxyGroups: []health.MihomoProxy{
				{Name: "PROXY", Type: "Selector", All: []string{"node-hk-1", "node-jp-1"}},
				{Name: "Google", Type: "Selector", All: []string{"PROXY", "node-jp-1"}},
				{Name: "Meta", Type: "Selector", All: []string{"PROXY", "node-jp-1"}},
			},
		},
		EgressDirect:  a("198.51.100.7"),
		EgressProxied: a("203.0.113.50"),
		EgressChain:   []string{"node-hk-1", "PROXY"},
	}
}
