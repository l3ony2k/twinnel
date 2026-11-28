package engine

// The state generation: how a health report knows it is out of date.
//
// The helper keeps the last health report, and clients reuse it instead of
// running the checks again (twinnel doctor, MCP doctor, the app's banner
// and status summary). Wall-clock age alone cannot tell that mihomo was
// stopped by someone else ten seconds ago, so every state change the
// helper sees bumps a counter and asks for a run (triggerHealth), and each
// report carries the counter as it was when its run started
// (health.Scheduler.Gen). health.last returns both; a report is current
// only while they match (rpc.HealthReport.Fresh).
//
// The changes: everything that calls triggerHealth (an apply, a service
// operation, dns.point/backup/restore, the DNS watchdog restoring or
// re-pointing, a takeover, a network change…), the events that say the
// system changed under the helper (service.changed, controller.owner,
// kernel.changed, tailnet.changed; noteEvent), and what status.get and
// health.last see of mihomo itself: whether its controller answers, its
// version, and whether its service runs and with which pid (observeMihomo),
// which catch a mihomo stopped, crashed or replaced outside the helper.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// stateGenState is the counter and the last look at mihomo.
type stateGenState struct {
	n    atomic.Uint64
	mu   sync.Mutex
	seen *mihomoSeen // nil before the first look
}

// mihomoSeen is what one look at mihomo found (observeMihomo).
type mihomoSeen struct {
	Reachable bool
	Version   string
	// Service: known (a managed service the manager can read), running,
	// and its pid.
	Service bool
	Running bool
	PID     int
}

// StateGen is the current state generation (at least 1).
func (e *Engine) StateGen() uint64 { return e.gen.n.Load() + 1 }

// stateChanged records a state change: a new generation, and a health run
// soon (debounced) so the report catches up.
func (e *Engine) stateChanged(reason string) {
	e.gen.n.Add(1)
	if s := e.healthSched(); s != nil {
		s.Trigger(reason)
	}
}

// stateEvents are the bus events that say the system changed.
var stateEvents = map[string]bool{
	rpc.EventServiceChanged:  true,
	rpc.EventControllerOwner: true,
	rpc.EventKernelChanged:   true,
	rpc.EventTailnetChanged:  true,
}

// noteEvent bumps the generation for a state event (publish).
func (e *Engine) noteEvent(typ string) {
	if stateEvents[typ] {
		e.stateChanged(typ)
	}
}

// observeMihomo looks at mihomo now: its controller's answer and its
// service. When either differs from the last look, the generation goes up
// and the controller's owner is read again (a new process may hold the
// port). It returns the controller's version or error for status.get.
func (e *Engine) observeMihomo(ctx context.Context, st *store.Settings) (string, error) {
	var seen mihomoSeen
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	v, verr := e.currentController(st).Version(vctx)
	cancel()
	if ctx.Err() != nil {
		return v, verr // the caller went away: what was seen says nothing
	}
	if verr == nil {
		seen.Reachable, seen.Version = true, v
	}
	if e.Services != nil && st.Mihomo.ServiceLabel != "" {
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		s, err := e.Services.Status(sctx, st.Mihomo.ServiceLabel)
		cancel()
		if err == nil && s.Installed {
			seen.Service, seen.Running, seen.PID = true, s.Running, s.PID
		}
	}
	e.gen.mu.Lock()
	prev := e.gen.seen
	e.gen.seen = &seen
	e.gen.mu.Unlock()
	if prev == nil || *prev == seen {
		return v, verr
	}
	reason := "mihomo service"
	if prev.Reachable != seen.Reachable || prev.Version != seen.Version {
		reason = "mihomo reachability"
	}
	e.log().Info("mihomo changed outside a helper operation", "reason", reason,
		"reachable", seen.Reachable, "running", seen.Running, "pid", seen.PID)
	e.stateChanged(reason)
	e.ControllerOwner(ctx) // publishes controller.owner when it changed
	return v, verr
}
