package main

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// Reversible actions on the 订阅与覆写 page happen at once and offer 撤销
// in a toast instead of asking first (kubai R-8, tips 009 and 016):
// 删除覆写, 放弃改动, 还原为已应用的版本 and 恢复这些覆写的状态. None of
// them touches what mihomo runs; they change the overrides, which the
// helper can be given back. Before the action the page reads what it may
// change (every stored override with its text, the managed layers'
// settings and the subscription in use); 撤销 writes that back.

// undoFor is how long a toast offers 撤销.
const undoFor = 8 * time.Second

// undoState is the overrides as they were before a reversible action.
type undoState struct {
	layers    []rpc.LayerContent // the stored overrides, in order
	coexist   bool
	guard     []string
	activeSub string
}

// undoToast is a toast waiting for the page's next frame.
type undoToast struct {
	title string
	u     *undoState
	at    time.Time
}

// snapshotOverrides reads what a reversible action may change. Off the
// main thread, inside the action's helperOp.
func snapshotOverrides(ctx context.Context, hc *status.HelperConn) (*undoState, error) {
	var (
		ls   []rpc.Layer
		set  rpc.Settings
		subs []rpc.Subscription
	)
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &ls); err != nil {
		return nil, err
	}
	if err := hc.Call(ctx, rpc.MethodSettingsGet, nil, &set); err != nil {
		return nil, err
	}
	if err := hc.Call(ctx, rpc.MethodSubList, nil, &subs); err != nil {
		return nil, err
	}
	u := &undoState{coexist: set.TailscaleCoexist, guard: slices.Clone(set.LeakGuardGroups)}
	for _, s := range subs {
		if s.Active {
			u.activeSub = s.ID
		}
	}
	for _, l := range ls {
		if l.Managed {
			continue
		}
		var lc rpc.LayerContent
		if err := hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: l.ID}, &lc); err != nil {
			return nil, err
		}
		lc.Layer = l // the list's hash and switch
		u.layers = append(u.layers, lc)
	}
	return u, nil
}

// restoreOverrides writes u back: overrides made since are deleted, the
// ones changed or deleted are written again with their text, switch and
// place, then the managed layers' settings and the subscription. Only
// what differs is written. An override deleted and written back again
// keeps its name but not a generated name's translation (the helper
// stores only the English name for a new layer).
func restoreOverrides(ctx context.Context, hc *status.HelperConn, u *undoState) error {
	var cur []rpc.Layer
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &cur); err != nil {
		return err
	}
	byID := map[string]rpc.Layer{}
	for _, l := range cur {
		if !l.Managed {
			byID[l.ID] = l
		}
	}
	keep := map[string]bool{}
	for _, l := range u.layers {
		keep[l.ID] = true
	}
	for _, l := range cur {
		if !l.Managed && !keep[l.ID] {
			if err := hc.Call(ctx, rpc.MethodLayerDelete, rpc.IDParams{ID: l.ID}, nil); err != nil {
				return err
			}
		}
	}
	for _, l := range u.layers {
		now, ok := byID[l.ID]
		if ok && now.Hash == l.Hash && now.Enabled == l.Enabled && now.Stage == l.Stage && now.Format == l.Format {
			continue
		}
		on := l.Enabled
		p := rpc.LayerPutParams{ID: l.ID, Stage: l.Stage, Format: l.Format, Enabled: &on, Content: l.Content}
		if !ok || now.Name != l.Name {
			p.Name = l.Name
		}
		if err := hc.Call(ctx, rpc.MethodLayerPut, p, nil); err != nil {
			return err
		}
	}
	order := make([]string, len(u.layers))
	for i, l := range u.layers {
		order[i] = l.ID
	}
	var after []rpc.Layer
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &after); err != nil {
		return err
	}
	var got []string
	for _, l := range after {
		if !l.Managed {
			got = append(got, l.ID)
		}
	}
	if !slices.Equal(got, order) {
		if err := hc.Call(ctx, rpc.MethodLayerReorder, rpc.ReorderParams{IDs: order}, nil); err != nil {
			return err
		}
	}
	var set rpc.Settings
	if err := hc.Call(ctx, rpc.MethodSettingsGet, nil, &set); err != nil {
		return err
	}
	var up rpc.SettingsUpdate
	if set.TailscaleCoexist != u.coexist {
		up.TailscaleCoexist = &u.coexist
	}
	if !slices.Equal(set.LeakGuardGroups, u.guard) {
		g := slices.Clone(u.guard)
		if g == nil {
			g = []string{}
		}
		up.LeakGuardGroups = &g
	}
	if up.TailscaleCoexist != nil || up.LeakGuardGroups != nil {
		if err := hc.Call(ctx, rpc.MethodSettingsUpdate, up, nil); err != nil {
			return err
		}
	}
	if u.activeSub != "" {
		var subs []rpc.Subscription
		if err := hc.Call(ctx, rpc.MethodSubList, nil, &subs); err != nil {
			return err
		}
		for _, s := range subs {
			if s.ID == u.activeSub && !s.Active {
				return hc.Call(ctx, rpc.MethodSubActivate, rpc.IDParams{ID: s.ID}, nil)
			}
		}
	}
	return nil
}

// reversibleOp runs a reversible action: it reads the overrides, runs
// do, and on success says what it did in a toast with 撤销 (instead of
// helperOp's own toast). done, when set, runs after on the main thread.
func (a *app) reversibleOp(label, doneText string, do func(ctx context.Context, hc *status.HelperConn) error, done func(error)) {
	var u *undoState
	a.helperOp(label, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		var err error
		if u, err = snapshotOverrides(ctx, hc); err != nil {
			return err
		}
		return do(ctx, hc)
	}, func(err error) {
		if err == nil {
			a.toast, a.toastOp = "", false // this toast says it, with 撤销
			v := &a.hd.cfg
			v.undo = u
			v.toastUndo = &undoToast{title: doneText, u: u, at: time.Now()}
		}
		if done != nil {
			done(err)
		}
	})
}

// flushUndoToast shows the waiting undo toast; the page calls it each
// frame. One left from a page shown too late to be useful is dropped.
func (a *app) flushUndoToast(c *ui.Context) {
	v := &a.hd.cfg
	ut := v.toastUndo
	if ut == nil {
		return
	}
	v.toastUndo = nil
	if time.Since(ut.at) > time.Second {
		return
	}
	T := i18n.T
	c.AddToast(ui.Toast{
		ID: "undo", Title: ut.title, Type: "success", Timeout: undoFor,
		Action: T.Undo, OnAction: func() { a.undoLast(ut.u) },
		OnClose: func() {
			if v.undo == ut.u {
				v.undo = nil
			}
		},
	})
}

// undoLast writes back the overrides an action changed.
func (a *app) undoLast(u *undoState) {
	T := i18n.T
	v := &a.hd.cfg
	if u == nil || v.undo != u {
		return
	}
	v.undo = nil
	a.helperOp(T.UndoOp, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return restoreOverrides(ctx, hc, u)
	}, func(err error) {
		if err != nil {
			return
		}
		a.toast, a.toastOp = T.Undone, false
		if v.sel != "" && !v.isNew {
			v.ed = nil // the editor reads the layer back
			a.loadLayerContent(v.sel)
		}
	})
}
