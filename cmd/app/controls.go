package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
)

// The v0.2 controls. Each is enabled when what it acts on can act
// (status.Snapshot.Can: mihomo's controller, Tailscale's LocalAPI, the
// helper); DUET_READ_ONLY=1 turns them all off for development and test
// runs, and the clients refuse writes on their own then. The switches and selects
// are optimistic (pending.go): they show the new value at once and go back,
// with controlError's note by them, if the change fails. Callers draw
// a.controlError(c, key) after the row that holds the control.

// tunOn is TUN's state as the controls show it (pending value first).
func (a *app) tunOn(s status.Snapshot) bool {
	return pendingValue(a, pendTun, s.Mihomo.Config.Tun.Enable)
}

// tunSwitch turns mihomo's TUN on or off.
func (a *app) tunSwitch(c *ui.Context, s status.Snapshot) {
	on := a.tunOn(s)
	if ui.Switch(c.Key("tun"), &on).Label(i18n.T.Tun).Disabled(!s.Can().Mihomo).Changed() {
		a.setTun(on)
	}
}

// setTun turns TUN on or off, optimistically (pending.go).
func (a *app) setTun(on bool) {
	a.optimistic(pendTun, on, i18n.T.TunFailed, func(ctx context.Context) error { return a.mh.SetTun(ctx, on) })
}

// modeControl is mihomo's mode as a segmented control (规则 / 全局 /
// 直连), in the flyout and on the overview.
func (a *app) modeControl(c *ui.Context, s status.Snapshot) ui.Element {
	T := i18n.T
	idx := 0
	cur := pendingValue(a, pendMode, s.Mihomo.Config.Mode)
	labels := make([]string, len(mihomo.Modes))
	for i, md := range mihomo.Modes {
		labels[i] = T.ModeName(string(md))
		if md == cur {
			idx = i
		}
	}
	e := ui.Segmented(c.Key("mode"), &idx, labels...).Disabled(!s.Can().Mihomo)
	if e.Changed() {
		md := mihomo.Modes[idx]
		a.optimistic(pendMode, md, T.ModeFailed, func(ctx context.Context) error { return a.mh.SetMode(ctx, md) })
	}
	return e
}

// mihomoMenu is the 操作 menu of mihomo's group: reload the config, flush
// the fake-ip and DNS caches. Actions go in a menu, not among the values.
func (a *app) mihomoMenu(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	off := !s.Can().Mihomo
	ui.MenuButton(c.Key("mihomo-actions"), T.Actions, func(m *ui.Menu) {
		if !s.Mihomo.Reachable {
			// Down: start it through the helper, which knows the
			// service (an adopted one included).
			a.mihomoServiceItems(m, s)
			return
		}
		if m.Item(T.ReloadConfig).Disabled(off).Chosen() {
			a.reloadMihomo(s)
		}
		m.Separator()
		if m.Item(T.FlushFakeIP).Disabled(off).Chosen() {
			a.do(a.mh.FlushFakeIP)
		}
		if m.Item(T.FlushDNS).Disabled(off).Chosen() {
			a.do(a.mh.FlushDNS)
		}
	})
}

// reloadMihomo re-applies the config through the helper when there is
// one (validate, write, reload, verify, roll back on failure), else asks
// mihomo to reload its file.
func (a *app) reloadMihomo(s status.Snapshot) {
	if s.Helper.Connected() {
		hc := a.hc
		a.do(func(ctx context.Context) error {
			return hc.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{Reason: i18n.T.ReloadReason, Force: true}, nil)
		})
		return
	}
	a.do(func(ctx context.Context) error { return a.mh.Reload(ctx, "") })
}

// tsPowerSwitch connects or disconnects Tailscale: on is "wants to run".
func (a *app) tsPowerSwitch(c *ui.Context, s status.Snapshot) ui.Element {
	on := pendingValue(a, pendTS, s.Tailscale.State.Prefs.WantRunning)
	sw := ui.Switch(c.Key("ts-power"), &on).Label(i18n.T.TSConnect).Disabled(!s.Can().Tailscale)
	if sw.Changed() {
		fn := a.ts.Down
		if on {
			fn = a.ts.Up
		}
		a.optimistic(pendTS, on, i18n.T.TSPowerFailed, fn)
	}
	return sw
}

// exitNodeOptions returns the names offered as exit nodes: "none", then the
// peers that offer it (and the one in use).
func exitNodeOptions(st tailscale.State) []string {
	opts := []string{i18n.T.ExitNodeNone}
	for _, p := range st.Peers {
		if p.ExitNodeOption || p.ExitNode {
			opts = append(opts, p.Name)
		}
	}
	return opts
}

// exitNodePicker selects the Tailscale exit node. Choosing one while
// mihomo's TUN is on asks first: both want the default route.
func (a *app) exitNodePicker(c *ui.Context, s status.Snapshot) ui.Element {
	T := i18n.T
	st := s.Tailscale.State
	sel := T.ExitNodeNone
	if st.ExitNode != nil {
		sel = st.ExitNode.Name
	}
	sel = pendingValue(a, pendExit, sel)
	opts := exitNodeOptions(st)
	e := ui.Select(c.Key("exit-node"), &sel, opts).Label(T.ExitNode).
		Disabled(!s.Can().Tailscale || len(opts) < 2)
	if e.Changed() {
		a.chooseExitNode(st, sel, s.Mihomo.Reachable && s.Mihomo.Config.Tun.Enable)
	}
	return e
}

func (a *app) chooseExitNode(st tailscale.State, name string, tunOn bool) {
	T := i18n.T
	id := ""
	for _, p := range st.Peers {
		if p.Name == name {
			id = p.ID
		}
	}
	set := func() {
		a.optimistic(pendExit, name, T.ExitNodeFailed, func(ctx context.Context) error { return a.ts.SetExitNode(ctx, id) })
	}
	if id == "" || !tunOn {
		set()
		return
	}
	// Confirmed: the conflict is the user's choice from now on, shown as
	// accepted, not as a failure (status.Snapshot.ExitNodeAccepted).
	confirmed := func() {
		a.acceptExitNode(name)
		set()
	}
	parent := a.win
	go func() { // Dialog.Message blocks until answered
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Message:            T.ExitNodeConflictTitle,
			Detail:             fmt.Sprintf(T.ExitNodeConflictBody, name),
			Buttons:            []string{T.Cancel, T.ExitNodeConfirm},
			DestructiveButtons: []int{1},
			CancelButton:       0,
			DefaultButton:      0,
		})
		if err == nil && res.Button == 1 {
			mygo.RunOnMain(confirmed)
		}
	}()
}
