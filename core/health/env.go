package health

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	neturl "net/url"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/autostart"
)

// Env gives checks the facts they need. Implementations are read-only.
// Each method may be slow (runs a command or an HTTP request); wrap an Env
// with Memo so one report run fetches each fact once.
type Env interface {
	// Platform returns runtime.GOOS of the inspected machine.
	Platform() string
	// Settings returns user preferences for the checks.
	Settings() Settings

	// RouteGet returns the route the OS would use for dest (an IP).
	RouteGet(ctx context.Context, dest string) (RouteLookup, error)
	// Routes returns the routing table (IPv4 and IPv6).
	Routes(ctx context.Context) ([]Route, error)
	// Interfaces returns network interfaces with their addresses.
	Interfaces(ctx context.Context) ([]Interface, error)
	// Resolvers returns the system DNS resolver configuration.
	Resolvers(ctx context.Context) ([]Resolver, error)
	// NetworkServices returns the network services (macOS) or links,
	// in service order, with their configured DNS servers.
	NetworkServices(ctx context.Context) ([]NetworkService, error)
	// VPNs returns system VPN / NetworkExtension configurations.
	VPNs(ctx context.Context) ([]VPN, error)

	// Tailscale returns the Tailscale status.
	Tailscale(ctx context.Context) (*TailscaleStatus, error)
	// MihomoRuntime returns state read from the mihomo controller.
	MihomoRuntime(ctx context.Context) (*MihomoRuntime, error)
	// MihomoConfig returns the parsed mihomo config file.
	MihomoConfig(ctx context.Context) (*MihomoConfig, error)

	// LookupMihomo queries mihomo's DNS listener for an A record.
	LookupMihomo(ctx context.Context, name string) (DNSAnswer, error)
	// LookupSystem resolves name with the OS resolver.
	LookupSystem(ctx context.Context, name string) (DNSAnswer, error)
}

// ErrDNSTimeout is returned (wrapped) by lookups that got no answer in time.
var ErrDNSTimeout = errors.New("DNS query timed out")

// Settings are user preferences for the checks.
type Settings struct {
	// SensitiveGroups are mihomo proxy groups that must never go DIRECT.
	SensitiveGroups []string `json:"sensitiveGroups,omitempty"`
	// AllowExitNode accepts a Tailscale exit node without failing.
	AllowExitNode bool `json:"allowExitNode,omitempty"`
	// Egress enables the opt-in egress test (leak-guard-egress): one HTTPS
	// request to an IP-echo endpoint directly and one through mihomo.
	Egress bool `json:"egress,omitempty"`
	// EgressURLs are the IP-echo endpoints, tried in order on each path
	// until one answers. Empty: DefaultEgressURLs.
	EgressURLs []string `json:"egressURLs,omitempty"`
	// DirectDNS are the DNS servers the physical network used before
	// Twinnel pointed the system DNS at mihomo (the helper's DNS backup).
	// The direct request resolves a hostname over them first, then over
	// the platform's DHCP servers and gateway (never the system resolver).
	DirectDNS []netip.Addr `json:"-"`
	// Binaries are the programs Twinnel installed or runs, checked by
	// binary-integrity (empty: that check skips).
	Binaries []BinaryRef `json:"binaries,omitempty"`
	// Autostarts reads the competing clients' autostart entries
	// (competitor-autostart; nil: that check skips). The helper sets it.
	Autostarts func(ctx context.Context) ([]autostart.Entry, error) `json:"-"`
	// DNSGuard reads what the helper knows about the system DNS it
	// manages (system-dns; nil: not known here, those parts are left out).
	// The helper sets it where it can set the system DNS.
	DNSGuard func(ctx context.Context) (DNSGuard, error) `json:"-"`
}

// DefaultEgressURLs are the IP-echo endpoints of the egress test, tried in
// order on each path (direct, then through mihomo) until one answers. Each
// answers one GET with the caller's address and needs nothing else (no
// key, no cookie, no personal data). Bodies ParseEgressBody reads:
//
//	https://1.1.1.1/cdn-cgi/trace
//	    Cloudflare trace on an IP literal: no DNS lookup at all. Its
//	    direct path is blocked in mainland China (1.1.1.1 and 1.0.0.1).
//	https://www.cloudflare.com/cdn-cgi/trace
//	    The same trace on Cloudflare's own name, served from other anycast
//	    addresses that answer from mainland China too (slower).
//	https://myip.ipip.net
//	    IPIP.net (mainland China): "当前 IP：<ip>  来自于：…".
//	https://ddns.oray.com/checkip
//	    Oray (mainland China): "Current IP Address: <ip>".
//
// The mainland ones come last: through mihomo the usual rules send them
// DIRECT, so they only stand in when the Cloudflare ones cannot be
// reached. Users and the CLI can replace the list (Settings.EgressURLs,
// `twinnel doctor --egress-url`).
var DefaultEgressURLs = []string{
	"https://1.1.1.1/cdn-cgi/trace",
	"https://www.cloudflare.com/cdn-cgi/trace",
	"https://myip.ipip.net",
	"https://ddns.oray.com/checkip",
}

