package main

// 接管 → 会自己启动的其他代理软件: the competing clients registered to
// start at boot or login (core/autostart, the helper's autostart.list). A
// takeover that only stopped them loses at the next login. 停用自启动 turns
// off the ones the helper can (disabled, not deleted) and offers 撤销 on
// the card; the others link to the system's login items or copy their
// command. The card is hidden when nothing is registered.

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// autostartView is the card's state (takeoverView.autostart).
type autostartView struct {
	list    *rpc.AutostartList
	loading bool
	loadErr string
	undo    *rpc.AutostartChangeParams // the last disable, until undone or reloaded
	done    int                        // how many the last disable turned off
}

// loadAutostarts reads autostart.list. Main thread.
func (a *app) loadAutostarts() {
	v := &a.hd.take.autostart
	if v.loading || !a.hd.connected {
		return
	}
	v.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var l rpc.AutostartList
		err := hc.Call(ctx, rpc.MethodAutostartList, nil, &l)
		mygo.RunOnMain(func() {
			v.loading = false
			if err != nil {
				v.loadErr = helperError(err)
				v.list = &rpc.AutostartList{} // an older helper: no card
				return
			}
			v.loadErr, v.list = "", &l
		})
	}()
}

// helperEntries are the enabled entries the helper can turn off.
func helperEntries(l *rpc.AutostartList) []string {
	var ids []string
	for _, e := range l.Entries {
		if e.Enabled && e.Disable == autostart.ByHelper {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func (a *app) autostartCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.hd.take.autostart
	if v.list == nil {
		if !v.loading && a.hd.connected && !a.hd.take.fixture {
			a.loadAutostarts()
		}
		return
	}
	if len(v.list.Entries) == 0 {
		return
	}
	ids := helperEntries(v.list)
	busy := a.hd.busy == T.AsDisabling
	listCard(c, a.tipHead(c, T.AsHead, T.AsTip, nil, func() {
		switch {
		case v.undo != nil:
			small(c, i18n.F(T.AsDisabled, v.done))
			if pushButton(c.Key("as-undo"), T.AsUndo).Disabled(a.hd.busy != "").Clicked() {
				a.autostartChange(*v.undo, 0)
			}
		case len(ids) > 0:
			b := busyButton(c.Key("as-disable"), T.AsDisable, false, busy, !s.WritesAllowed || a.hd.busy != "")
			if b.Clicked() && !busy {
				a.autostartChange(rpc.AutostartChangeParams{IDs: ids, Op: string(autostart.OpDisable)}, len(ids))
			}
		}
	}), func() {
		for i, e := range v.list.Entries {
			listRow(c.Key(fmt.Sprint("as", i))).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
					ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
						ui.Text(c, e.Client).SingleLine()
						what := T.AsKinds[string(e.Kind)]
						if what == "" {
							what = string(e.Kind)
						}
						sub := small(c, what+" · "+e.Name).SingleLine().Shrink(1)
						if e.Program != "" {
							sub.Tooltip(e.Program)
						}
					})
					if e.Enabled {
						switch e.Disable {
						case autostart.Guided:
							if u := systemSettingsURL(health.OpenLoginItems, runtime.GOOS); u != "" && actionLink(c.Key("as-open"), T.AsLoginItems).Clicked() {
								_ = mygo.Shell.OpenExternal(u)
							}
						case autostart.Manual:
							if e.Command != "" && actionLink(c.Key("as-copy"), T.AsCopyCmd).Clicked() {
								copyText(c, e.Command)
							}
						}
						effectChip(c, T.AsOn, "", true, false)
					} else {
						effectChip(c, T.AsOff, "", false, false)
					}
				})
			})
		}
	})
}

