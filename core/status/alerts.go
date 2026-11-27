package status

import (
	"fmt"
	"sort"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
)

// Alert is a desktop notification the app should show.
type Alert struct {
	Key   string // stable per condition: the notification ID, so a repeat replaces it
	Title string
	Body  string
}

// Alerter turns a stream of snapshots into alerts. Each condition fires
// once when it starts (after its grace time), not again until it has
// cleared, and at most once per MinGap even when it flaps; at most MaxBurst
// alerts go out per BurstWindow overall. Not safe for concurrent use.
type Alerter struct {
	MinGap      time.Duration // per key; default 30 min
	MaxBurst    int           // default 4
	BurstWindow time.Duration // default 5 min
	// MihomoGrace is how long mihomo must stay unreachable before it is
	// reported: a reload or restart takes a few seconds. Default 15 s.
	MihomoGrace time.Duration
	// KeyExpiryWarn is how early an expiring Tailscale key is reported.
	// Default 3 days.
	KeyExpiryWarn time.Duration

	eps      map[string]*episode
	lastSent map[string]time.Time
	sent     []time.Time
	// last known status of each health check (from the newest report seen)
	checks     map[string]health.Status
	lastReport time.Time
}

type episode struct {
	since time.Time
	fired bool
	ok    bool // may fire (health: the check was not failing before)
}

type cond struct {
	key, title, body string
	grace            time.Duration
	eligible         bool
}

func (a *Alerter) defaults() {
	if a.MinGap == 0 {
		a.MinGap = 30 * time.Minute
	}
	if a.MaxBurst == 0 {
		a.MaxBurst = 4
	}
	if a.BurstWindow == 0 {
		a.BurstWindow = 5 * time.Minute
	}
	if a.MihomoGrace == 0 {
		a.MihomoGrace = 15 * time.Second
	}
	if a.KeyExpiryWarn == 0 {
		a.KeyExpiryWarn = 3 * 24 * time.Hour
	}
	if a.eps == nil {
		a.eps = map[string]*episode{}
		a.lastSent = map[string]time.Time{}
		a.checks = map[string]health.Status{}
	}
}

// Check looks at a new snapshot (s.Time is "now") and returns the alerts
// to show, in a stable order.
func (a *Alerter) Check(s Snapshot) []Alert {
	a.defaults()
	now := s.Time
	t := i18n.Cur()
	var active []cond

	if s.Loaded && !s.Mihomo.Reachable && s.Mihomo.Err != "" {
		active = append(active, cond{key: "mihomo-down", title: t.AlertMihomoDown,
			body: s.Mihomo.Err, grace: a.MihomoGrace, eligible: true})
	}
	ts := s.Tailscale
	if ts.Reachable {
		switch ts.State.BackendState {
		case "NeedsLogin", "NeedsMachineAuth":
			active = append(active, cond{key: "ts-login", title: t.AlertTSNeedsLogin,
				body: t.TSState(ts.State.BackendState), eligible: true})
		}
		if exp := ts.State.KeyExpiry; exp != nil && exp.After(now) && exp.Sub(now) <= a.KeyExpiryWarn {
			active = append(active, cond{key: "ts-key/" + exp.UTC().Format(time.RFC3339), title: t.AlertTSKeyExpiry,
				body: fmt.Sprintf(t.AlertTSKeyExpiryBody, exp.Local().Format("2006-01-02 15:04"), Until(now, *exp)), eligible: true})
		}
	}

	// Health: a check that went from not failing (in the previous report)
	// to failing. A check already failing in the first report seen is not
	// reported: the tray turns red for it anyway.
	if rep := s.Health.Report; rep != nil {
		if rep.Started.After(a.lastReport) {
			prev := a.checks
			a.checks = map[string]health.Status{}
			for _, c := range rep.Checks {
				a.checks[c.ID] = c.Status
				if c.Status != health.StatusFail {
					continue
				}
				key := "health/" + c.ID
				p, known := prev[c.ID]
				if e := a.eps[key]; e == nil {
					a.eps[key] = &episode{since: now, ok: known && p != health.StatusFail}
				}
			}
			a.lastReport = rep.Started
		}
		for _, c := range rep.Checks {
			if c.Status == health.StatusFail {
				active = append(active, cond{key: "health/" + c.ID, title: fmt.Sprintf(t.AlertHealthFail, c.Title), body: c.Summary})
			}
		}
	}

	live := map[string]bool{}
	var out []Alert
	sort.SliceStable(active, func(i, j int) bool { return active[i].key < active[j].key })
	for _, c := range active {
		live[c.key] = true
		e := a.eps[c.key]
		if e == nil {
			e = &episode{since: now, ok: c.eligible}
			a.eps[c.key] = e
		}
		if e.fired || !e.ok || now.Sub(e.since) < c.grace {
			continue
		}
		if last, ok := a.lastSent[c.key]; ok && now.Sub(last) < a.MinGap {
			e.fired = true // flapping: swallow this episode
			continue
		}
		if !a.allowBurst(now) {
			continue // try again on the next snapshot
		}
		e.fired = true
		a.lastSent[c.key] = now
		a.sent = append(a.sent, now)
		out = append(out, Alert{Key: c.key, Title: c.title, Body: c.body})
	}
	for k := range a.eps {
		if !live[k] {
			delete(a.eps, k)
		}
	}
	return out
}

func (a *Alerter) allowBurst(now time.Time) bool {
	keep := a.sent[:0]
	for _, t := range a.sent {
		if now.Sub(t) < a.BurstWindow {
			keep = append(keep, t)
		}
	}
	a.sent = keep
	return len(a.sent) < a.MaxBurst
}
