package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
)

// The v0.2 controls. Every one is disabled unless DUET_ALLOW_WRITES=1, and
// the clients refuse writes on their own as well.

// tunSwitch turns mihomo's TUN on or off.
func (a *app) tunSwitch(c *ui.Context, s status.Snapshot) {
	on := s.Mihomo.Config.Tun.Enable
	if ui.Switch(c.Key("tun"), &on).Label(i18n.T.Tun).Disabled(!s.WritesAllowed || !s.Mihomo.Reachable).Changed() {
		a.do(func(ctx context.Context) error { return a.mh.SetTun(ctx, on) })
	}
}

// mihomoActions: reload the config, flush the fake-ip and DNS caches.
func (a *app) mihomoActions(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	off := !s.WritesAllowed || !s.Mihomo.Reachable
	ui.Row(c).Gap(8).Wrap().Children(func() {
		if iconButton(c.Key("reload"), iconRefresh, T.ReloadConfig, false).Disabled(off).Clicked() {
			if s.Helper.Connected() {
				// The helper owns the config: re-apply it (validate, write,
				// reload, verify, roll back on failure).
				hc := a.hc
				a.do(func(ctx context.Context) error {
					return hc.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{Reason: T.ReloadReason, Force: true}, nil)
				})
			} else {
				a.do(func(ctx context.Context) error { return a.mh.Reload(ctx, "") })
			}
		}
		if iconButton(c.Key("flush-fakeip"), iconFlush, T.FlushFakeIP, false).Disabled(off).Clicked() {
			a.do(a.mh.FlushFakeIP)
		}
		if iconButton(c.Key("flush-dns"), iconFlush, T.FlushDNS, false).Disabled(off).Clicked() {
			a.do(a.mh.FlushDNS)
		}
	})
}

// tsPowerButton connects or disconnects Tailscale.
func (a *app) tsPowerButton(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	st := s.Tailscale.State
	label, fn := T.TSUp, a.ts.Up
	if st.Prefs.WantRunning {
		label, fn = T.TSDown, a.ts.Down
	}
	if iconButton(c.Key("ts-power"), iconPower, label, false).Disabled(!s.WritesAllowed || !s.Tailscale.Reachable).Clicked() {
		a.do(fn)
	}
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
	opts := exitNodeOptions(st)
	e := ui.Select(c.Key("exit-node"), &sel, opts).Label(T.ExitNode).
		Disabled(!s.WritesAllowed || !s.Tailscale.Reachable || len(opts) < 2)
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
	set := func() { a.do(func(ctx context.Context) error { return a.ts.SetExitNode(ctx, id) }) }
	if id == "" || !tunOn {
		set()
		return
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
			mygo.RunOnMain(set)
		}
	}()
}
