// Package tailscale reads and changes the local Tailscale node through its
// LocalAPI (tailscale.com/client/local), whatever installed it: the
// open-source tailscaled (brew, static binary, package manager) or the macOS
// app. It turns ipnstate.Status into the app's own State model.
//
// Reads are always allowed. Write methods (up/down, exit node, prefs) return
// ErrWritesDisabled unless the client was built with AllowWrites.
package tailscale

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

// ErrWritesDisabled is returned by write methods of a read-only client.
var ErrWritesDisabled = errors.New("tailscale: write operations are disabled")

// API is the part of the LocalAPI this package uses; *local.Client
// provides it through LocalAPI, tests provide fakes.
type API interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (Watcher, error)
}

// Watcher yields IPN bus notifications.
type Watcher interface {
	Next() (ipn.Notify, error)
	Close() error
}

// LocalAPI adapts a *local.Client (nil: the default socket) to API.
func LocalAPI(lc *local.Client) API {
	if lc == nil {
		lc = &local.Client{}
	}
	return localAPI{lc}
}

type localAPI struct{ lc *local.Client }

func (l localAPI) Status(ctx context.Context) (*ipnstate.Status, error) { return l.lc.Status(ctx) }
func (l localAPI) GetPrefs(ctx context.Context) (*ipn.Prefs, error)     { return l.lc.GetPrefs(ctx) }
func (l localAPI) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	return l.lc.EditPrefs(ctx, mp)
}
func (l localAPI) StartLoginInteractive(ctx context.Context) error {
	return l.lc.StartLoginInteractive(ctx)
}
func (l localAPI) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (Watcher, error) {
	return l.lc.WatchIPNBus(ctx, mask)
}

// Client reads the node's state and, when allowed, changes it.
type Client struct {
	api         API
	allowWrites bool
	now         func() time.Time
}

// New returns a client over api.
func New(api API, allowWrites bool) *Client {
	return &Client{api: api, allowWrites: allowWrites, now: time.Now}
}

// WritesAllowed reports whether write methods are enabled.
func (c *Client) WritesAllowed() bool { return c.allowWrites }

// Peer is one node of the tailnet, or the local node.
type Peer struct {
	ID       string // stable node ID
	Name     string // short name: first label of DNSName, else HostName
	DNSName  string // full MagicDNS name without the trailing dot
	HostName string
	OS       string
	IPs      []netip.Addr
	Online   bool
	Active   bool // traffic was exchanged recently
	// Direct is true when the last path was a direct UDP path (CurAddr).
	// Otherwise an active peer goes through the DERP relay Relay, or
	// through a peer relay (PeerRelay).
	Direct           bool
	CurAddr          string
	Relay            string // DERP region code, e.g. "tok"
	PeerRelay        string
	LastSeen         time.Time // only set by control when offline
	LastHandshake    time.Time
	RxBytes, TxBytes int64
	ExitNode         bool // currently used as exit node
	ExitNodeOption   bool // may be used as exit node
	KeyExpiry        *time.Time
	Expired          bool
	Tags             []string
}

// IPv4 returns the first IPv4 address, or the zero Addr.
func (p Peer) IPv4() netip.Addr {
	for _, a := range p.IPs {
		if a.Is4() {
			return a
		}
	}
	return netip.Addr{}
}

// Path describes how traffic reaches the peer: "direct", "derp", "peer-relay"
// or "idle" when no path is active.
func (p Peer) Path() string {
	switch {
	case p.Direct:
		return "direct"
	case p.PeerRelay != "":
		return "peer-relay"
	case p.Active && p.Relay != "":
		return "derp"
	}
	return "idle"
}

// Prefs is the subset of ipn.Prefs the app shows.
type Prefs struct {
	WantRunning      bool
	LoggedOut        bool
	MagicDNS         bool // CorpDNS: use the tailnet's DNS settings
	ShieldsUp        bool
	RunSSH           bool
	RouteAll         bool // accept subnet routes
	ExitNodeID       string
	ExitNodeIP       netip.Addr
	ExitNodeAllowLAN bool
	Hostname         string
	ControlURL       string
}

