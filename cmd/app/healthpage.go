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
	pageHeader(c, T.PageHealth, func() {
		if h.Report != nil {
			if iconButton(c.Key("copy-report"), iconCopy, T.HealthCopyReport, false).Clicked() {
				copyText(c, h.Report.Markdown())
			}
		}
		if a.healthSrc != nil {
			label := T.HealthRerun
			if h.Running {
				label = T.HealthRunning
			}
			if iconButton(c.Key("rerun"), iconRefresh, label, false).Disabled(h.Running).Clicked() {
				a.rerunHealth()
			}
		}
	})
	switch {
	case a.healthSrc == nil:
		groupBox(c, t).Children(func() { emptyState(c, iconHealth, T.HealthUnavailable, "") })
		return
	case h.Report == nil && h.Err != "":
		groupBox(c, t).Children(func() { errorNote(c, fmt.Sprintf(T.HealthError, h.Err)) })
		return
	case h.Report == nil:
		groupBox(c, t).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Justify(ui.Center).Padding(20).Children(func() {
				ui.Spinner(c)
				small(c, T.HealthRunning)
			})
		})
		return
	}
	rep := h.Report
	cnt := rep.Counts()
	groupBox(c, t).Children(func() {
		icon, col, label := healthStatusLook(t, rep.Overall)
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(22).TextColor(col).Shrink(0)
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, label).FontSize(15).FontWeight(600)
				num(small(c, fmt.Sprintf(T.HealthSummary, cnt[health.StatusOK], cnt[health.StatusWarn],
					cnt[health.StatusFail], cnt[health.StatusSkip])))
				last := fmt.Sprintf(T.HealthLastRun, rep.Started.Format("15:04:05"), fmt.Sprintf(T.Seconds, rep.Duration.Seconds()))
				if a.health != nil && a.health.FromHelper() {
					last += " · " + T.HealthFromHelper
				}
				num(small(c, last))
			})
		})
		if h.Err != "" {
			errorNote(c, fmt.Sprintf(T.HealthError, h.Err))
		}
	})
	var problems, rest []health.CheckReport
	for _, cr := range rep.Checks {
		if cr.Status == health.StatusWarn || cr.Status == health.StatusFail {
			problems = append(problems, cr)
		} else {
			rest = append(rest, cr)
		}
	}
	list := func(title string, crs []health.CheckReport) {
		if len(crs) == 0 {
			return
		}
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, title).FontWeight(plat.headingWeight).PaddingX(4)
			groupBox(c, t).Padding(4, groupPadX).Gap(1).Dividers(1, pal(t).separator).Children(func() {
				for _, cr := range crs {
					a.healthCheckRow(c.Key("check:"+cr.ID), cr)
				}
			})
		})
	}
	list(T.HealthProblems, problems)
	list(T.HealthPassed, rest)
}

func (a *app) healthCheckRow(c *ui.Context, cr health.CheckReport) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	open, set := a.healthOpen[cr.ID]
	if !set { // problems start expanded
		open = cr.Status == health.StatusWarn || cr.Status == health.StatusFail
	}
	expandable := len(cr.Evidence) > 0 || cr.Fix != nil
	icon, col, _ := healthStatusLook(t, cr.Status)
	ui.Column(c).PaddingY(10).Gap(8).Children(func() {
		head := ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(20)
		head.Children(func() {
			ui.Icon(c, icon).FontSize(16).TextColor(col).Shrink(0)
			ui.Text(c, cr.Title).FontWeight(500).SingleLine().Shrink(0)
			if !open {
				ui.Text(c, cr.Summary).TextColor(t.TextMuted).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			if expandable {
				chev := iconCollapsed
				if open {
					chev = iconExpanded
				}
				ui.Icon(c, chev).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if expandable {
			head.Cursor(ui.CursorPointer)
			if head.Clicked() {
				a.healthOpen[cr.ID] = !open
			}
		}
		if !open {
			return
		}
		ui.Column(c).Gap(10).MarginStart(26).Children(func() {
			ui.Text(c, cr.Summary).Wrap().Selectable()
			if !expandable {
				return
			}
			if len(cr.Evidence) > 0 {
				ui.Column(c).Gap(6).Children(func() {
					small(c, T.HealthEvidence)
					ui.Column(c).Gap(3).Padding(10, 12).Radius(plat.controlRadius+2).Background(p.code).Border(1, p.groupLine).Children(func() {
						for _, e := range cr.Evidence {
							ui.Text(c, e).Font("monospace").FontSize(11).Wrap().Selectable()
						}
					})
				})
			}
			if f := cr.Fix; f != nil {
				ui.Column(c).Gap(6).Children(func() {
					small(c, T.HealthFix)
					ui.Text(c, f.Text).Wrap().Selectable()
					if f.Command != "" {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "$ "+strings.ReplaceAll(f.Command, "\n", "\n$ ")).Font("monospace").FontSize(12).
								Padding(8, 10).Radius(plat.controlRadius+2).Background(p.code).Border(1, p.groupLine).Grow(1).Wrap().Selectable()
							if iconButton(c.Key("copy-cmd"), iconCopy, T.CopyCmd, false).Clicked() {
								copyText(c, f.Command)
							}
						})
					}
				})
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