// MaxEgressURLs bounds a configured endpoint list (each path tries them
// within its time budget anyway).
const MaxEgressURLs = 8

// CheckEgressURLs reports what is wrong with a configured endpoint list:
// each must be an https URL with a host, without credentials.
func CheckEgressURLs(urls []string) error {
	if len(urls) > MaxEgressURLs {
		return fmt.Errorf("at most %d egress URLs", MaxEgressURLs)
	}
	for _, s := range urls {
		u, err := neturl.Parse(s)
		switch {
		case err != nil || len(s) > 512:
			return fmt.Errorf("invalid egress URL %q", s)
		case u.Scheme != "https" || u.Hostname() == "":
			return fmt.Errorf("egress URL %q: must be https://host/…", s)
		case u.User != nil:
			return fmt.Errorf("egress URL %q: must not carry credentials", s)
		}
	}
	return nil
}

// EgressEndpoints is s.EgressURLs, or DefaultEgressURLs when empty.
func (s Settings) EgressEndpoints() []string {
	if len(s.EgressURLs) > 0 {
		return s.EgressURLs
	}
	return DefaultEgressURLs
}

// EgressEnv is implemented by Envs that can run the egress test. Both
// methods make one HTTPS GET to url and return the client IP it reports.
type EgressEnv interface {
	// DirectEgress bypasses mihomo (socket bound to the physical interface).
	DirectEgress(ctx context.Context, url string) (netip.Addr, error)
	// ProxiedEgress goes through mihomo's mixed/HTTP port and also returns
	// the proxy chain mihomo used for it (from GET /connections), outermost
	// group last, e.g. ["node-a", "PROXY", "Final"].
	ProxiedEgress(ctx context.Context, url string) (netip.Addr, []string, error)
}

// ErrNotSupported is returned by optional Env features an Env lacks.
var ErrNotSupported = errors.New("not supported on this platform")

// DefaultSettings returns the defaults used when the user set nothing.
func DefaultSettings() Settings {
	return Settings{SensitiveGroups: []string{"Google", "Meta"}}
}

// ApplyUserChoices puts what the user chose in Twinnel (the helper's
// settings) into s: leakGuard are the groups 防泄漏 guards (empty keeps
// s's, the defaults), egress is 定期检测 (the egress test runs with every
// run). The helper, and the CLI's doctor and MCP doctor from the helper's
// settings.get, build check settings through it, so leak-guard checks the
// same groups everywhere.
func (s *Settings) ApplyUserChoices(leakGuard []string, egress bool) {
	if len(leakGuard) > 0 {
		s.SensitiveGroups = append([]string(nil), leakGuard...)
	}
	s.Egress = egress
}

// RouteLookup is the answer of a single route lookup.
type RouteLookup struct {
	Destination string
	Gateway     string
	Interface   string
	Flags       []string
}

// Route is one routing table entry.
type Route struct {
	Family      int // 4 or 6
	Destination string
	Gateway     string
	Flags       string
	Interface   string
}

// Interface is a network interface.
type Interface struct {
	Name   string
	Up     bool
	Active bool // link has carrier ("status: active" on macOS) or point-to-point up
	Addrs  []netip.Addr
}

// Resolver is one entry of the system resolver configuration.
type Resolver struct {
	Domain        string // empty for the default resolver
	Nameservers   []string
	SearchDomains []string
	Options       string
	IfIndex       string
	Scoped        bool
}

// NetworkService is a configured network service (macOS) / link.
type NetworkService struct {
	Name    string
	Device  string // e.g. en0; empty for VPN services
	Enabled bool
	Order   int      // 1-based service order
	DNS     []string // manually configured DNS servers; empty means DHCP
	// DNSSource tells where DNS came from when the platform knows better
	// than "set means manual" (Linux: NetworkManager's ipv4.dns and
	// ipv4.ignore-auto-dns). Empty keeps the macOS meaning of DNS.
	DNSSource string
}

// NetworkService.DNSSource values.
const (
	DNSSourceManual  = "manual"  // static servers only
	DNSSourceAuto    = "auto"    // DHCP / router advertisement
	DNSSourceMixed   = "mixed"   // static servers plus the DHCP ones
	DNSSourceUnknown = "unknown" // per-link servers, origin not known (resolved without NetworkManager)
	DNSSourceGlobal  = "global"  // no per-link servers: resolv.conf / the global resolver
)