// State is one snapshot of the local node and its tailnet.
type State struct {
	BackendState   string // NoState, NeedsLogin, NeedsMachineAuth, Stopped, Starting, Running
	Version        string
	TUN            bool
	AuthURL        string
	Health         []string
	User           string // login name of the node's owner
	Tailnet        string
	MagicDNSSuffix string
	Self           Peer
	Peers          []Peer // online first, then by name
	ExitNode       *Peer  // the exit node in use, if any
	ExitNodeOnline bool
	Prefs          Prefs
	KeyExpiry      *time.Time
	Fetched        time.Time
}

// Running reports whether the node is up.
func (s State) Running() bool { return s.BackendState == ipn.Running.String() }

// OnlinePeers counts online peers.
func (s State) OnlinePeers() int {
	n := 0
	for _, p := range s.Peers {
		if p.Online {
			n++
		}
	}
	return n
}

// KeyExpiresWithin reports whether the node key expires within d of now.
func (s State) KeyExpiresWithin(now time.Time, d time.Duration) bool {
	return s.KeyExpiry != nil && s.KeyExpiry.Sub(now) < d
}

// State reads the current state: status with peers, and prefs.
func (c *Client) State(ctx context.Context) (State, error) {
	st, err := c.api.Status(ctx)
	if err != nil {
		return State{}, err
	}
	prefs, err := c.api.GetPrefs(ctx)
	if err != nil {
		return State{}, err
	}
	s := FromStatus(st, prefs)
	s.Fetched = c.now()
	return s, nil
}

// FromStatus converts LocalAPI answers to a State. prefs may be nil.
func FromStatus(st *ipnstate.Status, prefs *ipn.Prefs) State {
	s := State{
		BackendState:   st.BackendState,
		Version:        st.Version,
		TUN:            st.TUN,
		AuthURL:        st.AuthURL,
		Health:         append([]string(nil), st.Health...),
		MagicDNSSuffix: st.MagicDNSSuffix,
	}
	if st.CurrentTailnet != nil {
		s.Tailnet = st.CurrentTailnet.Name
		if s.MagicDNSSuffix == "" {
			s.MagicDNSSuffix = st.CurrentTailnet.MagicDNSSuffix
		}
	}
	if st.Self != nil {
		s.Self = peerFrom(st.Self)
		s.KeyExpiry = st.Self.KeyExpiry
		if u, ok := st.User[st.Self.UserID]; ok {
			s.User = u.LoginName
		}
	}
	for _, ps := range st.Peer {
		if ps == nil || ps.ShareeNode {
			continue
		}
		s.Peers = append(s.Peers, peerFrom(ps))
	}
	sort.Slice(s.Peers, func(i, j int) bool {
		a, b := s.Peers[i], s.Peers[j]
		if a.Online != b.Online {
			return a.Online
		}
		return a.Name < b.Name
	})
	// An exit node is in use only while the node runs: logged out or
	// stopped, the preference is kept but nothing goes through it, so it
	// is not reported (it would read as a live conflict).
	if en := st.ExitNodeStatus; en != nil && st.BackendState == "Running" {
		s.ExitNodeOnline = en.Online
		for i := range s.Peers {
			if s.Peers[i].ID == string(en.ID) {
				p := s.Peers[i]
				s.ExitNode = &p
			}
		}
		if s.ExitNode == nil {
			s.ExitNode = &Peer{ID: string(en.ID), Name: string(en.ID), Online: en.Online}
		}
	}
	if prefs != nil {
		s.Prefs = Prefs{
			WantRunning:      prefs.WantRunning,
			LoggedOut:        prefs.LoggedOut,
			MagicDNS:         prefs.CorpDNS,
			ShieldsUp:        prefs.ShieldsUp,
			RunSSH:           prefs.RunSSH,
			RouteAll:         prefs.RouteAll,
			ExitNodeID:       string(prefs.ExitNodeID),
			ExitNodeIP:       prefs.ExitNodeIP,
			ExitNodeAllowLAN: prefs.ExitNodeAllowLANAccess,
			Hostname:         prefs.Hostname,
			ControlURL:       prefs.ControlURL,
		}
	}
	return s
}

