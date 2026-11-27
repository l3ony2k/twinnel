// Package fixture is a synthetic health.Env: every fact comes from a
// Machine value (built in, or loaded from a JSON file), never from the
// computer it runs on. Screenshot runs and tests use it so no real route,
// resolver, VPN, tailnet or mihomo rule ends up in a picture.
package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/l3ony2k/duet/core/health"
)

// Machine is everything a health run reads. Zero fields read as empty
// (no routes, no VPNs, ...); a nil Tailscale, Runtime or Config is an
// error, as an absent tailscaled or controller would be.
type Machine struct {
	Platform   string                        `json:"platform,omitempty"` // "" = darwin
	Routes     map[string]health.RouteLookup `json:"routes,omitempty"`   // route lookup by destination
	Table      []health.Route                `json:"table,omitempty"`
	Interfaces []health.Interface            `json:"interfaces,omitempty"`
	Resolvers  []health.Resolver             `json:"resolvers,omitempty"`
	Services   []health.NetworkService       `json:"services,omitempty"`
	VPNs       []health.VPN                  `json:"vpns,omitempty"`
	Tailscale  *health.TailscaleStatus       `json:"tailscale,omitempty"`
	Runtime    *health.MihomoRuntime         `json:"runtime,omitempty"`
	Config     *health.MihomoConfig          `json:"config,omitempty"`
	MihomoDNS  map[string]health.DNSAnswer   `json:"mihomoDNS,omitempty"`
	SystemDNS  map[string]health.DNSAnswer   `json:"systemDNS,omitempty"`

	// The opt-in egress test's answers.
	EgressDirect  netip.Addr `json:"egressDirect,omitempty"`
	EgressProxied netip.Addr `json:"egressProxied,omitempty"`
	EgressChain   []string   `json:"egressChain,omitempty"`

	// TailnetIface and TunIface answer route lookups not in Routes:
	// tailnet addresses go to TailnetIface, everything else to TunIface.
	TailnetIface string `json:"tailnetIface,omitempty"`
	TunIface     string `json:"tunIface,omitempty"`
}

// Env serves a Machine as a health.Env (and health.EgressEnv).
type Env struct {
	M     Machine
	Prefs health.Settings
	// TailscaleFn, when set, replaces M.Tailscale (e.g. a parsed
	// DUET_TAILSCALE_STATUS_FILE, so the checks and the pages agree).
	// Lookups and routes of tailnet names then follow it.
	TailscaleFn func(ctx context.Context) (*health.TailscaleStatus, error)
}

var (
	_ health.Env       = (*Env)(nil)
	_ health.EgressEnv = (*Env)(nil)
)

// Load reads a Machine from a JSON file.
func Load(path string) (Machine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Machine{}, err
	}
	var m Machine
	if err := json.Unmarshal(b, &m); err != nil {
		return Machine{}, fmt.Errorf("health fixture %s: %w", path, err)
	}
	return m, nil
}

func (e *Env) Platform() string {
	if e.M.Platform == "" {
		return health.Darwin
	}
	return e.M.Platform
}

func (e *Env) Settings() health.Settings { return e.Prefs }

var tailnet4, tailnet6 = netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func (e *Env) RouteGet(_ context.Context, dest string) (health.RouteLookup, error) {
	if r, ok := e.M.Routes[dest]; ok {
		return r, nil
	}
	a, err := netip.ParseAddr(dest)
	if err != nil {
		return health.RouteLookup{}, fmt.Errorf("route get %s: not an address", dest)
	}
	if (tailnet4.Contains(a) || tailnet6.Contains(a)) && e.M.TailnetIface != "" {
		return health.RouteLookup{Destination: dest, Interface: e.M.TailnetIface}, nil
	}
	if e.M.TunIface != "" {
		return health.RouteLookup{Destination: dest, Interface: e.M.TunIface}, nil
	}
	return health.RouteLookup{}, fmt.Errorf("route get %s: no route", dest)
}

func (e *Env) Routes(context.Context) ([]health.Route, error)         { return e.M.Table, nil }
func (e *Env) Interfaces(context.Context) ([]health.Interface, error) { return e.M.Interfaces, nil }
func (e *Env) Resolvers(context.Context) ([]health.Resolver, error)   { return e.M.Resolvers, nil }
func (e *Env) NetworkServices(context.Context) ([]health.NetworkService, error) {
	return e.M.Services, nil
}
func (e *Env) VPNs(context.Context) ([]health.VPN, error) { return e.M.VPNs, nil }

func (e *Env) Tailscale(ctx context.Context) (*health.TailscaleStatus, error) {
	if e.TailscaleFn != nil {
		return e.TailscaleFn(ctx)
	}
	if e.M.Tailscale == nil {
		return nil, errors.New("fixture: tailscale not running")
	}
	return e.M.Tailscale, nil
}

func (e *Env) MihomoRuntime(context.Context) (*health.MihomoRuntime, error) {
	if e.M.Runtime == nil {
		return nil, errors.New("fixture: mihomo controller not reachable")
	}
	return e.M.Runtime, nil
}

func (e *Env) MihomoConfig(context.Context) (*health.MihomoConfig, error) {
	if e.M.Config == nil {
		return nil, errors.New("fixture: no mihomo config")
	}
	return e.M.Config, nil
}

func (e *Env) LookupMihomo(ctx context.Context, name string) (health.DNSAnswer, error) {
	if r, ok := e.M.MihomoDNS[name]; ok {
		return r, nil
	}
	if r, ok := e.tailnetName(ctx, name); ok {
		return r, nil
	}
	return health.DNSAnswer{}, health.ErrDNSTimeout
}

func (e *Env) LookupSystem(ctx context.Context, name string) (health.DNSAnswer, error) {
	if r, ok := e.M.SystemDNS[name]; ok {
		return r, nil
	}
	if r, ok := e.tailnetName(ctx, name); ok {
		return r, nil
	}
	return health.DNSAnswer{Name: name, Rcode: "NXDOMAIN"}, nil
}

// tailnetName answers a tailnet node's short or MagicDNS name with its
// IPv4 address, as MagicDNS would.
func (e *Env) tailnetName(ctx context.Context, name string) (health.DNSAnswer, bool) {
	ts, err := e.Tailscale(ctx)
	if err != nil || ts == nil {
		return health.DNSAnswer{}, false
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	for _, p := range append([]health.TailscalePeer{ts.Self}, ts.Peers...) {
		fq := strings.TrimSuffix(strings.ToLower(p.DNSName), ".")
		if fq == "" || (name != fq && name != p.ShortName()) {
			continue
		}
		ips := p.TailscaleIPs
		if p.DNSName == ts.Self.DNSName && len(ips) == 0 {
			ips = ts.TailscaleIPs
		}
		var v4 []netip.Addr
		for _, a := range ips {
			if a.Is4() {
				v4 = append(v4, a)
			}
		}
		return health.DNSAnswer{Name: name, Rcode: "NOERROR", Addrs: v4}, true
	}
	return health.DNSAnswer{}, false
}

func (e *Env) DirectEgress(context.Context, string) (netip.Addr, error) {
	if !e.M.EgressDirect.IsValid() {
		return netip.Addr{}, errors.New("fixture: no direct egress")
	}
	return e.M.EgressDirect, nil
}

func (e *Env) ProxiedEgress(context.Context, string) (netip.Addr, []string, error) {
	if !e.M.EgressProxied.IsValid() {
		return netip.Addr{}, nil, errors.New("fixture: no proxied egress")
	}
	return e.M.EgressProxied, e.M.EgressChain, nil
}
