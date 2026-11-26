//go:build darwin

package darwin

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/health"
)

// ---- Tailscale ------------------------------------------------------------

type tsPeerJSON struct {
	HostName     string
	DNSName      string
	OS           string
	TailscaleIPs []string
	Online       bool
	Active       bool
	CurAddr      string
	Relay        string
	PeerRelay    string
	ExitNode     bool
	Addrs        []string
	LastSeen     time.Time
}

type tsStatusJSON struct {
	Version        string
	BackendState   string
	TUN            bool
	TailscaleIPs   []string
	MagicDNSSuffix string
	CurrentTailnet *struct {
		MagicDNSSuffix  string
		MagicDNSEnabled bool
	}
	Self   tsPeerJSON
	Peer   map[string]tsPeerJSON
	Health []string
}

func parseAddrs(ss []string) []netip.Addr {
	var out []netip.Addr
	for _, s := range ss {
		if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, a)
		}
	}
	return out
}

func convPeer(p tsPeerJSON) health.TailscalePeer {
	return health.TailscalePeer{
		HostName: p.HostName, DNSName: p.DNSName, OS: p.OS,
		TailscaleIPs: parseAddrs(p.TailscaleIPs), Online: p.Online, Active: p.Active,
		CurAddr: p.CurAddr, Relay: p.Relay, PeerRelay: p.PeerRelay, ExitNode: p.ExitNode,
		Addrs: p.Addrs, LastSeen: p.LastSeen,
	}
}

// ParseTailscaleStatus parses `tailscale status --json`.
func ParseTailscaleStatus(data []byte) (*health.TailscaleStatus, error) {
	var j tsStatusJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	st := &health.TailscaleStatus{
		Version: j.Version, BackendState: j.BackendState, TUN: j.TUN,
		TailscaleIPs: parseAddrs(j.TailscaleIPs), MagicDNSSuffix: j.MagicDNSSuffix,
		Self: convPeer(j.Self), Health: j.Health,
	}
	if j.CurrentTailnet != nil {
		st.MagicDNS = j.CurrentTailnet.MagicDNSEnabled
		if st.MagicDNSSuffix == "" {
			st.MagicDNSSuffix = j.CurrentTailnet.MagicDNSSuffix
		}
	}
	for _, p := range j.Peer {
		cp := convPeer(p)
		st.Peers = append(st.Peers, cp)
		if p.ExitNode {
			c := cp
			st.ExitNode = &c
		}
	}
	// Map iteration order is random; keep reports stable.
	sortPeers(st.Peers)
	return st, nil
}

func sortPeers(ps []health.TailscalePeer) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].DNSName < ps[j].DNSName })
}

// ---- mihomo controller ----------------------------------------------------

type tunJSON struct {
	Enable              bool     `json:"enable"`
	Device              string   `json:"device"`
	Stack               string   `json:"stack"`
	AutoRoute           bool     `json:"auto-route"`
	DNSHijack           []string `json:"dns-hijack"`
	RouteExcludeAddress []string `json:"route-exclude-address"`
	Inet4Address        []string `json:"inet4-address"`
}

func (t tunJSON) conv() health.TUNConfig {
	return health.TUNConfig{Enable: t.Enable, Device: t.Device, Stack: t.Stack, AutoRoute: t.AutoRoute,
		DNSHijack: t.DNSHijack, RouteExcludeAddress: t.RouteExcludeAddress, Inet4Address: t.Inet4Address}
}

// ParseMihomoConfigs parses GET /configs.
func ParseMihomoConfigs(data []byte) (health.MihomoRuntimeConfig, error) {
	var j struct {
		Mode        string  `json:"mode"`
		Port        int     `json:"port"`
		SocksPort   int     `json:"socks-port"`
		MixedPort   int     `json:"mixed-port"`
		AllowLan    bool    `json:"allow-lan"`
		BindAddress string  `json:"bind-address"`
		TUN         tunJSON `json:"tun"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return health.MihomoRuntimeConfig{}, fmt.Errorf("mihomo /configs: %w", err)
	}
	return health.MihomoRuntimeConfig{Mode: strings.ToLower(j.Mode), Port: j.Port, SocksPort: j.SocksPort,
		MixedPort: j.MixedPort, AllowLan: j.AllowLan, BindAddress: j.BindAddress, TUN: j.TUN.conv()}, nil
}

// ParseMihomoProxies parses GET /proxies.
func ParseMihomoProxies(data []byte) (map[string]health.MihomoProxy, error) {
	var j struct {
		Proxies map[string]struct {
			Name string   `json:"name"`
			Type string   `json:"type"`
			Now  string   `json:"now"`
			All  []string `json:"all"`
		} `json:"proxies"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("mihomo /proxies: %w", err)
	}
	out := make(map[string]health.MihomoProxy, len(j.Proxies))
	for k, p := range j.Proxies {
		name := p.Name
		if name == "" {
			name = k
		}
		out[k] = health.MihomoProxy{Name: name, Type: p.Type, Now: p.Now, All: p.All}
	}
	return out, nil
}

