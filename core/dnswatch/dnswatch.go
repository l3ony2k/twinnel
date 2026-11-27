// Package dnswatch keeps the system DNS usable when mihomo dies.
//
// Once Twinnel pointed the system DNS at mihomo (core/sysdns: dns.point,
// a backup exists), every name lookup on the machine depends on mihomo's
// DNS listener. If mihomo stops, hangs or crash-loops, the machine loses
// name resolution, and a user who does not know where the setting is
// cannot get back online. The Watchdog notices that and temporarily
// writes the backed-up DNS servers back; once mihomo has answered again
// for a while it points the system DNS at mihomo again.
//
// It never fights the user: it only restores services that still point
// at mihomo (sysdns.PointsAt the target), and it only points back services
// that still hold exactly the values it restored. A service the user
// changed by hand in between is left as the user set it.
//
// The platform parts (the resolver probe, the service manager, the system
// DNS) are functions and interfaces, so tests drive it with fakes and a
// fake clock through Tick.
package dnswatch

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/sysdns"
)

// Phases of the watchdog.
const (
	// PhaseOff: Twinnel did not point the system DNS (no backup), or the
	// platform cannot set it. Nothing to watch.
	PhaseOff = "off"
	// PhaseWatching: the system DNS is Twinnel's and mihomo answers (or
	// has not been down long enough to act).
	PhaseWatching = "watching"
	// PhaseRestored: the watchdog wrote the earlier DNS back because
	// mihomo did not answer; it points back once mihomo is healthy.
	PhaseRestored = "restored"
)

// Why the watchdog restored the DNS.
const (
	// CauseStopped: mihomo's service is not running.
	CauseStopped = "stopped"
	// CauseCrashLoop: the service restarted again and again.
	CauseCrashLoop = "crash-loop"
	// CauseNoAnswer: the service runs but its DNS has not answered for a
	// long time (hung, or its DNS listener is off).
	CauseNoAnswer = "no-answer"
)

// Service is what the service manager says about mihomo's service.
type Service struct {
	// Known: the manager answered (false: unknown, treated as not running
	// once DNS has failed long enough).
	Known   bool
	Running bool
	PID     int
	// Runs counts the starts (launchd's "runs"); 0 when the platform does
	// not count them (the PID is compared then).
	Runs int
}

// Config tunes the watchdog; zero fields take the defaults.
type Config struct {
	// Interval between ticks in Run (default 5s).
	Interval time.Duration
	// DownAfter: how long mihomo's DNS must fail, with the service not
	// running, before the DNS is restored (default 20s: longer than a
	// restart or a kernel switch).
	DownAfter time.Duration
	// HangAfter: how long it must fail while the service runs (default
	// 90s).
	HangAfter time.Duration
	// CrashRestarts within CrashWindow count as a crash loop (default 3
	// in 3 minutes); the DNS is restored at the next failed probe.
	CrashRestarts int
	CrashWindow   time.Duration
	// RepointAfter: how long mihomo must answer, with the service running
	// and not crash-looping, before the DNS is pointed back (default 30s).
	RepointAfter time.Duration
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.DownAfter <= 0 {
		c.DownAfter = 20 * time.Second
	}
	if c.HangAfter <= 0 {
		c.HangAfter = 90 * time.Second
	}
	if c.CrashRestarts <= 0 {
		c.CrashRestarts = 3
	}
	if c.CrashWindow <= 0 {
		c.CrashWindow = 3 * time.Minute
	}
	if c.RepointAfter <= 0 {
		c.RepointAfter = 30 * time.Second
	}
	return c
}

