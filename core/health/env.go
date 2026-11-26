package health

import (
	"context"
	"errors"
	"net/netip"
	"time"
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
	// PreferredPeers are Tailscale peers (host name, MagicDNS short name or
	// IP) expected to connect directly. Empty means all online peers.
	PreferredPeers []string `json:"preferredPeers,omitempty"`
	// SensitiveGroups are mihomo proxy groups that must never go DIRECT.
	SensitiveGroups []string `json:"sensitiveGroups,omitempty"`
	// AllowExitNode accepts a Tailscale exit node without failing.
	AllowExitNode bool `json:"allowExitNode,omitempty"`
}

// DefaultSettings returns the defaults used when the user set nothing.
func DefaultSettings() Settings {
	return Settings{SensitiveGroups: []string{"Google", "Meta"}}
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
}

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
	Rules              []MihomoRule
	ProxyGroups        []MihomoProxy
}

// MihomoDNSConfig is the DNS part of the mihomo config file.
type MihomoDNSConfig struct {
	Enable           bool
	Listen           string
	EnhancedMode     string
	FakeIPRange      string
	FakeIPFilter     []string
	NameserverPolicy map[string][]string
}

// DNSAnswer is the result of a DNS lookup.
type DNSAnswer struct {
	Name  string
	Rcode string // NOERROR, NXDOMAIN, SERVFAIL, ...
	Addrs []netip.Addr
}