// ParseMihomoRules parses GET /rules.
func ParseMihomoRules(data []byte) ([]health.MihomoRule, error) {
	var j struct {
		Rules []struct {
			Index   *int   `json:"index"`
			Type    string `json:"type"`
			Payload string `json:"payload"`
			Proxy   string `json:"proxy"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("mihomo /rules: %w", err)
	}
	out := make([]health.MihomoRule, len(j.Rules))
	for i, r := range j.Rules {
		idx := i
		if r.Index != nil {
			idx = *r.Index
		}
		out[i] = health.MihomoRule{Index: idx, Type: health.NormRuleType(r.Type), Payload: r.Payload, Target: r.Proxy}
	}
	return out, nil
}

// ParseMihomoVersion parses GET /version.
func ParseMihomoVersion(data []byte) (string, bool, error) {
	var j struct {
		Meta    bool   `json:"meta"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return "", false, fmt.Errorf("mihomo /version: %w", err)
	}
	return j.Version, j.Meta, nil
}

// ---- mihomo config file ---------------------------------------------------

// fileConfig lists only the fields checks need. Proxies and providers are
// left out on purpose: they hold credentials.
type fileConfig struct {
	ExternalController string  `yaml:"external-controller"`
	Secret             *string `yaml:"secret"`
	DNS                struct {
		Enable           bool                 `yaml:"enable"`
		Listen           string               `yaml:"listen"`
		EnhancedMode     string               `yaml:"enhanced-mode"`
		FakeIPRange      string               `yaml:"fake-ip-range"`
		FakeIPFilter     []string             `yaml:"fake-ip-filter"`
		NameserverPolicy map[string]yaml.Node `yaml:"nameserver-policy"`
	} `yaml:"dns"`
	TUN struct {
		Enable              bool     `yaml:"enable"`
		Device              string   `yaml:"device"`
		Stack               string   `yaml:"stack"`
		AutoRoute           bool     `yaml:"auto-route"`
		DNSHijack           []string `yaml:"dns-hijack"`
		RouteExcludeAddress []string `yaml:"route-exclude-address"`
		Inet4Address        []string `yaml:"inet4-address"`
	} `yaml:"tun"`
	Rules       []string `yaml:"rules"`
	ProxyGroups []struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
}

// ParseMihomoConfigFile parses a mihomo config file. The returned secret is
// meant only for authenticating to the controller; it is not stored in the
// returned config.
func ParseMihomoConfigFile(path string, data []byte) (*health.MihomoConfig, string, error) {
	var f fileConfig
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	c := &health.MihomoConfig{
		Path:               path,
		ExternalController: f.ExternalController,
		DNS: health.MihomoDNSConfig{
			Enable: f.DNS.Enable, Listen: f.DNS.Listen, EnhancedMode: f.DNS.EnhancedMode,
			FakeIPRange: f.DNS.FakeIPRange, FakeIPFilter: f.DNS.FakeIPFilter,
			NameserverPolicy: map[string][]string{},
		},
		TUN: health.TUNConfig{Enable: f.TUN.Enable, Device: f.TUN.Device, Stack: f.TUN.Stack,
			AutoRoute: f.TUN.AutoRoute, DNSHijack: f.TUN.DNSHijack,
			RouteExcludeAddress: f.TUN.RouteExcludeAddress, Inet4Address: f.TUN.Inet4Address},
	}
	secret := ""
	if f.Secret != nil {
		secret = *f.Secret
	}
	c.SecretSet = secret != ""
	for k, n := range f.DNS.NameserverPolicy {
		var list []string
		switch n.Kind {
		case yaml.SequenceNode:
			_ = n.Decode(&list)
		case yaml.ScalarNode:
			for _, s := range strings.Split(n.Value, ",") {
				if s = strings.TrimSpace(s); s != "" {
					list = append(list, s)
				}
			}
		}
		c.DNS.NameserverPolicy[k] = list
	}
	for i, r := range f.Rules {
		c.Rules = append(c.Rules, ParseRuleLine(i, r))
	}
	for _, g := range f.ProxyGroups {
		c.ProxyGroups = append(c.ProxyGroups, health.MihomoProxy{Name: g.Name, Type: g.Type, All: g.Proxies})
	}
	return c, secret, nil
}

// splitTop splits s on commas outside parentheses.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// ParseRuleLine parses one rule from a config file ("TYPE,payload,target[,opts]").
func ParseRuleLine(i int, line string) health.MihomoRule {
	f := splitTop(line)
	r := health.MihomoRule{Index: i, Type: health.NormRuleType(f[0])}
	switch {
	case r.Type == "MATCH" && len(f) >= 2:
		r.Target = f[1]
	case len(f) >= 3:
		r.Payload, r.Target = f[1], f[2]
	case len(f) == 2:
		r.Payload = f[1]
	}
	return r
}
