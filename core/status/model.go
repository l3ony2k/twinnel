// Package status combines mihomo and Tailscale into one snapshot that the
// UI renders, keeps it fresh (polling plus event streams), and rates the
// overall state green / yellow / red.
package status

import (
	"fmt"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
)

// Level is the overall status color.
type Level int

const (
	LevelUnknown Level = iota // still loading
	LevelOK                   // green
	LevelWarn                 // yellow
	LevelError                // red
)

// Text returns the label of the level.
func (l Level) Text() string {
	switch l {
	case LevelOK:
		return i18n.Cur().StatusOK
	case LevelWarn:
		return i18n.Cur().StatusWarn
	case LevelError:
		return i18n.Cur().StatusError
	}
	return i18n.Cur().StatusUnknown
}

// TrafficSample is one /traffic sample with its time.
type TrafficSample struct {
	mihomo.Traffic
	Time time.Time
}

// Member is one member of a group.
type Member struct {
	Name    string
	Type    string
	IsGroup bool
	Alive   bool
	Delay   int  // ms of the last test, 0 = failed
	Tested  bool // there was a test
	// Builtin: the member (or, for a nested group, the node it resolves
	// to) is DIRECT, REJECT or another built-in. It is never tested and
	// shows no delay.
	Builtin bool
}

// DelayText is the member's delay label: "—" for a built-in.
func (m Member) DelayText() string { return delayLabel(m.Delay, m.Tested, m.Builtin) }

// Group is a proxy group with its members resolved.
type Group struct {
	Name    string
	Type    string
	Now     string
	Chain   []string // Now, then what nested groups selected, to the node
	Node    string   // last of Chain: the node carrying the traffic
	Delay   int      // delay of Node
	Tested  bool
	Builtin bool // Node is DIRECT, REJECT or another built-in (no delay)
	Members []Member
	Hidden  bool
	TestURL string
}

// DelayText is the group's delay label (its node's): "—" for a built-in.
func (g Group) DelayText() string { return delayLabel(g.Delay, g.Tested, g.Builtin) }

func delayLabel(ms int, tested, builtin bool) string {
	if builtin {
		return NoDelay
	}
	return DelayText(ms, tested)
}

// NoDelay is the delay label of a built-in (DIRECT, REJECT, ...).
const NoDelay = "—"

// Selectable reports whether a user may choose a member (Selector groups).
func (g Group) Selectable() bool { return g.Type == "Selector" }

// Mihomo is mihomo's part of a snapshot.
type Mihomo struct {
	Reachable    bool
	Unauthorized bool
	Err          string
	Version      string
	Config       mihomo.Config
	Groups       []Group
	Primary      *Group // the configured primary group, or the first Selector
	Traffic      TrafficSample
	Connections  int
	// Memory is what mihomo says it uses (the memory field of
	// /connections, Go's in-use heap); 0 when it does not say.
	Memory int64
	// Process is mihomo's process as the system lists it (resident
	// memory, CPU, start); nil when not found or not read.
	Process *Process
	// DNSMode is the config file's DNS mode: fake-ip, redir-host,
	// DNSModeNormal, DNSModeOff; "" when unknown.
	DNSMode string
	Updated time.Time
}

// Tailscale is Tailscale's part of a snapshot.
type Tailscale struct {
	Reachable bool
	Err       string
	Install   tailscale.Install
	State     tailscale.State
	// Process is Tailscale's daemon as the system lists it; nil when not
	// found or not read (the app builds run it as a system extension).
	Process *Process
	Updated time.Time
}

// Snapshot is everything the UI shows at one time.
type Snapshot struct {
	Time          time.Time
	Mihomo        Mihomo
	Tailscale     Tailscale
	Health        Health
	Helper        Helper
	Level         Level
	Reasons       []Reason
	WritesAllowed bool // off only in development and test runs (DUET_READ_ONLY=1)
	Loaded        bool // both sides answered or failed at least once
	// AcceptedExitNode is the exit node the user accepted using while
	// mihomo's TUN is on (Config.ExitNodeAccepted).
	AcceptedExitNode string
}

// Reason explains a level other than OK.
type Reason struct {
	Level  Level
	Text   string
	Health bool // comes from the health checks (the panel shows those on a line of their own)
	Code   string
	Count  int // ReasonHealth: the number the text says
	// Raw is the error behind a reason as the system said it (English),
	// for a secondary line and 复制; Text says it in the UI language.
	Raw string
}

// Reason codes.
const (
	ReasonMihomoAuth     = "mihomo-auth"
	ReasonMihomoDown     = "mihomo-down"
	ReasonTunOff         = "tun-off"
	ReasonPrimaryDead    = "primary-dead"
	ReasonTSNotInstalled = "ts-not-installed"
	ReasonTSDown         = "ts-down"
	ReasonTSNeedsLogin   = "ts-needs-login"
	ReasonTSStopped      = "ts-stopped"
	ReasonTSExitNode     = "exit-node"
	ReasonTSKeyExpiry    = "key-expiry"
	ReasonTSHealth       = "ts-health"
	ReasonTSSystemVPN    = "system-vpn"
	ReasonHealth         = "health"
)

