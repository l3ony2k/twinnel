package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// Optimistic controls (kubai 017 e): a switch, segmented control or select
// shows the value the person chose at once, while the change goes to
// mihomo or Tailscale. The snapshot catches up a moment later. Without
// this the control redraws from the old snapshot on the next frame, flips
// back, then flips again when the refresh lands.
//
// A pending value is kept per key until a snapshot shows it, or until
// pendingHold after the last call for that key ended. A failed call drops
// it (the old value comes back) and leaves an error for controlError to
// show next to the control. All of this is main-thread state.

// Keys of the pending controls. nodeKey(group) is a group's selected node.
const (
	pendMode = "mode"
	pendTun  = "tun"
	pendTS   = "ts-power"
	pendExit = "exit-node"
)

func nodeKey(group string) string { return "node/" + group }

// pendingHold is how long a pending value waits for the snapshot to agree
// after its call succeeded.
const pendingHold = 5 * time.Second

type pendingEntry struct {
	val      any
	inflight int       // calls for this key still running
	until    time.Time // set when the last call ends: stop waiting then
}

type pendingState struct {
	vals map[string]*pendingEntry
	errs map[string]string
}

// pendingValue returns the value to draw for key: the chosen one while it
// is pending, else actual (the snapshot's). It forgets a pending value
// once the snapshot agrees or it has waited long enough.
func pendingValue[T comparable](a *app, key string, actual T) T {
	e := a.pend.vals[key]
	if e == nil {
		return actual
	}
	v, ok := e.val.(T)
	if !ok {
		delete(a.pend.vals, key)
		return actual
	}
	if e.inflight == 0 && (v == actual || time.Now().After(e.until)) {
		delete(a.pend.vals, key)
		return actual
	}
	return v
}

// optimistic records val as key's pending value and runs fn off the main
// thread. failed is the error's wording (%s: the reason), shown by the
// control through controlError.
func (a *app) optimistic(key string, val any, failed string, fn func(ctx context.Context) error) {
	if a.pend.vals == nil {
		a.pend.vals, a.pend.errs = map[string]*pendingEntry{}, map[string]string{}
	}
	e := a.pend.vals[key]
	if e == nil {
		e = &pendingEntry{}
		a.pend.vals[key] = e
	}
	e.val = val
	e.inflight++
	delete(a.pend.errs, key)
	a.invalidate()
	mon := a.mon
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := fn(ctx)
		mygo.RunOnMain(func() {
			e.inflight--
			if err != nil {
				if a.pend.vals[key] == e && e.inflight == 0 {
					delete(a.pend.vals, key)
				}
				a.pend.errs[key] = fmt.Sprintf(failed, controlReason(key, err))
			} else {
				e.until = time.Now().Add(pendingHold)
				a.markDailyUsed()
				time.AfterFunc(pendingHold+50*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
			}
			a.invalidate()
		})
		mon.Refresh()
	}()
}

// controlFailed returns key's last error, "" when its last change worked.
func (a *app) controlFailed(key string) string { return a.pend.errs[key] }

// controlError draws key's last error next to its control, nothing when
// the last change worked or none was made.
func (a *app) controlError(c *ui.Context, key string) {
	if msg := a.pend.errs[key]; msg != "" {
		errorNote(c.Key("pend-err-"+key), msg)
	}
}

// selectNode picks a group's node; the tiles and the panel select show it
// at once (pendingValue with nodeKey).
func (a *app) selectNode(group, name string) {
	a.optimistic(nodeKey(group), name, i18n.T.NodeFailed, func(ctx context.Context) error {
		return a.mh.SelectProxy(ctx, group, name)
	})
}

// --- progress on the pressed button (kubai 017 b) ---

// A short wait shows on the button that started it (busy.go): the
// timing belongs to the operation (a.hd.span), so the mark also keeps its
// minimum after the operation ended and frames are scheduled for it.

// opSpinning reports whether op's button shows its spinner: op runs (or
// ended within busyMin of its spinner appearing).
func (a *app) opSpinning(op string) bool {
	d := &a.hd
	return d.spanOp == op && d.span.visible(time.Now())
}

// opButton is iconButton for a helper operation: while helperOp(op, …)
// runs (after busyDelay, for at least busyMin) the refresh icon turns and
// any other becomes a spinner (busyIcon); the label and width stay. Drawing it also tells opNotes not
// to repeat the running operation at the top of the page.
func (a *app) opButton(c *ui.Context, icon *ui.SVG, label string, primary bool, op string) ui.Element {
	if a.hd.busy == op {
		a.hd.busyOnButton = true
	}
	spin := a.opSpinning(op)
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = pushFace(c, ui.Button(c, ""))
	}
	return b.Label(label).Busy(spin).Children(func() {
		mark := busyIcon(c, icon, spin) // the refresh icon turns, others become a spinner
		if spin && primary {            // on the accent, in the label's color
			mark.TextColor(c.Theme().AccentText)
		}
		ui.Text(c, label).SingleLine()
	})
}

// beginOp and endOp time the running helper operation for opSpinning and
// schedule the frames where the spinner appears or goes. Main thread.
func (a *app) beginOp(op string) {
	d := &a.hd
	d.spanOp, d.span, d.busyOnButton = op, busySpan{start: time.Now()}, false
	time.AfterFunc(busyDelay+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
}

func (a *app) endOp() {
	d := &a.hd
	d.span.end = time.Now()
	if left := d.span.start.Add(busyDelay + busyMin).Sub(d.span.end); left > 0 && d.span.visible(d.span.end) {
		time.AfterFunc(left+10*time.Millisecond, func() { mygo.RunOnMain(a.invalidate) })
	}
}

// markDailyUsed records the first everyday operation (关于 → 入门, step 4).
// Saved once; a failure is logged and tried again next time.
func (a *app) markDailyUsed() {
	if a.shotMode || a.cfg.DailyUsed {
		return
	}
	cfg := a.cfg
	cfg.DailyUsed = true
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		log.Printf("daily step: %v", err)
		return
	}
	a.cfg = cfg
}
