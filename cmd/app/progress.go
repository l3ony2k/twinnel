package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/egoist/mygo/ui"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// Progress of the slow helper operations (save-perf): a wait long enough
// to show a spinner also says what it waits for.

// stageDiff is a preview's second step (configView.prevStage): loading
// the running and the composed text for the diff.
const stageDiff = "diff"

// composeStatus asks whether 应用 has work: compose.status, which skips
// mihomo -t; a helper from before it answers compose.preview instead.
func composeStatus(ctx context.Context, hc *status.HelperConn) (rpc.ComposeStatus, error) {
	var r rpc.ComposeStatus
	err := hc.Call(ctx, rpc.MethodComposeStatus, nil, &r)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == rpc.CodeMethodNotFound {
		var pr rpc.ComposeResult
		if err = hc.Call(ctx, rpc.MethodComposePreview, nil, &pr); err == nil {
			r = rpc.ComposeStatus{Hash: pr.Hash, Changed: pr.Changed}
		}
	}
	return r, err
}

// takeStages keeps the last compose.stage of evs as the running apply's
// stage and returns the other events: stages are progress, not history,
// so the log and the reloads never see them. Main thread.
func (a *app) takeStages(evs []rpc.Event) []rpc.Event {
	out := evs[:0:0]
	for _, e := range evs {
		if e.Type != rpc.EventComposeStage {
			out = append(out, e)
			continue
		}
		var cs rpc.ComposeStage
		if json.Unmarshal(e.Data, &cs) == nil {
			a.hd.applyStage = cs.Stage
			a.invalidate()
		}
	}
	return out
}

// composeProgress words what a running apply or preview does, once its
// button shows the spinner; "" otherwise.
func (a *app) composeProgress() string {
	T := i18n.T
	switch {
	case a.opSpinning(T.Apply):
		return stageText(a.hd.applyStage)
	case a.opSpinning(T.Preview):
		if a.hd.cfg.prevStage == stageDiff {
			return T.StageDiff
		}
		return T.StageValidate
	}
	return ""
}

// stageText words an apply's stage; before the first one it is checking.
func stageText(stage string) string {
	T := i18n.T
	switch stage {
	case rpc.StageWrite:
		return T.StageWrite
	case rpc.StageReload:
		return T.StageReload
	case rpc.StageVerify:
		return T.StageVerify
	}
	return T.StageValidate
}

// saveButton is the primary 保存 for helper operation op, disabled
// while disabled: while op runs long enough for a spinner (busy.go) it
// shows one with 正在保存…, so the wait says what it is.
func (a *app) saveButton(c *ui.Context, op string, disabled bool) ui.Element {
	T := i18n.T
	if a.hd.busy == op {
		a.hd.busyOnButton = true
	}
	spin := a.opSpinning(op)
	b := busyFace(c, ui.PrimaryButton(c, "").Label(T.Save), true, disabled, spin)
	return b.Busy(spin).Children(func() {
		if spin {
			onBusyFace(c, spinnerIcon(c), true, spin)
			ui.Text(c, T.Saving).SingleLine()
			return
		}
		ui.Text(c, T.Save).SingleLine()
	})
}