// State is the watchdog's persistent state (kept across helper restarts:
// a helper that restarts while the DNS is restored still points it back).
type State struct {
	Phase string `json:"phase"`
	// Cause and Since: why and when the DNS was restored (PhaseRestored).
	Cause string    `json:"cause,omitempty"`
	Since time.Time `json:"since,omitzero"`
	// Restored are the services the watchdog wrote back, with the values
	// it wrote: what pointing back compares with.
	Restored []sysdns.Service `json:"restored,omitempty"`
	// LeftAlone are the services the last point-back skipped because the
	// user changed their DNS by hand in between.
	LeftAlone []string `json:"left_alone,omitempty"`
	// LastRepoint is when the watchdog last pointed the DNS back.
	LastRepoint time.Time `json:"last_repoint,omitzero"`
}

// IsRestored reports whether the watchdog holds the DNS restored.
func (s State) IsRestored() bool { return s.Phase == PhaseRestored }

// Watchdog watches mihomo's DNS. Create it with the fields set, then call
// Run (or Tick from tests).
type Watchdog struct {
	Config Config
	// DNS reads and sets the system DNS.
	DNS sysdns.Platform
	// Backup returns what the services used before Twinnel pointed them
	// (nil: Twinnel did not, nothing to watch).
	Backup func() (*sysdns.Backup, error)
	// Target is what Twinnel points the services at.
	Target func() []string
	// Probe sends one query to mihomo's DNS; nil error: it answered.
	Probe func(ctx context.Context) error
	// Service reads mihomo's service (nil: never known).
	Service func(ctx context.Context) (Service, error)
	// Busy reports that a deliberate change runs (a service restart, a
	// kernel switch, a takeover): a failing probe is expected then.
	Busy func() bool
	// Lock is held while the watchdog reads and sets the system DNS, so
	// dns.point and dns.restore never interleave with it (nil: none).
	Lock sync.Locker
	// Path keeps the State ("": in memory only).
	Path string
	Now  func() time.Time
	Log  *slog.Logger
	// OnChange is called after the phase changed (restored, pointed back,
	// cleared), outside Lock.
	OnChange func(State)

	mu    sync.Mutex
	state State
	// loaded: state was read from Path.
	loaded bool

	// in memory only:
	failSince time.Time // first failed probe of this episode
	okSince   time.Time // first good probe after a failure (restored)
	lastFail  time.Time
	lastPID   int
	lastRuns  int
	restarts  []time.Time
	noted     bool // "nothing to restore" was logged this episode
}

func (w *Watchdog) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Watchdog) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

// State returns the current state.
func (w *Watchdog) State() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	st := w.state
	st.Restored = slices.Clone(st.Restored)
	st.LeftAlone = slices.Clone(st.LeftAlone)
	if st.Phase == "" {
		st.Phase = PhaseOff
	}
	return st
}

// load reads the state file once. w.mu held.
func (w *Watchdog) load() {
	if w.loaded {
		return
	}
	w.loaded = true
	if w.Path == "" {
		return
	}
	b, err := os.ReadFile(w.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.log().Warn("dns watchdog: read state", "err", err)
		}
		return
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		w.log().Warn("dns watchdog: bad state file", "err", err)
		return
	}
	w.state = st
}

// save writes the state. w.mu held.
func (w *Watchdog) save() {
	if w.Path == "" {
		return
	}
	if w.state.Phase != PhaseRestored && w.state.LastRepoint.IsZero() && len(w.state.LeftAlone) == 0 {
		if err := os.Remove(w.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			w.log().Warn("dns watchdog: remove state", "err", err)
		}
		return
	}
	b, err := json.MarshalIndent(w.state, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(w.Path), 0o700)
	}
	if err == nil {
		tmp := w.Path + ".tmp"
		if err = os.WriteFile(tmp, append(b, '\n'), 0o600); err == nil {
			err = os.Rename(tmp, w.Path)
		}
	}
	if err != nil {
		w.log().Warn("dns watchdog: write state", "err", err)
	}
}

// Reset forgets a restore: the user pointed the DNS again (dns.point) or
// kept the earlier one for good (dns.restore). Call it with Lock held or
// after the change; it never touches the system DNS.
func (w *Watchdog) Reset() {
	w.mu.Lock()
	w.load()
	changed := w.state.Phase == PhaseRestored
	w.state = State{}
	w.failSince, w.okSince, w.noted = time.Time{}, time.Time{}, false
	w.save()
	st := w.state
	w.mu.Unlock()
	if changed && w.OnChange != nil {
		st.Phase = PhaseOff
		w.OnChange(st)
	}
}