// Can is what the app can change right now, per thing a control acts
// on: a control whose target can't act is disabled.
type Can struct {
	Mihomo    bool // the controller answers and takes the secret: mode, node, TUN, reload
	Tailscale bool // the LocalAPI answers: connect, exit node
	Helper    bool // the helper is connected: config, services, kernel
}

// Can returns what the controls can change in s.
func (s Snapshot) Can() Can {
	w := s.WritesAllowed
	return Can{
		Mihomo:    w && s.Mihomo.Reachable && !s.Mihomo.Unauthorized,
		Tailscale: w && s.Tailscale.Reachable,
		Helper:    w && s.Helper.Connected(),
	}
}

// ExitNodeAccepted reports whether Tailscale uses an exit node the user
// accepted (AcceptedExitNode), and its name.
func (s Snapshot) ExitNodeAccepted() (string, bool) {
	ts := s.Tailscale
	if !ts.Reachable || ts.State.ExitNode == nil {
		return "", false
	}
	n := ts.State.ExitNode.Name
	return n, s.AcceptedExitNode != "" && n == s.AcceptedExitNode
}

// acceptedCauses are the health causes the user accepted in s.
func (s Snapshot) acceptedCauses() []string {
	if _, ok := s.ExitNodeAccepted(); ok {
		return []string{health.CauseExitNode}
	}
	return nil
}

// TakenOver reports whether duet manages the config: the helper says it
// applied one or has an active subscription. Without a helper answer it
// is false when no helper is installed and true otherwise, so a helper
// that went down after takeover does not make problems look harmless.
func (s Snapshot) TakenOver() bool {
	h := s.Helper
	switch {
	case h.Status != nil:
		return h.Status.LastApply != nil || h.Status.ActiveSubscription != ""
	case h.State == HelperNotInstalled:
		return false
	}
	return true
}

// HealthBadge is the health checks' one number, as every place shows it:
// the failures when there are some, else the warnings, with the level
// they count at (0 and LevelOK: nothing to show).
func (s Snapshot) HealthBadge() (int, Level) {
	for _, r := range s.Reasons {
		if r.Code == ReasonHealth {
			return r.Count, r.Level
		}
	}
	return 0, LevelOK
}

// BuildGroups turns a /proxies answer into groups in configuration order.
func BuildGroups(all map[string]mihomo.Proxy) []Group { return buildGroups(all, nil) }

// buildGroups is BuildGroups with the monitor's own delay results, which
// win over mihomo's history when newer.
func buildGroups(all map[string]mihomo.Proxy, cache map[string]delayResult) []Group {
	var out []Group
	for _, p := range mihomo.Groups(all) {
		g := Group{Name: p.Name, Type: p.Type, Now: p.Now, Hidden: p.Hidden, TestURL: p.TestURL}
		g.Chain = mihomo.Resolve(all, p.Name)
		if len(g.Chain) > 0 {
			g.Node = g.Chain[len(g.Chain)-1]
			g.Delay, g.Tested, g.Builtin = delayOf(all, cache, g.Node)
		}
		for _, name := range p.All {
			m := Member{Name: name}
			if q, ok := all[name]; ok {
				m.Type, m.IsGroup, m.Alive = q.Type, q.IsGroup(), q.Alive
				target := name
				if m.IsGroup { // a nested group's delay is its node's
					if ch := mihomo.Resolve(all, name); len(ch) > 0 {
						target = ch[len(ch)-1]
					}
				}
				m.Delay, m.Tested, m.Builtin = delayOf(all, cache, target)
			} else {
				m.Builtin = mihomo.IsBuiltin(name, "")
			}
			g.Members = append(g.Members, m)
		}
		out = append(out, g)
	}
	return out
}

// delayOf is the delay of the node name: the newer of mihomo's last test
// and the monitor's own result. Built-ins have none.
func delayOf(all map[string]mihomo.Proxy, cache map[string]delayResult, name string) (ms int, tested, builtin bool) {
	p := all[name]
	if mihomo.IsBuiltin(name, p.Type) {
		return 0, false, true
	}
	ms, tested = p.LastDelay()
	var at time.Time
	if n := len(p.History); n > 0 {
		at = p.History[n-1].Time
	}
	if r, ok := cache[name]; ok && (!tested || !r.at.Before(at)) {
		ms, tested = r.ms, true
	}
	return ms, tested, false
}

// PickPrimary returns the group named name, else the first Selector that
// is not GLOBAL, else nil.
func PickPrimary(groups []Group, name string) *Group {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	for i := range groups {
		if groups[i].Type == "Selector" && groups[i].Name != "GLOBAL" {
			return &groups[i]
		}
	}
	return nil
}

// KeyExpiryWarning is how early an expiring Tailscale key turns yellow.
const KeyExpiryWarning = 7 * 24 * time.Hour