// VPN is a system VPN or NetworkExtension configuration.
type VPN struct {
	Name     string
	Kind     string // e.g. "VPN (com.example.app)"
	BundleID string
	Status   string // Connected, Disconnected, Connecting, ...
	Enabled  bool
}

// Connected reports whether the VPN currently carries traffic.
func (v VPN) Connected() bool {
	switch v.Status {
	case "Connected", "Connecting", "Reasserting":
		return true
	}
	return false
}

// TailscaleStatus is the subset of `tailscale status --json` checks need.
type TailscaleStatus struct {
	Version        string
	BackendState   string
	TUN            bool
	TailscaleIPs   []netip.Addr
	MagicDNSSuffix string
	MagicDNS       bool
	Self           TailscalePeer
	Peers          []TailscalePeer
	ExitNode       *TailscalePeer // nil when no exit node is used
	Health         []string
}

// TailscalePeer is one node of the tailnet.
type TailscalePeer struct {
	HostName     string
	DNSName      string // FQDN with trailing dot
	OS           string
	TailscaleIPs []netip.Addr
	Online       bool
	Active       bool
	CurAddr      string // direct endpoint; empty if relayed or idle
	Relay        string // home DERP region
	PeerRelay    string
	ExitNode     bool
	Addrs        []string
	LastSeen     time.Time
}

// ShortName returns the first label of the MagicDNS name.
func (p TailscalePeer) ShortName() string {
	n := p.DNSName
	for i := 0; i < len(n); i++ {
		if n[i] == '.' {
			return n[:i]
		}
	}
	return n
}

// MihomoRuntime is state read from the mihomo controller.
type MihomoRuntime struct {
	Version  string
	Meta     bool
	Configs  MihomoRuntimeConfig
	Proxies  map[string]MihomoProxy
	Rules    []MihomoRule
	AuthUsed bool // a secret was sent with the requests
}

// MihomoRuntimeConfig is the subset of GET /configs checks need.
type MihomoRuntimeConfig struct {
	Mode        string
	MixedPort   int
	Port        int
	SocksPort   int
	AllowLan    bool
	BindAddress string
	TUN         TUNConfig
}

// TUNConfig is the TUN part of a mihomo config (runtime or file).
type TUNConfig struct {
	Enable              bool
	Device              string
	Stack               string
	AutoRoute           bool
	DNSHijack           []string
	RouteExcludeAddress []string
	Inet4Address        []string
	AutoDetectInterface bool
}

// MihomoProxy is a proxy or group from GET /proxies.
type MihomoProxy struct {
	Name string
	Type string   // Selector, URLTest, Fallback, LoadBalance, Direct, Reject, Shadowsocks, ...
	Now  string   // selected member, groups only
	All  []string // members, groups only
}

// IsGroup reports whether p is a proxy group.
func (p MihomoProxy) IsGroup() bool { return p.All != nil }

// MihomoRule is one rule. Type is normalized by NormRuleType.
type MihomoRule struct {
	Index   int
	Type    string
	Payload string
	Target  string
}

// MihomoConfig is the subset of the mihomo config file checks need. It
// deliberately does not parse proxies or providers (they hold credentials).
type MihomoConfig struct {
	Path               string
	ExternalController string
	SecretSet          bool // the secret value itself is never kept
	DNS                MihomoDNSConfig
	TUN                TUNConfig
	// InterfaceName is the top-level interface-name (outbound binding).
	InterfaceName string
	// HostNames are the keys of the top-level hosts map (values not kept).
	HostNames   []string
	Rules       []MihomoRule
	ProxyGroups []MihomoProxy
}

// DNS modes besides mihomo's own enhanced-mode values (fake-ip,
// redir-host).
const (
	DNSModeOff    = "off"    // dns.enable is false
	DNSModeNormal = "normal" // dns on, no enhanced mode
)

// Mode is the DNS mode d sets: its enhanced-mode in lower case (fake-ip,
// redir-host), DNSModeNormal with none, DNSModeOff while dns is off.
func (d MihomoDNSConfig) Mode() string {
	if !d.Enable {
		return DNSModeOff
	}
	if m := strings.ToLower(strings.TrimSpace(d.EnhancedMode)); m != "" {
		return m
	}
	return DNSModeNormal
}

// MihomoDNSConfig is the DNS part of the mihomo config file.
type MihomoDNSConfig struct {
	Enable           bool
	Listen           string
	EnhancedMode     string
	FakeIPRange      string
	FakeIPFilter     []string
	FakeIPFilterMode string // "" (= blacklist), "blacklist", "whitelist", "rule"
	UseHosts         *bool  // nil when unset (mihomo default: true)
	NameserverPolicy map[string][]string
}

// DNSAnswer is the result of a DNS lookup.
type DNSAnswer struct {
	Name  string
	Rcode string // NOERROR, NXDOMAIN, SERVFAIL, ...
	Addrs []netip.Addr
}