// Run ticks every Config.Interval until ctx ends.
func (w *Watchdog) Run(ctx context.Context) {
	cfg := w.Config.withDefaults()
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		w.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick looks once: probe, decide, act.
func (w *Watchdog) Tick(ctx context.Context) {
	if w.Lock != nil {
		w.Lock.Lock()
	}
	changed, st := w.tick(ctx)
	if w.Lock != nil {
		w.Lock.Unlock()
	}
	if changed && w.OnChange != nil {
		w.OnChange(st)
	}
}

func (w *Watchdog) tick(ctx context.Context) (bool, State) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	cfg := w.Config.withDefaults()
	now := w.now()

	if w.DNS == nil || w.Backup == nil || w.Probe == nil || w.Target == nil {
		return false, w.state
	}
	bk, err := w.Backup()
	if err != nil {
		w.log().Warn("dns watchdog: read backup", "err", err)
		return false, w.state
	}
	if bk == nil {
		// Twinnel no longer owns the system DNS (dns.restore, uninstall):
		// a restore in progress is over.
		if w.state.Phase == PhaseRestored {
			w.state = State{}
			w.save()
			return true, w.state
		}
		w.failSince, w.okSince = time.Time{}, time.Time{}
		return false, w.state
	}
	if w.state.Phase == "" || w.state.Phase == PhaseOff {
		w.state.Phase = PhaseWatching
	}
	if w.Busy != nil && w.Busy() {
		// A deliberate restart: neither its downtime nor its first
		// answers say anything.
		w.failSince, w.okSince = time.Time{}, time.Time{}
		return false, w.state
	}
	target := w.Target()
	if len(target) == 0 {
		return false, w.state
	}
	probeErr := w.Probe(ctx)
	if ctx.Err() != nil {
		return false, w.state
	}

	if w.state.Phase == PhaseRestored {
		return w.whileRestored(ctx, cfg, now, target, probeErr)
	}
	return w.whileWatching(ctx, cfg, now, target, bk, probeErr)
}

// sample reads the service and counts its restarts. w.mu held.
func (w *Watchdog) sample(ctx context.Context, cfg Config, now time.Time) Service {
	if w.Service == nil {
		return Service{}
	}
	s, err := w.Service(ctx)
	if err != nil {
		return Service{}
	}
	switch {
	case s.Runs > 0 && w.lastRuns > 0 && s.Runs > w.lastRuns:
		for range min(s.Runs-w.lastRuns, cfg.CrashRestarts) {
			w.restarts = append(w.restarts, now)
		}
	case s.Runs == 0 && s.PID > 0 && w.lastPID > 0 && s.PID != w.lastPID:
		w.restarts = append(w.restarts, now)
	}
	if s.Runs > 0 {
		w.lastRuns = s.Runs
	}
	if s.PID > 0 {
		w.lastPID = s.PID
	}
	w.restarts = slices.DeleteFunc(w.restarts, func(t time.Time) bool { return now.Sub(t) > cfg.CrashWindow })
	return s
}

func (w *Watchdog) crashLooping(cfg Config) bool { return len(w.restarts) >= cfg.CrashRestarts }

