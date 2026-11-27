package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/rpc"
)

// healthState is what the engine remembers about the health scheduler.
type healthState struct {
	mu     sync.Mutex
	sched  *health.Scheduler
	reason string
	at     time.Time
}

// SetHealth attaches the health scheduler. Run runs it; every finished run
// publishes health.updated, and every check whose status changed since the
// previous run publishes check.changed (the first run only publishes
// health.updated). A successful apply that wrote a new config triggers a
// run. Call before Run.
func (e *Engine) SetHealth(s *health.Scheduler) {
	if s.Overlay == nil {
		s.Overlay = e.egressOverlay
	}
	prev := s.OnEvent
	s.OnEvent = func(ev health.Event) {
		e.onHealthEvent(ev)
		if prev != nil {
			prev(ev)
		}
	}
	e.health.mu.Lock()
	e.health.sched = s
	e.health.mu.Unlock()
}

func (e *Engine) healthSched() *health.Scheduler {
	e.health.mu.Lock()
	defer e.health.mu.Unlock()
	return e.health.sched
}

func (e *Engine) onHealthEvent(ev health.Event) {
	e.health.mu.Lock()
	e.health.reason, e.health.at = ev.Reason, ev.At.UTC()
	e.health.mu.Unlock()
	for _, c := range ev.Changes {
		if c.From == "" {
			continue
		}
		e.publish(rpc.EventCheckChanged, c)
	}
	sum := rpc.HealthSummary{Reason: ev.Reason, At: ev.At.UTC(), Counts: map[string]int{}}
	if ev.Report != nil {
		sum.Overall = string(ev.Report.Overall)
		for st, n := range ev.Report.Counts() {
			sum.Counts[string(st)] = n
		}
	}
	e.publish(rpc.EventHealthUpdated, sum)
}

// triggerHealth asks for a (debounced) health run, if health is attached.
func (e *Engine) triggerHealth(reason string) {
	if s := e.healthSched(); s != nil {
		s.Trigger(reason)
	}
}

func (e *Engine) healthReport(rep *health.Report) (rpc.HealthReport, error) {
	var out rpc.HealthReport
	e.health.mu.Lock()
	out.Reason, out.At = e.health.reason, e.health.at
	e.health.mu.Unlock()
	if rep == nil {
		return out, nil
	}
	b, err := json.Marshal(rep)
	if err != nil {
		return out, err
	}
	out.Report = b
	return out, nil
}

// HealthLast returns the newest report without running anything; a lang
// other than the report's asks for a run in it (Scheduler.SetLang).
func (e *Engine) HealthLast(lang string) (rpc.HealthReport, error) {
	s := e.healthSched()
	if s == nil {
		return rpc.HealthReport{}, rpc.Errorf(rpc.CodeFailed, "health checks are not available in this helper")
	}
	s.SetLang(lang)
	return e.healthReport(s.Last())
}

// HealthRun runs every check now (waiting for a run in progress) and
// returns the new report.
func (e *Engine) HealthRun(ctx context.Context, reason, lang string) (rpc.HealthReport, error) {
	s := e.healthSched()
	if s == nil {
		return rpc.HealthReport{}, rpc.Errorf(rpc.CodeFailed, "health checks are not available in this helper")
	}
	if reason == "" {
		reason = "manual"
	}
	s.UseLang(lang)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	rep := s.RunNow(ctx, reason)
	if ctx.Err() != nil {
		return rpc.HealthReport{}, rpc.Errorf(rpc.CodeFailed, "health run cancelled: %v", ctx.Err())
	}
	return e.healthReport(rep)
}

// netLoop forwards network changes: a debounced health run, and with
// coexist on a tailnet refresh (a tailscaled restart can move the utun).
func (e *Engine) netLoop(ctx context.Context) {
	if e.NetChanges == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case reason, ok := <-e.NetChanges:
			if !ok {
				return
			}
			e.triggerHealth(reason)
			if st, err := e.Store.Settings(); err == nil && st.Tailscale.Coexist && e.Tailnet != nil {
				if _, err := e.RefreshTailnet(ctx); err != nil && ctx.Err() == nil {
					e.log().Debug("tailnet refresh after network change", "err", err)
				}
			}
		}
	}
}

// --- the egress test on demand (health.egress) ---

// egressFile keeps the last on-demand egress result (a health.CheckReport
// with At), so it survives a helper restart.
const egressFile = "health-egress.json"

// EgressKeep is how long an on-demand egress result stands in for the
// check while 定期检测 is off. Older, the row says 未开启 again.
const EgressKeep = 24 * time.Hour

// HealthEgress runs the egress test once (EgressProbe), keeps the result
// with its time, puts it into the last report and returns that report.
func (e *Engine) HealthEgress(ctx context.Context, lang string) (rpc.HealthReport, error) {
	s := e.healthSched()
	if s == nil || e.EgressProbe == nil {
		e.log().Warn("egress test", "err", "not available (health checks off)")
		return rpc.HealthReport{}, rpc.Errorf(rpc.CodeFailed, "the egress test is not available in this helper")
	}
	if lang != "" {
		s.UseLang(lang)
		ctx = health.WithLang(ctx, lang)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cr := e.EgressProbe(ctx)
	if ctx.Err() != nil {
		e.log().Warn("egress test", "err", ctx.Err())
		return rpc.HealthReport{}, rpc.Errorf(rpc.CodeFailed, "egress test cancelled: %v", ctx.Err())
	}
	cr.At = e.now().UTC()
	// The summary names no IP (MaskIP'd evidence stays in the file).
	lv := slog.LevelInfo
	if cr.Status != health.StatusOK {
		lv = slog.LevelWarn
	}
	e.log().Log(ctx, lv, "egress test", "status", cr.Status, "summary", cr.Summary)
	if err := e.Store.WriteJSON(egressFile, cr); err != nil {
		e.log().Warn("save egress result", "err", err)
	}
	rep := s.Replace(cr, "egress")
	if rep == nil { // no report yet: run them all, the overlay brings it in
		rep = s.RunNow(ctx, "egress")
	}
	return e.healthReport(rep)
}

// egressOverlay is the scheduler's Overlay: while 定期检测 is off, the
// last on-demand egress result (younger than EgressKeep) stands in for
// the check's 未开启.
func (e *Engine) egressOverlay(rep *health.Report) {
	if rep == nil {
		return
	}
	if st, err := e.Store.Settings(); err == nil && st.HealthEgress {
		return // the run measured it
	}
	var cr health.CheckReport
	if err := e.Store.ReadJSON(egressFile, &cr); err != nil || cr.ID != (health.LeakEgress{}).ID() {
		return
	}
	if cr.At.IsZero() || e.now().Sub(cr.At) > EgressKeep {
		return
	}
	for i, c := range rep.Checks {
		if c.ID == cr.ID && c.Status == health.StatusSkip && c.At.IsZero() {
			rep.Checks[i] = cr
		}
	}
}