func peerFrom(ps *ipnstate.PeerStatus) Peer {
	dns := strings.TrimSuffix(ps.DNSName, ".")
	name := ps.HostName
	if dns != "" {
		name, _, _ = strings.Cut(dns, ".")
	}
	p := Peer{
		ID:             string(ps.ID),
		Name:           name,
		DNSName:        dns,
		HostName:       ps.HostName,
		OS:             ps.OS,
		IPs:            append([]netip.Addr(nil), ps.TailscaleIPs...),
		Online:         ps.Online,
		Active:         ps.Active,
		Direct:         ps.CurAddr != "",
		CurAddr:        ps.CurAddr,
		Relay:          ps.Relay,
		PeerRelay:      ps.PeerRelay,
		LastSeen:       ps.LastSeen,
		LastHandshake:  ps.LastHandshake,
		RxBytes:        ps.RxBytes,
		TxBytes:        ps.TxBytes,
		ExitNode:       ps.ExitNode,
		ExitNodeOption: ps.ExitNodeOption,
		KeyExpiry:      ps.KeyExpiry,
		Expired:        ps.Expired,
	}
	if ps.Tags != nil {
		p.Tags = ps.Tags.AsSlice()
	}
	return p
}

// Watch calls onChange for every IPN bus notification that may change the
// State (backend state, prefs, netmap, health, login), until ctx ends or the
// bus breaks. The first call comes at once with the initial state. Callers
// should re-read State, rate-limited: notifications can be frequent.
func (c *Client) Watch(ctx context.Context, onChange func()) error {
	w, err := c.api.WatchIPNBus(ctx, ipn.NotifyInitialState|ipn.NotifyInitialPrefs)
	if err != nil {
		return err
	}
	defer w.Close()
	for {
		n, err := w.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if relevant(n) {
			onChange()
		}
	}
}

func relevant(n ipn.Notify) bool {
	return n.State != nil || n.Prefs != nil || n.LoginFinished != nil ||
		n.ErrMessage != nil || n.BrowseToURL != nil || n.Health != nil ||
		n.SelfChange != nil || n.InitialStatus != nil ||
		len(n.PeerChangedPatch) > 0 || len(n.PeersChanged) > 0 || len(n.PeersRemoved) > 0
}

// --- write operations: tested only against fakes ---

func (c *Client) edit(ctx context.Context, mp *ipn.MaskedPrefs) error {
	if !c.allowWrites {
		return ErrWritesDisabled
	}
	_, err := c.api.EditPrefs(ctx, mp)
	return err
}

// Up connects the node (as `tailscale up` with the current prefs).
func (c *Client) Up(ctx context.Context) error {
	return c.edit(ctx, &ipn.MaskedPrefs{Prefs: ipn.Prefs{WantRunning: true}, WantRunningSet: true})
}

// Down disconnects the node (as `tailscale down`).
func (c *Client) Down(ctx context.Context) error {
	return c.edit(ctx, &ipn.MaskedPrefs{Prefs: ipn.Prefs{WantRunning: false}, WantRunningSet: true})
}

// loginer is implemented by APIs that can start an interactive login
// (localAPI); fakes need not.
type loginer interface {
	StartLoginInteractive(ctx context.Context) error
}

// ErrNoLogin is returned by Login when the API cannot start a login.
var ErrNoLogin = errors.New("tailscale: login is not supported here")

// Login starts an interactive login (as `tailscale login`): the node then
// reports the URL to open (State.AuthURL).
func (c *Client) Login(ctx context.Context) error {
	if !c.allowWrites {
		return ErrWritesDisabled
	}
	l, ok := c.api.(loginer)
	if !ok {
		return ErrNoLogin
	}
	return l.StartLoginInteractive(ctx)
}

// SetExitNode uses the peer with the stable ID id as exit node; "" stops
// using one.
func (c *Client) SetExitNode(ctx context.Context, id string) error {
	return c.edit(ctx, &ipn.MaskedPrefs{
		Prefs:         ipn.Prefs{ExitNodeID: tailcfg.StableNodeID(id)},
		ExitNodeIDSet: true,
		ExitNodeIPSet: true, // clears an exit node chosen by IP
	})
}

// SetMagicDNS turns use of the tailnet's DNS settings on or off.
func (c *Client) SetMagicDNS(ctx context.Context, on bool) error {
	return c.edit(ctx, &ipn.MaskedPrefs{Prefs: ipn.Prefs{CorpDNS: on}, CorpDNSSet: true})
}

// SetShieldsUp blocks or allows incoming connections.
func (c *Client) SetShieldsUp(ctx context.Context, on bool) error {
	return c.edit(ctx, &ipn.MaskedPrefs{Prefs: ipn.Prefs{ShieldsUp: on}, ShieldsUpSet: true})
}