// Evaluate sets s.Level and s.Reasons from the rest of s.
func Evaluate(s *Snapshot) {
	t := i18n.Cur()
	var rs []Reason
	add := func(l Level, code, text string) { rs = append(rs, Reason{Level: l, Text: text, Code: code}) }
	// down says "can't reach X" in the UI language, with the reason in
	// words when duet knows it; the system's own text goes in Raw.
	down := func(code, head, err string) {
		text := head
		if why := i18n.NetReason(err); why != "" {
			text += t.NetReasonSep + why
		}
		rs = append(rs, Reason{Level: LevelError, Text: text, Code: code, Raw: err})
	}
	m := s.Mihomo
	switch {
	case m.Unauthorized:
		add(LevelError, ReasonMihomoAuth, t.ReasonMihomoAuth)
	case !m.Reachable && m.Err != "":
		down(ReasonMihomoDown, t.ReasonMihomoDown, m.Err)
	case m.Reachable:
		if !m.Config.Tun.Enable {
			add(LevelWarn, ReasonTunOff, t.ReasonTunOff)
		}
		if p := m.Primary; p != nil && p.Tested && p.Delay <= 0 && !p.Builtin {
			add(LevelWarn, ReasonPrimaryDead, fmt.Sprintf(t.ReasonPrimaryDead, p.Name))
		}
	}
	ts := s.Tailscale
	st := ts.State
	switch {
	case !ts.Reachable && ts.Install.Kind == tailscale.InstallUnknown && ts.Err != "":
		add(LevelWarn, ReasonTSNotInstalled, t.ReasonTSNotInstalled)
	case !ts.Reachable && ts.Err != "":
		down(ReasonTSDown, t.ReasonTSDown, ts.Err)
	case ts.Reachable:
		switch st.BackendState {
		case "NeedsLogin", "NeedsMachineAuth":
			add(LevelError, ReasonTSNeedsLogin, t.ReasonTSNeedsLogin)
		case "Stopped":
			add(LevelWarn, ReasonTSStopped, t.ReasonTSStopped)
		}
		// An exit node the user accepted is a state, not a problem
		// (the overview shows it on a neutral row).
		if _, ok := s.ExitNodeAccepted(); st.ExitNode != nil && !ok {
			add(LevelWarn, ReasonTSExitNode, fmt.Sprintf(t.ReasonTSExitNode, st.ExitNode.Name))
		}
		if st.KeyExpiry != nil && st.KeyExpiresWithin(s.Time, KeyExpiryWarning) {
			add(LevelWarn, ReasonTSKeyExpiry, fmt.Sprintf(t.ReasonTSKeyExpiry, Until(s.Time, *st.KeyExpiry)))
		}
		for _, h := range st.Health {
			add(LevelWarn, ReasonTSHealth, fmt.Sprintf(t.ReasonTSHealth, h))
		}
	}
	if ts.Install.UsesSystemVPN {
		add(LevelWarn, ReasonTSSystemVPN, t.ReasonTSSystemVPN)
	}
	// The health checks overlap the reasons above (mihomo down fails some of
	// them too), but they also see what the snapshot cannot: routes, DNS,
	// rules. One number counts: the failures, else the warnings. Before
	// duet manages the config, failures are what it found on this Mac,
	// not something that broke: a warning.
	warn, fail := s.Health.Counts()
	// Failures that only an exit node the user has not decided on yet
	// causes are a choice to make, not something duet broke: a warning,
	// with 接受 / 关闭出口节点 next to it (design review U4).
	exitOnly := false
	if fail > 0 && ts.Reachable && st.ExitNode != nil && s.Health.Report != nil {
		c := health.Accept(s.Health.Report, health.CauseExitNode).Counts()
		exitOnly = c[health.StatusFail] == 0
	}
	switch {
	case fail > 0 && (!s.TakenOver() || exitOnly):
		rs = append(rs, Reason{Level: LevelWarn, Text: i18n.F(t.ReasonHealthFound, fail), Health: true, Code: ReasonHealth, Count: fail})
	case fail > 0:
		rs = append(rs, Reason{Level: LevelError, Text: fmt.Sprintf(t.ReasonHealthFail, fail), Health: true, Code: ReasonHealth, Count: fail})
	case warn > 0:
		rs = append(rs, Reason{Level: LevelWarn, Text: i18n.F(t.ReasonHealthWarn, warn), Health: true, Code: ReasonHealth, Count: warn})
	}
	s.Reasons = rs
	switch {
	case !s.Loaded:
		s.Level = LevelUnknown
	default:
		s.Level = LevelOK
		for _, r := range rs {
			s.Level = max(s.Level, r.Level)
		}
	}
}

// String returns the level's machine name: ok, warn, error, unknown.
func (l Level) String() string {
	switch l {
	case LevelOK:
		return "ok"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	}
	return "unknown"
}

// MarshalText encodes the level by its machine name (JSON output).
func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }
