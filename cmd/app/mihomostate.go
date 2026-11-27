package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// Twinnel's mihomo in the window. Every surface that shows its state (the
// sidebar's and the rail's marks, the flyout's module, the 总览 card, the
// tiles, the traffic chart, the 节点 page) renders status.MihomoView, never
// Mihomo.Reachable: when another program's mihomo holds the controller,
// the controller answers, but for that program (Windows QA round 2, E).
// TestNoRawMihomoReachable keeps it so.

// mihomoMark is the mark and words of Twinnel's mihomo: a filled dot
// running, an empty ring down, and while another program's mihomo holds
// the controller a neutral ring with a bar (⊖): not Twinnel's fault, not
// running for Twinnel either.
func mihomoMark(t *ui.Theme, v status.MihomoView) (stateMark, ui.Color, string) {
	switch v.Phase {
	case status.MihomoRunning:
		return markOK, stateMarkColor(t, markOK), v.Text()
	case status.MihomoForeign:
		return markForeign, stateMarkColor(t, markForeign), v.Text()
	}
	return markDown, stateMarkColor(t, markDown), v.Text()
}

// mihomoLockTip is why a control that acts on mihomo is off: another
// program's mihomo is on the controller ("" otherwise: the control's own
// state says it).
func mihomoLockTip(s status.Snapshot) string {
	if s.MihomoView().Foreign() {
		return i18n.T.MihomoForeignLocked
	}
	return ""
}

// lockTip sets e's tooltip to tip when there is one.
func lockTip(e ui.Element, tip string) ui.Element {
	if tip != "" {
		e.Tooltip(tip)
	}
	return e
}

// controllerOwnerCheck is the controller-owner row of the health report,
// if it failed: the fixes of the foreign state live there.
func controllerOwnerCheck(s status.Snapshot) (health.CheckReport, bool) {
	rep := s.Health.Report
	if rep == nil {
		return health.CheckReport{}, false
	}
	for _, cr := range rep.Checks {
		if cr.ID == (health.ControllerOwnerCheck{}).ID() && cr.Status == health.StatusFail {
			return cr, true
		}
	}
	return health.CheckReport{}, false
}

// mihomoRestartHelps reports whether starting or restarting Twinnel's
// mihomo can change anything: not while another program's mihomo holds
// the controller (Twinnel's could not bind the port; the ways out are
// foreignFixes). Every 启动/重启 mihomo control asks it.
func mihomoRestartHelps(s status.Snapshot) bool { return !s.MihomoView().Foreign() }

// canRestartMihomo: the helper can restart mihomo now, and it would help.
func (a *app) canRestartMihomo(s status.Snapshot) bool {
	return s.Can().Helper && a.hd.busy == "" && mihomoRestartHelps(s)
}

// foreignFixes are the foreign state's two ways out, the controller-owner
// row's fixes: 换个控制端口 (its one-click fix, previewed on the health
// page as every fix is; the step before it when one is missing) and 怎么退出它
// (the row opened, with the steps for this platform). The 总览 card, the
// flyout and the 总览 banner all offer these, in this order; nil unless
// the view is foreign.
func (a *app) foreignFixes(s status.Snapshot) []bannerAct {
	if !s.MihomoView().Foreign() {
		return nil
	}
	T := i18n.T
	openRow := func() {
		a.openPage(pageHealth)
		a.healthOpen[(health.ControllerOwnerCheck{}).ID()] = true
	}
	var acts []bannerAct
	if cr, ok := controllerOwnerCheck(s); ok {
		for _, rf := range rowFixes(cr, reportPlatform(s)) {
			if rf.reason != health.ReasonOwnerPortShared || rf.spec.Kind != health.FixAuto {
				continue
			}
			if move := a.fixButton(cr, rf, s); move != nil {
				label := move.label
				if label == T.FixDo {
					label = T.ForeignMovePort
				}
				run := move.run
				acts = append(acts, bannerAct{key: "foreign-move", label: label, off: move.off, run: func() { openRow(); run() }})
			}
		}
	}
	return append(acts, bannerAct{key: "foreign-quit", label: T.ForeignQuitHow, run: openRow})
}

// foreignActions renders foreignFixes as buttons (the card, the flyout,
// a page's foreign state); height 0 is the default.
func (a *app) foreignActions(c *ui.Context, s status.Snapshot, height float32) {
	ui.Row(c.Key("foreign-acts")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		for _, act := range a.foreignFixes(s) {
			b := pushButton(c.Key(act.key), act.label).Disabled(act.off)
			if height > 0 {
				b.Height(height)
			}
			if b.Clicked() {
				act.run()
			}
		}
	})
}

// foreignCardBody is the 总览 mihomo card while another program's mihomo
// holds the controller: the state with its mark, the address and the
// process that holds it, and the two ways out. None of the other
// program's values (version, ports, TUN, mode) are shown as Twinnel's.
func (a *app) foreignCardBody(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	v := s.MihomoView()
	ui.Row(c.Key("foreign")).Gap(12).AlignItems(ui.Center).MinHeight(40).Label(v.Text()).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).Size(40, 40).Radius(plat.innerRadius).Background(pal(t).code).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
			ui.Icon(c, iconSkip).FontSize(iconNav).TextColor(t.TextMuted)
		})
		ui.Text(c, v.Text()).FontWeight(500).Wrap().LineHeight(lineText).Grow(1).Shrink(1).MinWidth(0)
	})
	// The rows every surface lists (the CLI's status too): the address,
	// the process, what started it.
	for _, f := range v.Facts() {
		if f.Key == status.FactController {
			kvMono(c, f.Label, f.Value)
			continue
		}
		kvWith(c, f.Label, func() { middleText(c, f.Value).Selectable().Tooltip(f.Value) })
	}
	a.foreignActions(c, s, 0)
}

// foreignPageState is a page's state while another program's mihomo holds
// the controller (节点 and the pages that read mihomo): what holds it and
// the two ways out, at the size of the unreachable state.
func (a *app) foreignPageState(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	v := s.MihomoView()
	groupBox(c, t).MinHeight(unreachablePageH).Justify(ui.Center).Children(func() {
		ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
			ui.Icon(c, iconSkip).FontSize(iconLarge).TextColor(t.TextMuted)
			ui.Text(c, v.Text()).FontWeight(500).TextAlign(ui.Center).Wrap().LineHeight(lineText).MaxWidth(420)
			if d := v.Detail(); d != "" {
				monoText(c, d).FontSize(plat.smallSize).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().LineHeight(lineText).MaxWidth(420).Selectable()
			}
			ui.Row(c).Margin(8, 0, 0, 0).Children(func() { a.foreignActions(c, s, 0) })
		})
	})
}
