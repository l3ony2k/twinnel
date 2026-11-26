// Package status combines mihomo and Tailscale into one snapshot that the
// UI renders, keeps it fresh (polling plus event streams), and rates the
// overall state green / yellow / red.
package status

import (
	"fmt"
	"time"

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
		return i18n.T.StatusOK
	case LevelWarn:
		return i18n.T.StatusWarn
	case LevelError:
		return i18n.T.StatusError
	}
	return i18n.T.StatusUnknown
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
}

// Group is a proxy group with its members resolved.
type Group struct {
	Name    string
	Type    string
	Now     string
	Chain   []string // Now, then what nested groups selected, to the node
	Node    string   // last of Chain: the node carrying the traffic
	Delay   int      // delay of Node
	Tested  bool
	Members []Member
	Hidden  bool
	TestURL string
}

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
	Updated      time.Time
}

// Tailscale is Tailscale's part of a snapshot.
type Tailscale struct {
	Reachable bool
	Err       string
	Install   tailscale.Install
	State     tailscale.State
	Updated   time.Time
}

// Snapshot is everything the UI shows at one time.
type Snapshot struct {
	Time          time.Time
	Mihomo        Mihomo
	Tailscale     Tailscale
	Health        Health
	Level         Level
	Reasons       []Reason
	WritesAllowed bool
	Loaded        bool // both sides answered or failed at least once
}

// Reason explains a level other than OK.
type Reason struct {
	Level  Level
	Text   string
	Health bool // comes from the health checks (the panel shows those on a line of their own)
}

// BuildGroups turns a /proxies answer into groups in configuration order.
func BuildGroups(all map[string]mihomo.Proxy) []Group {
	var out []Group
	for _, p := range mihomo.Groups(all) {
		g := Group{Name: p.Name, Type: p.Type, Now: p.Now, Hidden: p.Hidden, TestURL: p.TestURL}
		g.Chain = mihomo.Resolve(all, p.Name)
		if len(g.Chain) > 0 {
			g.Node = g.Chain[len(g.Chain)-1]
			g.Delay, g.Tested = all[g.Node].LastDelay()
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
				m.Delay, m.Tested = all[target].LastDelay()
			}
			g.Members = append(g.Members, m)
		}
		out = append(out, g)
	}
	return out
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
	t := i18n.T
	var rs []Reason
	add := func(l Level, text string) { rs = append(rs, Reason{Level: l, Text: text}) }
	m := s.Mihomo
	switch {
	case m.Unauthorized:
		add(LevelError, t.ReasonMihomoAuth)
	case !m.Reachable && m.Err != "":
		add(LevelError, fmt.Sprintf(t.ReasonMihomoDown, m.Err))
	case m.Reachable:
		if !m.Config.Tun.Enable {
			add(LevelWarn, t.ReasonTunOff)
		}
		if p := m.Primary; p != nil && p.Tested && p.Delay <= 0 && p.Node != "DIRECT" && p.Node != "REJECT" {
			add(LevelWarn, fmt.Sprintf(t.ReasonPrimaryDead, p.Name))
		}
	}
	ts := s.Tailscale
	st := ts.State
	switch {
	case !ts.Reachable && ts.Install.Kind == tailscale.InstallUnknown && ts.Err != "":
		add(LevelWarn, t.ReasonTSNotInstalled)
	case !ts.Reachable && ts.Err != "":
		add(LevelError, fmt.Sprintf(t.ReasonTSDown, ts.Err))
	case ts.Reachable:
		switch st.BackendState {
		case "NeedsLogin", "NeedsMachineAuth":
			add(LevelError, t.ReasonTSNeedsLogin)
		case "Stopped":
			add(LevelWarn, t.ReasonTSStopped)
		}
		if st.ExitNode != nil {
			add(LevelWarn, fmt.Sprintf(t.ReasonTSExitNode, st.ExitNode.Name))
		}
		if st.KeyExpiry != nil && st.KeyExpiresWithin(s.Time, KeyExpiryWarning) {
			add(LevelWarn, fmt.Sprintf(t.ReasonTSKeyExpiry, Until(s.Time, *st.KeyExpiry)))
		}
		for _, h := range st.Health {
			add(LevelWarn, fmt.Sprintf(t.ReasonTSHealth, h))
		}
	}
	if ts.Install.UsesSystemVPN {
		add(LevelWarn, t.ReasonTSSystemVPN)
	}
	// The health checks overlap the reasons above (mihomo down fails some of
	// them too), but they also see what the snapshot cannot: routes, DNS,
	// rules. Their worst result counts.
	switch warn, fail := s.Health.Counts(); {
	case fail > 0:
		rs = append(rs, Reason{LevelError, fmt.Sprintf(t.ReasonHealthFail, fail), true})
	case warn > 0:
		rs = append(rs, Reason{LevelWarn, fmt.Sprintf(t.ReasonHealthWarn, warn), true})
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
