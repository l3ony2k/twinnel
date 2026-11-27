package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// The foreign-controller preflight (report P0 #2): before any reload,
// make sure the mihomo on the controller address is the one Twinnel runs.
// Another client's mihomo (Clash Party, Clash Verge Rev…) on the same
// port refuses Twinnel's config path (SAFE_PATHS) or, worse, loads it.

// ErrForeignController is wrapped by apply errors when the controller
// belongs to another program's mihomo.
var ErrForeignController = errors.New("the controller belongs to another mihomo")

// foreignError is the apply error of a foreign controller: worded for
// core/msg ("controller-foreign"), the evidence kept apart.
func foreignError(o foreign.Owner) error {
	who := "another program"
	if n := o.Client.Name(); n != "" {
		who = n
	}
	return fmt.Errorf("%w: %s is held by %s; Twinnel did not reload it", ErrForeignController, o.Addr, who)
}

// foreignIdleWarning is the warning of an apply that had nothing to send
// while another program's mihomo holds the controller: "ok" would hide
// that Twinnel's mihomo is not the one running (core/msg
// "controller-foreign-idle").
func foreignIdleWarning(o foreign.Owner) string {
	who := "another program"
	if n := o.Client.Name(); n != "" {
		who = n
	}
	return fmt.Sprintf("the config did not change, but %s is held by %s's mihomo; Twinnel's mihomo is not the one running", o.Addr, who)
}

// ownerTTL is how long a port-owner answer is reused by the auto-apply
// gate and status.get (an apply always asks again).
const ownerTTL = 20 * time.Second

// expectOwner is what Twinnel runs: the adopted binary or the managed
// kernel, and the managed service's process.
func (e *Engine) expectOwner(ctx context.Context, st *store.Settings) foreign.Expect {
	var exp foreign.Expect
	if st.Mihomo.Binary != "" {
		exp.Binaries = append(exp.Binaries, st.Mihomo.Binary)
	}
	if e.Kernels != nil {
		exp.Binaries = append(exp.Binaries, e.Kernels.Binary())
		exp.Dirs = append(exp.Dirs, e.Kernels.Dir)
	}
	if e.Services != nil && st.Mihomo.ServiceLabel != "" {
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if s, err := e.Services.Status(sctx, st.Mihomo.ServiceLabel); err == nil && s.PID > 0 {
			exp.PIDs = append(exp.PIDs, s.PID)
		}
		cancel()
	}
	return exp
}

// ControllerOwner reads who listens on the controller address now and
// remembers it. Without a platform lookup (PortOwner nil) or when it
// finds nothing, the verdict is Unknown, except that a SAFE_PATHS refusal
// seen earlier stays until a reload succeeds or the port owner is known.
func (e *Engine) ControllerOwner(ctx context.Context) foreign.Owner {
	st, err := e.Store.Settings()
	if err != nil {
		return foreign.Owner{Verdict: foreign.Unknown}
	}
	addr := controllerAddr(st.Mihomo.Controller)
	o := foreign.Owner{Addr: addr, Verdict: foreign.Unknown, At: e.now()}
	if port := foreign.Port(addr); port > 0 && e.PortOwner != nil {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		p, perr := e.PortOwner(pctx, port)
		cancel()
		if perr != nil {
			e.log().Debug("controller port owner", "err", perr)
		}
		o = foreign.Classify(addr, p, e.expectOwner(ctx, st), e.now())
	}
	e.setOwner(o)
	return e.Owner()
}

// Owner is the last known owner of the controller (Unknown if none).
func (e *Engine) Owner() foreign.Owner {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.owner == nil {
		return foreign.Owner{Verdict: foreign.Unknown}
	}
	return *e.owner
}

// recentOwner returns the last answer while it is fresh, else asks again.
func (e *Engine) recentOwner(ctx context.Context) foreign.Owner {
	e.mu.Lock()
	o := e.owner
	e.mu.Unlock()
	if o != nil && e.now().Sub(o.At) < ownerTTL {
		return *o
	}
	return e.ControllerOwner(ctx)
}

// setOwner stores a new answer. An Unknown answer does not clear a
// SAFE_PATHS verdict (only a known owner or a successful reload does).
// When the controller is no longer another program's and an auto-apply
// was held back, it runs now (with nothing listening, it writes the
// config for the next start).
func (e *Engine) setOwner(o foreign.Owner) {
	e.mu.Lock()
	prev := e.owner
	if o.Verdict == foreign.Unknown && prev != nil && prev.Verdict == foreign.Foreign && prev.Via == foreign.ViaSafePaths && prev.Addr == o.Addr {
		e.mu.Unlock()
		return
	}
	e.owner = &o
	var pending *rpc.ApplyParams
	if o.Verdict != foreign.Foreign && e.pendingAuto != nil {
		pending, e.pendingAuto = e.pendingAuto, nil
	}
	e.mu.Unlock()
	changed := prev == nil && o.Verdict != foreign.Unknown || prev != nil && (prev.Verdict != o.Verdict || prev.PID != o.PID)
	if changed {
		if o.Verdict == foreign.Foreign {
			e.log().Warn("the mihomo controller belongs to another program", "owner", o.Label(), "work_dir", o.WorkDir, "via", o.Via)
		}
		e.publish(rpc.EventControllerOwner, o)
	}
	if pending != nil {
		e.log().Info("the controller is Twinnel's again; running the held-back auto-apply", "reason", pending.Reason)
		e.resumed.Add(1)
		go func() {
			defer e.resumed.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if !e.adopting.Load() {
				_, _ = e.Apply(ctx, *pending)
			}
		}()
	}
}

// clearForeign forgets a SAFE_PATHS verdict after a reload went through.
func (e *Engine) clearForeign() {
	e.mu.Lock()
	if e.owner != nil && e.owner.Verdict == foreign.Foreign {
		e.owner = nil
	}
	e.mu.Unlock()
}

// holdAutoApply is the auto-apply gate: while the controller is another
// program's, an automatic apply is not attempted (it would only fail
// again, or worse); the newest one waits and runs once the controller is
// Twinnel's again. It reports whether p was held back.
func (e *Engine) holdAutoApply(ctx context.Context, p rpc.ApplyParams) bool {
	o := e.recentOwner(ctx)
	if !o.IsForeign() {
		return false
	}
	e.mu.Lock()
	e.pendingAuto = &p
	e.mu.Unlock()
	e.log().Info("auto-apply held back: the controller belongs to another program", "owner", o.Label(), "reason", p.Reason)
	return true
}