// autostartChange runs autostart.change; n is how many a disable turns
// off (0 for an undo). The card reloads after it.
func (a *app) autostartChange(p rpc.AutostartChangeParams, n int) {
	T := i18n.T
	v := &a.hd.take.autostart
	var r rpc.AutostartChangeResult
	a.helperOp(T.AsDisabling, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodAutostartChange, p, &r)
	}, func(err error) {
		if err == nil {
			if p.Op == string(autostart.OpDisable) {
				v.undo, v.done = r.Undo, n
			} else {
				v.undo, v.done = nil, 0
			}
		}
		v.list = nil // reload
	})
}

// autostartShots: the card with synthetic entries of this platform's
// kinds, then after 停用自启动 (撤销 offered). Nothing is sent.
func (a *app) autostartShots(save func(*mygo.Window, string), wait func(time.Duration), on func(func())) {
	v := &a.hd.take.autostart
	on(func() {
		v.list, v.undo, v.done = shotAutostarts(runtime.GOOS), nil, 0
		a.shotSection = "autostart"
	})
	wait(500 * time.Millisecond)
	save(a.win, "takeover-autostart")
	on(func() {
		ids := helperEntries(v.list)
		for i := range v.list.Entries {
			if v.list.Entries[i].Disable == autostart.ByHelper {
				v.list.Entries[i].Enabled = false
			}
		}
		v.undo, v.done = &rpc.AutostartChangeParams{IDs: ids, Op: string(autostart.OpEnable)}, len(ids)
	})
	wait(500 * time.Millisecond)
	save(a.win, "takeover-autostart-off")
}

// shotAutostarts are synthetic entries (no real paths or names).
func shotAutostarts(goos string) *rpc.AutostartList {
	var es []autostart.Entry
	switch goos {
	case "windows":
		es = []autostart.Entry{
			{ID: `scheduled-task:\mihomo-party`, Kind: autostart.ScheduledTask, Name: `\mihomo-party`, Client: "Clash Party", Program: `C:\Program Files\Clash Party\Clash Party.exe`, Enabled: true, Disable: autostart.ByHelper},
			{ID: `scheduled-task:\mihomo-party-run`, Kind: autostart.ScheduledTask, Name: `\mihomo-party-run`, Client: "Clash Party", Enabled: true, Disable: autostart.ByHelper},
			{ID: "run-key:Clash Verge", Kind: autostart.RunKey, Name: "Clash Verge", Client: "Clash Verge", Program: `C:\Program Files\Clash Verge\clash-verge.exe`, Enabled: false, Disable: autostart.ByHelper},
		}
	case "linux":
		es = []autostart.Entry{
			{ID: "systemd-system:clash-verge-service.service", Kind: autostart.SystemdSystem, Name: "clash-verge-service.service", Client: "Clash Verge", Enabled: true, Disable: autostart.ByHelper},
			{ID: "systemd-user:example:mihomo.service", Kind: autostart.SystemdUser, Name: "mihomo.service", Client: "mihomo", Enabled: true, Disable: autostart.Manual, Command: "systemctl --user disable --now mihomo.service"},
		}
	default:
		es = []autostart.Entry{
			{ID: "launch-agent:501:io.github.clash-verge-rev", Kind: autostart.LaunchAgent, Name: "io.github.clash-verge-rev", Client: "Clash Verge", Enabled: true, Disable: autostart.ByHelper},
			{ID: "login-item:ClashX Meta", Kind: autostart.LoginItem, Name: "ClashX Meta", Client: "ClashX", Enabled: true, Disable: autostart.Guided, Open: autostart.LoginItemsSettings},
		}
	}
	return &rpc.AutostartList{Supported: true, Entries: es}
}

// autostartFlow (TWINNEL_SHOTS_FLOW=autostart): the 接管 page's card on
// its own, with synthetic entries.
func (a *app) autostartFlow(save func(*mygo.Window, string)) {
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.win.Invalidate() }) }
	on(func() {
		a.shotSnap = func(s *status.Snapshot) {
			s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
			s.WritesAllowed, s.Loaded = true, true
			status.Evaluate(s)
		}
		a.openPage(pageTakeover)
	})
	a.autostartShots(save, func(d time.Duration) { time.Sleep(d) }, on)
	on(func() { a.shotSection = "" })
}
