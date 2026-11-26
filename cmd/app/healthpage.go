package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// HealthRefresh is how often the health page reruns the checks while it
// shows (the monitor also runs them at start and every 10 minutes).
const HealthRefresh = 60 * time.Second

// healthStatusLook returns the icon, color and label of a check status.
func healthStatusLook(t *ui.Theme, s health.Status) (*ui.SVG, ui.Color, string) {
	T := i18n.T
	switch s {
	case health.StatusOK:
		return iconOK, t.Success, T.HealthOK
	case health.StatusWarn:
		return iconWarning, t.Warning, T.HealthWarn
	case health.StatusFail:
		return iconError, t.Danger, T.HealthFail
	}
	return iconSkip, t.TextMuted, T.HealthSkip
}

// iconButton is a button with an icon before its label.
func iconButton(c *ui.Context, icon *ui.SVG, label string, primary bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = ui.Button(c, "")
	}
	return b.Label(label).Children(func() {
		ui.Icon(c, icon).FontSize(14)
		ui.Text(c, label).SingleLine()
	})
}

func copyText(c *ui.Context, text string) {
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
		c.Toast(i18n.T.ErrorPrefix + err.Error())
		return
	}
	c.Toast(i18n.T.Copied)
}

// rerunHealth runs the checks now: in the helper when connected (instead
// of only reading its last report), else in-process.
func (a *app) rerunHealth() {
	if a.health != nil {
		a.health.fresh.Store(true)
	}
	a.runHealth()
}

// runHealth refreshes the health report in the background: the helper's
// last report when connected, else an in-process run.
func (a *app) runHealth() {
	mon := a.mon
	go mon.RunHealth(context.Background())
}

func (a *app) healthPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	h := s.Health
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		pageTitle(c, T.PageHealth)
		ui.Spacer(c)
		if h.Report != nil {
			ui.Text(c, fmt.Sprintf(T.HealthLastRun, h.Report.Started.Format("15:04:05"),
				fmt.Sprintf(T.Seconds, h.Report.Duration.Seconds()))).FontSize(12).TextColor(t.TextMuted).SingleLine()
			if a.health != nil && a.health.FromHelper() {
				ui.Badge(c, T.HealthFromHelper)
			}
			if iconButton(c.Key("copy-report"), iconCopy, T.HealthCopyReport, false).Clicked() {
				copyText(c, h.Report.Markdown())
			}
		}
		if a.healthSrc != nil {
			label := T.HealthRerun
			if h.Running {
				label = T.HealthRunning
			}
			if iconButton(c.Key("rerun"), iconRefresh, label, true).Disabled(h.Running).Clicked() {
				a.rerunHealth()
			}
		}
	})
	switch {
	case a.healthSrc == nil:
		card(c, "", func() { ui.Text(c, T.HealthUnavailable).TextColor(t.TextMuted) })
		return
	case h.Report == nil && h.Err != "":
		card(c, "", func() { ui.Text(c, fmt.Sprintf(T.HealthError, h.Err)).TextColor(t.Danger).Wrap() })
		return
	case h.Report == nil:
		card(c, "", func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, T.HealthRunning).TextColor(t.TextMuted)
			})
		})
		return
	}
	rep := h.Report
	cnt := rep.Counts()
	card(c, "", func() {
		icon, col, label := healthStatusLook(t, rep.Overall)
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(24).TextColor(col)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, label).FontSize(16).Bold()
				ui.Text(c, fmt.Sprintf(T.HealthSummary, cnt[health.StatusOK], cnt[health.StatusWarn],
					cnt[health.StatusFail], cnt[health.StatusSkip])).FontSize(12).TextColor(t.TextMuted)
			})
		})
		if h.Err != "" {
			ui.Text(c, fmt.Sprintf(T.HealthError, h.Err)).FontSize(12).TextColor(t.Danger).Wrap()
		}
	})
	for _, cr := range rep.Checks {
		a.healthCheckCard(c.Key("check:"+cr.ID), cr)
	}
}

func (a *app) healthCheckCard(c *ui.Context, cr health.CheckReport) {
	t := c.Theme()
	T := i18n.T
	open, set := a.healthOpen[cr.ID]
	if !set { // problems start expanded
		open = cr.Status == health.StatusWarn || cr.Status == health.StatusFail
	}
	expandable := len(cr.Evidence) > 0 || cr.Fix != nil
	icon, col, _ := healthStatusLook(t, cr.Status)
	quiet := cr.Status == health.StatusOK || cr.Status == health.StatusSkip
	ui.Column(c).Padding(12, 16).Gap(8).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
		head := ui.Row(c).Gap(10).AlignItems(ui.Center)
		head.Children(func() {
			ui.Icon(c, icon).FontSize(16).TextColor(col)
			ui.Text(c, cr.Title).FontSize(13).Bold().SingleLine().Shrink(0)
			if quiet && !open {
				// Passing checks fold into one line: title, then the summary.
				ui.Text(c, cr.Summary).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1)
			} else {
				ui.Text(c, cr.ID).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			if expandable {
				chev := iconCollapsed
				if open {
					chev = iconExpanded
				}
				ui.Icon(c, chev).FontSize(14).TextColor(t.TextMuted)
			}
		})
		if expandable {
			head.Cursor(ui.CursorPointer)
			if head.Clicked() {
				a.healthOpen[cr.ID] = !open
			}
		}
		if quiet && !open {
			return
		}
		// Summary, indented under the title.
		ui.Text(c, cr.Summary).FontSize(13).Wrap().Selectable().MarginStart(26)
		if !open || !expandable {
			return
		}
		ui.Column(c).Gap(8).MarginStart(26).Children(func() {
			if len(cr.Evidence) > 0 {
				ui.Text(c, T.HealthEvidence).FontSize(11).TextColor(t.TextMuted)
				ui.Column(c).Gap(3).Padding(10, 12).Radius(6).Background(t.Background).Border(1, t.Border).Children(func() {
					for _, e := range cr.Evidence {
						ui.Text(c, e).Font("monospace").FontSize(11).Wrap().Selectable()
					}
				})
			}
			if f := cr.Fix; f != nil {
				ui.Text(c, T.HealthFix).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, f.Text).FontSize(12).Wrap().Selectable()
				if f.Command != "" {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "$ "+strings.ReplaceAll(f.Command, "\n", "\n$ ")).Font("monospace").FontSize(12).
							Padding(8, 10).Radius(6).Background(t.Background).Border(1, t.Border).Grow(1).Wrap().Selectable()
						if iconButton(c.Key("copy-cmd"), iconCopy, T.CopyCmd, false).Clicked() {
							copyText(c, f.Command)
						}
					})
				}
			}
		})
	})
}

// healthLine is the panel's one line about the health checks; clicking it
// opens the health page.
func (a *app) healthLine(c *ui.Context, s status.Snapshot) {
	if a.healthSrc == nil {
		return
	}
	t := c.Theme()
	T := i18n.T
	h := s.Health
	var icon *ui.SVG
	var col ui.Color
	var text string
	warn, fail := h.Counts()
	switch {
	case h.Report == nil:
		icon, col, text = iconHealth, t.TextMuted, T.PanelHealthPending
	case fail > 0:
		icon, col, text = iconError, t.Danger, fmt.Sprintf(T.PanelHealthFail, fail)
	case warn > 0:
		icon, col, text = iconWarning, t.Warning, fmt.Sprintf(T.PanelHealthWarn, warn)
	default:
		icon, col, text = iconOK, t.Success, T.PanelHealthOK
	}
	row := ui.Row(c.Key("health-line")).Gap(8).AlignItems(ui.Center).Padding(6, 8).MarginX(-8).Radius(plat.controlRadius).Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		ui.Text(c, text).Grow(1).SingleLine()
		ui.Icon(c, iconCollapsed).FontSize(12).TextColor(t.TextMuted)
	})
	if row.Hovered() {
		row.Background(t.Text.Alpha(0.08))
	}
	if row.Clicked() {
		old := a.page
		a.page = pageHealth
		a.openMain()
		a.pageChanged(old)
	}
}
