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
		b = pushFace(c, ui.Button(c, ""))
	}
	return b.Label(label).Children(func() {
		ui.Icon(c, icon).FontSize(iconInline)
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
		// Failed, page size (022 d): what did not happen and that nothing
		// changed, the raw error secondary and selectable, and a retry.
		groupBox(c, t).Children(func() {
			ui.Column(c).Gap(10).AlignItems(ui.Center).Children(func() {
				emptyState(c, iconError, T.HealthFailedTitle, T.HealthFailedHint)
				ui.Text(c, h.Err).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).TextAlign(ui.Center).
					Wrap().MaxWidth(520).Selectable()
				if a.healthSrc != nil && iconButton(c.Key("retry"), iconRefresh, T.HealthRerun, false).Disabled(h.Running).Clicked() {
					a.rerunHealth()
				}
				ui.Box(c).Height(16)
			})
		})
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
			ui.Icon(c, icon).FontSize(iconLarge).TextColor(col).Shrink(0)
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, healthHeadline(label, cnt)).FontSize(plat.leadSize).FontWeight(600)
				num(small(c, healthCountsText(cnt)))
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
		// The same heading as every other group: lined up with the
		// labels in its box (groupPadX).
		listCard(c, listHead(c, nil, title, func() {
			num(small(c, fmt.Sprint(len(crs))))
		}), func() {
			for _, cr := range crs {
				a.healthCheckRow(c.Key("check:"+cr.ID), cr)
			}
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
	_, _, stateText := healthStatusLook(t, cr.Status)
	ui.Column(c).PaddingY(10).Gap(8).Clip().Transition(expandFor(open)).Children(func() {
		head := ui.Row(c.Key("head")).Gap(10).AlignItems(ui.Center).MinHeight(20).Label(stateText + "：" + cr.Title)
		head.ContextMenu(func(m *ui.Menu) {
			if m.Item(T.HealthCopySummary).Chosen() {
				copyText(c, checkText(cr))
			}
			if f := cr.Fix; f != nil && f.Command != "" && m.Item(T.CopyCmd).Chosen() {
				copyText(c, f.Command)
			}
		})
		head.Children(func() {
			ui.Icon(c, icon).FontSize(iconNav).TextColor(col).Shrink(0).AccessHidden(true)
			ui.Text(c, cr.Title).FontWeight(500).SingleLine().Shrink(0)
			switch {
			case !expandable:
				// Nothing to open, so nothing may be cut off: the
				// sentence wraps under itself.
				ui.Text(c, cr.Summary).TextColor(t.TextMuted).Wrap().Shrink(1).Selectable()
			case !open:
				// Cut to one line; the whole sentence on hover, as
				// macOS shows a truncated cell's text.
				ui.Text(c, cr.Summary).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(cr.Summary)
			}
			ui.Spacer(c)
			if expandable {
				disclosure(c, open).Shrink(0)
			}
		})
		if expandable {
			head.Cursor(ui.CursorPointer).Focusable().Role(ui.RoleDisclosure).Expanded(open)
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
					ui.Column(c).Gap(3).Padding(10, 12).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Children(func() {
						evidenceLines(c, cr.Evidence)
					})
				})
			}
			if f := cr.Fix; f != nil {
				ui.Column(c).Gap(6).Children(func() {
					small(c, T.HealthFix)
					ui.Text(c, f.Text).Wrap().Selectable()
					if f.Command != "" {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "$ "+strings.ReplaceAll(f.Command, "\n", "\n$ ")).Font("monospace").FontSize(plat.calloutSize).
								Padding(8, 10).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Grow(1).Wrap().Selectable()
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
	row := ui.Row(c.Key("health-line")).Gap(8).AlignItems(ui.Center).Padding(6, 8).MarginX(-8).Radius(plat.controlRadius).Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(text)
	row.Children(func() {
		ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
		ui.Text(c, text).Grow(1).SingleLine()
		ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
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

// healthCountsText is the report's counts: 通过 always, the others only
// when there are some (review-a: no zero counts).
// healthHeadline is the health report's headline in the words the
// overview and the flyout use for it (健康检查：2 项失败, 健康检查：全部通过),
// so one report reads the same everywhere. label is the status word for a
// report with nothing failed, warned or passed (跳过).
func healthHeadline(label string, cnt map[health.Status]int) string {
	T := i18n.T
	switch {
	case cnt[health.StatusFail] > 0:
		return fmt.Sprintf(T.ReasonHealthFail, cnt[health.StatusFail])
	case cnt[health.StatusWarn] > 0:
		return fmt.Sprintf(T.ReasonHealthWarn, cnt[health.StatusWarn])
	case cnt[health.StatusOK] > 0:
		return T.PanelHealthOK
	}
	return label
}

func healthCountsText(cnt map[health.Status]int) string {
	T := i18n.T
	parts := []string{fmt.Sprintf("%s %d", T.HealthOK, cnt[health.StatusOK])}
	for _, k := range []struct {
		st    health.Status
		label string
	}{{health.StatusWarn, T.HealthWarn}, {health.StatusFail, T.HealthFail}, {health.StatusSkip, T.HealthSkip}} {
		if n := cnt[k.st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", k.label, n))
		}
	}
	return strings.Join(parts, " · ")
}

// splitEvidence splits an evidence line that starts with a label padded
// by spaces ("系统   macbook …") into the label and the value; a line
// without one comes back as the value alone.
func splitEvidence(line string) (label, value string) {
	i := strings.Index(line, "  ")
	if i <= 0 {
		return "", line
	}
	label = line[:i]
	if strings.TrimSpace(label) != label || strings.ContainsAny(label, ":=") || len([]rune(label)) > 8 {
		return "", line
	}
	value = strings.TrimLeft(line[i:], " ")
	if value == "" {
		return "", line
	}
	return label, value
}

// evidenceLines draws evidence as two columns where lines carry a label
// padded with spaces: the label in the UI face, the value in mono.
// Aligning with spaces in a mono face drifts once a label is Chinese
// (review-a 007-7). In a block where one line is padded that way
// ("系统   a → b"), its siblings with a one-word label ("mihomo a → b")
// split at their first space, so the values line up.
func evidenceLines(c *ui.Context, lines []string) {
	t := c.Theme()
	padded := false
	for _, l := range lines {
		if lb, _ := splitEvidence(l); lb != "" {
			padded = true
			break
		}
	}
	for _, l := range lines {
		lb, val := splitEvidence(l)
		if padded && lb == "" {
			lb, val = splitFirstWord(l)
		}
		if !padded || lb == "" {
			ui.Text(c, l).Font("monospace").FontSize(plat.smallSize).Wrap().Selectable()
			continue
		}
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Text(c, lb).FontSize(plat.smallSize).TextColor(t.TextMuted).Width(56).Shrink(0).SingleLine()
			ui.Text(c, val).Font("monospace").FontSize(plat.smallSize).Grow(1).Shrink(1).Wrap().Selectable()
		})
	}
}

// splitFirstWord splits "mihomo a → b" into "mihomo" and "a → b" when the
// first word is short enough to be a label.
func splitFirstWord(line string) (label, value string) {
	i := strings.IndexByte(line, ' ')
	if i <= 0 || len([]rune(line[:i])) > 8 || strings.ContainsAny(line[:i], ":：=") {
		return "", line
	}
	return line[:i], strings.TrimLeft(line[i:], " ")
}

// checkText is one check as plain text, for the clipboard.
func checkText(cr health.CheckReport) string {
	T := i18n.T
	_, _, st := healthStatusLook(&ui.Theme{}, cr.Status)
	var b strings.Builder
	fmt.Fprintf(&b, "%s：%s\n%s\n", st, cr.Title, cr.Summary)
	if len(cr.Evidence) > 0 {
		fmt.Fprintf(&b, "\n%s\n%s\n", T.HealthEvidence, strings.Join(cr.Evidence, "\n"))
	}
	if f := cr.Fix; f != nil {
		fmt.Fprintf(&b, "\n%s\n%s\n", T.HealthFix, f.Text)
		if f.Command != "" {
			fmt.Fprintf(&b, "$ %s\n", f.Command)
		}
	}
	return b.String()
}
