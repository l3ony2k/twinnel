package status

import (
	"context"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
)

// HealthSource runs the coexistence health checks. Today the app runs them
// in-process (health.Runner over the platform Env); later the helper will
// answer over RPC behind the same interface.
type HealthSource interface {
	Run(ctx context.Context) (*health.Report, error)
}

// HealthFunc adapts a function to HealthSource.
type HealthFunc func(ctx context.Context) (*health.Report, error)

// Run calls f.
func (f HealthFunc) Run(ctx context.Context) (*health.Report, error) { return f(ctx) }

// RunnerSource runs a health.Runner over an Env made fresh for every run.
type RunnerSource struct {
	Runner *health.Runner
	Env    func() health.Env
}

// Run runs all checks once.
func (r RunnerSource) Run(ctx context.Context) (*health.Report, error) {
	return r.Runner.Run(health.WithLang(ctx, i18n.Tag()), r.Env()), nil
}

// Health is the health part of a snapshot.
type Health struct {
	Report  *health.Report // last finished run; nil before the first
	Running bool
	Err     string
	Updated time.Time
}

// Counts returns the number of warn and fail results of the last report.
func (h Health) Counts() (warn, fail int) {
	if h.Report == nil {
		return 0, 0
	}
	c := h.Report.Counts()
	return c[health.StatusWarn], c[health.StatusFail]
}

// SetExitNodeAccepted records the exit node the user accepted ("" for
// none) and re-rates the snapshot.
func (m *Monitor) SetExitNodeAccepted(name string) {
	m.update(func(s *Snapshot) { s.AcceptedExitNode = name })
}

// Stale reports whether the last report is older than after at now.
func (h Health) Stale(now time.Time, after time.Duration) bool {
	return h.Report != nil && now.Sub(h.Report.Started) > after
}

// HealthTimeout bounds one run of all checks.
const HealthTimeout = 60 * time.Second

// RunHealth runs the health checks now unless a run is under way, and
// returns when it is done. Without a HealthSource it does nothing.
func (m *Monitor) RunHealth(ctx context.Context) {
	src := m.o.Health
	if src == nil {
		return
	}
	m.mu.Lock()
	if m.snap.Health.Running {
		m.mu.Unlock()
		return
	}
	m.snap.Health.Running = true // claimed under the lock: one run at a time
	m.mu.Unlock()
	m.update(func(*Snapshot) {})
	ctx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()
	rep, err := src.Run(ctx)
	m.update(func(s *Snapshot) {
		h := &s.Health
		h.Running = false
		h.Updated = m.o.Now()
		if err != nil {
			h.Err = ShortError(err)
			return
		}
		h.Err = ""
		m.rawHealth = rep // update applies the accepted causes
	})
}

// healthLoop runs the checks at start and then every HealthInterval.
func (m *Monitor) healthLoop(ctx context.Context) {
	for {
		m.RunHealth(ctx)
		if m.o.HealthInterval < 0 || !sleep(ctx, m.o.HealthInterval) {
			return
		}
	}
}