func (w *Watchdog) whileWatching(ctx context.Context, cfg Config, now time.Time, target []string, bk *sysdns.Backup, probeErr error) (bool, State) {
	if probeErr == nil {
		w.failSince, w.noted = time.Time{}, false
		if !w.lastFail.IsZero() && now.Sub(w.lastFail) > cfg.CrashWindow {
			// Long healthy: earlier restarts say nothing any more.
			w.lastFail, w.lastPID, w.lastRuns, w.restarts = time.Time{}, 0, 0, nil
		}
		return false, w.state
	}
	if w.failSince.IsZero() {
		w.failSince = now
	}
	w.lastFail = now
	svc := w.sample(ctx, cfg, now)
	down := now.Sub(w.failSince)
	var cause string
	switch {
	case w.crashLooping(cfg):
		cause = CauseCrashLoop
	case down >= cfg.DownAfter && !(svc.Known && svc.Running):
		cause = CauseStopped
	case down >= cfg.HangAfter:
		cause = CauseNoAnswer
	default:
		return false, w.state
	}

	svcs, err := w.DNS.Services(ctx)
	if err != nil {
		w.log().Warn("dns watchdog: read system DNS", "err", err)
		return false, w.state
	}
	var restored []sysdns.Service
	var failed error
	for _, b := range bk.Services {
		i := slices.IndexFunc(svcs, func(s sysdns.Service) bool { return s.Name == b.Name })
		if i < 0 || !sysdns.PointsAt(svcs[i].Servers, target) {
			continue // gone, or not on mihomo any more: the user's
		}
		val, _ := bk.RestoreValue(b.Name, target)
		if err := w.DNS.Set(ctx, b.Name, val); err != nil {
			failed = err
			w.log().Warn("dns watchdog: restore", "service", b.Name, "err", err)
			continue
		}
		restored = append(restored, sysdns.Service{Name: b.Name, Servers: val})
	}
	if len(restored) == 0 {
		if !w.noted {
			w.noted = true
			w.log().Info("dns watchdog: mihomo's DNS is down but no service uses it; nothing to restore", "cause", cause, "err", failed)
		}
		return false, w.state
	}
	w.state = State{Phase: PhaseRestored, Cause: cause, Since: now, Restored: restored}
	w.okSince = time.Time{}
	w.save()
	w.log().Warn("dns watchdog: mihomo's DNS does not answer; system DNS temporarily restored",
		"cause", cause, "services", len(restored), "down_for", down.Round(time.Second), "probe", probeErr)
	return true, w.state
}

func (w *Watchdog) whileRestored(ctx context.Context, cfg Config, now time.Time, target []string, probeErr error) (bool, State) {
	if probeErr != nil {
		w.okSince = time.Time{}
		w.lastFail = now
		w.sample(ctx, cfg, now)
		return false, w.state
	}
	if w.okSince.IsZero() {
		w.okSince = now
	}
	svc := w.sample(ctx, cfg, now)
	if now.Sub(w.okSince) < cfg.RepointAfter || w.crashLooping(cfg) || svc.Known && !svc.Running {
		return false, w.state
	}
	svcs, err := w.DNS.Services(ctx)
	if err != nil {
		w.log().Warn("dns watchdog: read system DNS", "err", err)
		return false, w.state
	}
	var back, left []string
	for _, r := range w.state.Restored {
		i := slices.IndexFunc(svcs, func(s sysdns.Service) bool { return s.Name == r.Name })
		if i < 0 {
			continue
		}
		if !slices.Equal(svcs[i].Servers, r.Servers) {
			left = append(left, r.Name) // changed by hand since: the user's
			continue
		}
		if err := w.DNS.Set(ctx, r.Name, target); err != nil {
			// Try again on the next tick; what was pointed stays pointed.
			w.log().Warn("dns watchdog: point back", "service", r.Name, "err", err)
			w.state.Restored = slices.DeleteFunc(w.state.Restored, func(s sysdns.Service) bool { return slices.Contains(back, s.Name) })
			w.save()
			return false, w.state
		}
		back = append(back, r.Name)
	}
	w.state = State{Phase: PhaseWatching, LeftAlone: left, LastRepoint: now}
	w.failSince, w.okSince, w.noted = time.Time{}, time.Time{}, false
	w.restarts, w.lastFail = nil, time.Time{}
	w.save()
	w.log().Info("dns watchdog: mihomo answers again; system DNS pointed back", "services", len(back), "left_alone", len(left))
	return true, w.state
}
