package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
)

// controlShots (DUET_SHOTS_FLOW=controls) renders the states of
// pending.go and states.go that a live run rarely holds long enough to
// capture: a pending switch and a failed one in the panel, a busy button,
// skeleton rows, a loading box, the unreachable states, the unapplied
// mark. Hidden windows, captured from memory, like every shot.
func (a *app) controlShots(save func(*mygo.Window, string)) {
	if os.Getenv("DUET_SHOTS_WRITE") == "1" && a.cfg.AllowWrites && a.cfg.Controller != "" &&
		!strings.HasSuffix(a.cfg.Controller, ":9090") { // a fake controller only, never the live one
		a.optimisticShot(save)
	}
	var win *mygo.Window
	mygo.RunOnMain(func() {
		T := i18n.T
		s := a.mon.Snapshot()
		// The panel: TUN pending on (the call still runs), the mode's last
		// change failed.
		a.pend.vals = map[string]*pendingEntry{pendTun: {val: !s.Mihomo.Config.Tun.Enable, inflight: 1}}
		a.pend.errs = map[string]string{pendMode: i18n.F(T.ModeFailed, "connection refused")}
		// A helper operation whose spinner shows on its button.
		a.hd.busy, a.hd.spanOp = T.Apply, T.Apply
		a.hd.span = busySpan{start: time.Now().Add(-time.Second)}
		a.hd.unapplied, a.hd.loaded = true, false
		a.hd.loadStart = time.Now().Add(-time.Second)
		a.togglePanel()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "controls", Hidden: true, Width: a.winWidth(), Height: 1100,
			Content: ui.View(a.controlsGallery),
		})
	})
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		save(panel, "panel-states")
	}
	save(win, "controls-gallery")
	mygo.RunOnMain(func() {
		a.hd.busy, a.hd.spanOp, a.hd.span = "", "", busySpan{}
		a.pend = pendingState{}
		win.Close()
	})
}

func (a *app) controlsGallery(c *ui.Context) {
	a.applyTheme(c)
	t := c.Theme()
	T := i18n.T
	c.Root().Background(t.Background)
	ui.Column(c).Padding(24, plat.pagePadX).Gap(16).Fill().Children(func() {
		listCard(c, func() {
			ui.Text(c, T.PreviewApply).FontWeight(plat.headingWeight)
			a.unappliedNote(c)
			ui.Spacer(c)
			a.opButton(c.Key("idle"), iconPreview, T.Preview, false, T.Preview)
			a.opButton(c.Key("busy"), iconOK, T.Apply, true, T.Apply)
		}, func() {
			if !a.helperListsLoading(c, 3) {
				small(c, "loaded")
			}
		})
		a.loadingBox(c, "gallery", 120)
		a.controlError(c, pendMode)
		ui.Row(c).Gap(16).Children(func() {
			ui.Column(c).Basis(0).Grow(1).Children(func() {
				a.unreachableState(c, svcMihomo, "dial tcp 127.0.0.1:9090: connect: connection refused")
			})
			ui.Column(c).Basis(0).Grow(1).Children(func() {
				a.unreachableState(c.Key("ts"), svcTailscale, "dial unix /var/run/tailscaled.socket: connect: no such file or directory")
			})
		})
		groupBox(c, t).Children(func() {
			unreachableLine(c, svcMihomo, "connection refused")
			unreachableLine(c.Key("ts-line"), svcTailscale, "no such file or directory")
		})
	})
}

// optimisticShot changes the mode through the panel's own path against a
// fake controller: the panel must show the new mode in the very next
// frame, before the controller answers, and still show it once the
// snapshot agrees. It logs what it saw and puts the mode back.
func (a *app) optimisticShot(save func(*mygo.Window, string)) {
	var before mihomo.Mode
	mygo.RunOnMain(func() {
		before = a.mon.Snapshot().Mihomo.Config.Mode
		a.togglePanel()
	})
	time.Sleep(800 * time.Millisecond)
	next := mihomo.ModeGlobal
	if before == next {
		next = mihomo.ModeRule
	}
	var shown mihomo.Mode
	mygo.RunOnMain(func() {
		a.optimistic(pendMode, next, i18n.T.ModeFailed, func(ctx context.Context) error {
			time.Sleep(time.Second) // a slow controller
			return a.mh.SetMode(ctx, next)
		})
		shown = pendingValue(a, pendMode, a.mon.Snapshot().Mihomo.Config.Mode)
	})
	time.Sleep(200 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	save(panel, "panel-mode-pending")
	time.Sleep(3 * time.Second)
	var snap mihomo.Mode
	var left bool
	mygo.RunOnMain(func() {
		snap = a.mon.Snapshot().Mihomo.Config.Mode
		left = a.pend.vals[pendMode] != nil
		pendingValue(a, pendMode, snap)
		left = left && a.pend.vals[pendMode] != nil
	})
	save(panel, "panel-mode-done")
	log.Printf("optimistic: before=%s chosen=%s shown at once=%s snapshot after=%s still pending=%v err=%q",
		before, next, shown, snap, left, a.controlFailed(pendMode))
	mygo.RunOnMain(func() {
		a.optimistic(pendMode, before, i18n.T.ModeFailed, func(ctx context.Context) error { return a.mh.SetMode(ctx, before) })
		a.panel.Close()
	})
	time.Sleep(time.Second)
}
