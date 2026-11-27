package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
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
	case health.StatusAccepted:
		return iconSkip, t.TextMuted, T.HealthAccepted
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
		c.Toast(i18n.T.ErrorText(err.Error()))
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
			b := pushButton(c.Key("rerun"), "").Label(label).Disabled(h.Running)
			spin := showBusy(c, b, h.Running)
			b.Children(func() {
				turningIcon(c, iconRefresh, spin) // the refresh icon turns while the checks run
				ui.Text(c, label).SingleLine()
			})
			if b.Clicked() {
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
				emptyState(c, iconError, T.HealthFailedTitle, "")
				ui.Text(c, h.Err).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).TextAlign(ui.Center).
					Wrap().LineHeight(lineText).MaxWidth(520).Selectable()
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
	demote := healthDemoted(s)
	groupBox(c, t).Children(func() {
		icon, col, label := healthStatusLook(t, rep.Overall)
		head := healthHeadline(label, cnt)
		// The headline is the one number the banner and the sidebar show,
		// at their level (Snapshot.HealthBadge): a finding they call a
		// warning is a warning here too, not 1 项失败 in red (R-kubai-6).
		if n, l := s.HealthBadge(); n > 0 && l != status.LevelOK {
			icon, col = levelIcon(l), levelColor(t, l)
			for _, r := range s.Reasons {
				if r.Code == status.ReasonHealth {
					head = healthReasonShort(r)
				}
			}
		}
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(iconLarge).TextColor(col).Shrink(0)
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, head).FontSize(plat.leadSize).FontWeight(600)
				num(small(c, healthCountsText(cnt)))
				last := fmt.Sprintf(T.HealthLastRunAt, rep.Started.Format("15:04:05"))
				if rep.Duration >= 100*time.Millisecond { // "用时 0.0 秒" says nothing
					last = fmt.Sprintf(T.HealthLastRun, rep.Started.Format("15:04:05"), fmt.Sprintf(T.Seconds, rep.Duration.Seconds()))
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					num(small(c, last))
					if h.Running {
						ui.Spinner(c).Size(12, 12) // a new report is on its way
					}
				})
			})
		})
		if h.Err != "" {
			errorNote(c, fmt.Sprintf(T.HealthError, h.Err))
		}
	})
	var problems, accepted, caused, rest []health.CheckReport
	for _, cr := range rep.Checks {
		if egressOff(cr) && a.egress.result != nil {
			cr = *a.egress.result // the in-process test stands in for 未开启 (no helper)
		}
		switch {
		case cr.Status == health.StatusWarn || cr.Status == health.StatusFail:
			problems = append(problems, cr)
		case cr.Status == health.StatusAccepted && cr.CausedBy != "":
			caused = append(caused, cr)
		case cr.Status == health.StatusAccepted:
			accepted = append(accepted, cr)
		default:
			rest = append(rest, cr)
		}
	}
	list := func(title, tip string, crs []health.CheckReport, extra func()) {
		if len(crs) == 0 {
			return
		}
		// The same heading as every other group: lined up with the
		// labels in its box (groupPadX).
		listCard(c, listHeadTip(c, nil, title, tip, func() {
			num(small(c, fmt.Sprint(len(crs))))
		}), func() {
			for _, cr := range crs {
				a.healthCheckRow(c.Key("check:"+cr.ID), cr, s, demote)
			}
			if extra != nil {
				extra()
			}
		})
	}
	list(T.HealthProblems, "", problems, nil)
	// An accepted exit node: its check, and the checks it breaks grouped
	// under it, folded; none of them counts.
	list(T.HealthAccepted, T.TipHealthAccepted, accepted, func() {
		if len(caused) == 0 {
			return
		}
		open := a.healthOpen["caused-by"]
		ui.Column(c.Key("caused")).PaddingY(listRowPadY).Gap(4).Clip().Transition(expandFor(open)).Children(func() {
			l := linkDisclosure(c.Key("caused-head"), open, fmt.Sprintf(T.CountF, T.CausedByExitNode, len(caused)))
			if l.Clicked() {
				a.healthOpen["caused-by"] = !open
			}
			if open {
				ui.Column(c).MarginStart(26).Children(func() {
					for _, cr := range caused {
						a.healthCheckRow(c.Key("caused:"+cr.ID), cr, s, demote)
					}
				})
			}
		})
	})
	list(T.HealthPassed, "", rest, nil)
}

// healthDemoted: the banner rates the checks' failures as warnings (a
// finding before takeover), so their rows wear the warning's glyph and
// color too: one problem, one severity everywhere (R-kubai-6).
func healthDemoted(s status.Snapshot) bool {
	_, l := s.HealthBadge()
	if l != status.LevelWarn || s.Health.Report == nil {
		return false
	}
	return s.Health.Report.Counts()[health.StatusFail] > 0
}

func (a *app) healthCheckRow(c *ui.Context, cr health.CheckReport, s status.Snapshot, demote bool) {
	t := c.Theme()
	T := i18n.T
	p := pal(t)
	open, set := a.healthOpen[cr.ID]
	if !set { // problems start expanded
		open = cr.Status == health.StatusWarn || cr.Status == health.StatusFail
	}
	// Every row with something more to say opens: its sentence stays
	// one line in the list, whole in the tooltip and in the opened row,
	// and the chevron is always there (kubai R-20). A skipped or accepted
	// row with nothing else has no chevron.
	egress := cr.ID == (health.LeakEgress{}).ID()
	off := egressOff(cr)
	// The egress row's own fix (检测出口) is on the row, not in the fold.
	fix := cr.Fix != nil && !(egress && cr.Fix.AutoFix == health.FixEgressProbe)
	expandable := len(cr.Evidence) > 0 || fix ||
		cr.Summary != "" && cr.Status != health.StatusSkip && cr.Status != health.StatusAccepted ||
		egress && !off && cr.Summary != ""
	look := cr.Status
	if demote && look == health.StatusFail {
		look = health.StatusWarn
	}
	icon, col, _ := healthStatusLook(t, look)
	_, _, stateText := healthStatusLook(t, cr.Status)
	if off {
		// Not tested yet: a setting, not a finding. The power glyph,
		// not skip's ⊖, which reads as something left unresolved.
		icon, stateText = iconPower, T.EgressUntested
	}
	ui.Column(c).PaddingY(listRowPadY).Gap(8).Clip().Transition(expandFor(open)).Children(func() {
		head := ui.Row(c.Key("head")).Gap(10).AlignItems(ui.Center).MinHeight(20).Label(stateText + T.LabelSep + cr.Title)
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
			case egress:
				infoTip(c, cr.Title, T.TipEgress, nil)
				// What the state means, in a few words: 未测过, or when
				// it was tested and what it found.
				line := egressLine(cr, s)
				ui.Text(c, line).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(line)
			case cr.Status == health.StatusSkip:
				// Skipped: the short reason as a chip; the error, if
				// any, is in the evidence.
				tag(c, cr.Summary).Shrink(1)
			case cr.Status == health.StatusAccepted:
				tag(c, T.HealthAccepted)
			case !open:
				// Cut to one line; the whole sentence on hover, as
				// macOS shows a truncated cell's text.
				ui.Text(c, cr.Summary).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(cr.Summary)
			}
			ui.Spacer(c)
			if egress {
				// The row carries its controls (U31): 检测出口 and the
				// 定期检测 switch, its ⓘ saying what the test sends.
				a.egressControls(c, cr, s)
			} else if cr.Status == health.StatusSkip && !open {
				if act := a.fixAction(cr, s); act != nil {
					busy := egress && a.egress.running || a.hd.busy == act.label
					if busyButton(c.Key("fix-inline"), act.label, false, busy, act.off).Clicked() {
						act.run()
					}
				}
			}
			if expandable {
				disclosure(c, open).Shrink(0)
			}
		})
		if expandable {
			head.Focusable().Role(ui.RoleDisclosure).Expanded(open)
			if head.Clicked() {
				a.healthOpen[cr.ID] = !open
			}
		}
		if egress && a.egress.err != "" {
			// A failed run is said, whether or not the row is open.
			ui.Column(c).MarginStart(26).Children(func() {
				failedRowWhy(c.Key("egress-failed"), T.EgressFailed, a.egress.why, a.egress.err, a.testEgress, a.egress.running)
			})
		}
		if !open || !expandable {
			return
		}
		ui.Column(c).Gap(10).MarginStart(26).Children(func() {
			if cr.Status != health.StatusSkip || egress {
				ui.Text(c, cr.Summary).Wrap().LineHeight(lineText).Selectable()
			}
			if egress && cr.Egress != nil {
				egressIPs(c, cr.Egress)
			}
			if len(cr.Evidence) > 0 {
				// Repeated lines fold into one with a count; at most
				// evidenceShown lines until 显示全部.
				lines := health.DedupeEvidence(cr.Evidence)
				all := a.evidenceAll[cr.ID]
				ui.Column(c).Gap(6).Children(func() {
					small(c, T.HealthEvidence)
					ui.Column(c).Gap(3).Padding(10, 12).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Children(func() {
						shown := lines
						if !all && len(shown) > evidenceShown {
							shown = shown[:evidenceShown]
						}
						evidenceLines(c, evidenceTexts(shown))
					})
					if len(lines) > evidenceShown {
						if linkDisclosure(c.Key("ev-all"), all, i18n.F(T.EvidenceAll, len(lines))).Clicked() {
							if a.evidenceAll == nil {
								a.evidenceAll = map[string]bool{}
							}
							a.evidenceAll[cr.ID] = !all
						}
					}
				})
			}
			if f := cr.Fix; f != nil && fix {
				ui.Column(c).Gap(6).Children(func() {
					small(c, T.HealthFix)
					ui.Text(c, f.Text).Wrap().LineHeight(lineText).Selectable()
					if act := a.fixAction(cr, s); act != nil {
						ui.Row(c).Children(func() {
							busy := egress && a.egress.running || a.hd.busy == act.label
							if busyButton(c.Key("fix"), act.label, false, busy, act.off).Clicked() {
								act.run()
							}
						})
					}
					if f.Command != "" {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "$ "+strings.ReplaceAll(f.Command, "\n", "\n$ ")).Font("monospace").FontSize(plat.calloutSize).
								Padding(8, 10).Radius(plat.innerRadius).Background(p.code).Border(1, p.groupLine).Grow(1).Wrap().LineHeight(lineText).Selectable()
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

// evidenceShown is how many evidence lines a check shows until 显示全部.
const evidenceShown = 3

// evidenceTexts are deduplicated lines as text, a repeated one with its
// count (×12).
func evidenceTexts(lines []health.EvidenceLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
		if l.Count > 1 {
			out[i] += " " + fmt.Sprintf(i18n.T.EvidenceTimes, l.Count)
		}
	}
	return out
}

// healthStaleAfter is when a report's age shows: twice the background
// interval (status.Options.HealthInterval, 10 minutes).
const healthStaleAfter = 20 * time.Minute

// healthAge is the report's age while it is stale ("25 分钟前更新"), else
// "": a number that is not current says so.
func healthAge(s status.Snapshot) string {
	h := s.Health
	if !h.Stale(s.Time, healthStaleAfter) {
		return ""
	}
	return fmt.Sprintf(i18n.T.HealthAge, status.Ago(s.Time, h.Report.Started))
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
		return i18n.F(T.HealthFailN, cnt[health.StatusFail])
	case cnt[health.StatusWarn] > 0:
		return i18n.F(T.HealthWarnN, cnt[health.StatusWarn])
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
			ui.Text(c, l).Font("monospace").FontSize(plat.smallSize).Wrap().LineHeight(lineText).Selectable()
			continue
		}
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Text(c, lb).FontSize(plat.smallSize).TextColor(t.TextMuted).Width(56).Shrink(0).SingleLine()
			ui.Text(c, val).Font("monospace").FontSize(plat.smallSize).Grow(1).Shrink(1).Wrap().LineHeight(lineText).Selectable()
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
	fmt.Fprintf(&b, "%s%s%s\n%s\n", st, T.LabelSep, cr.Title, cr.Summary)
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

// egressOff: the egress check as a run reports it while it is off (no
// on-demand result to show): skipped, the test offered.
func egressOff(cr health.CheckReport) bool {
	return cr.ID == (health.LeakEgress{}).ID() && cr.Status == health.StatusSkip && cr.At.IsZero() &&
		cr.Fix != nil && cr.Fix.AutoFix == health.FixEgressProbe
}

// egressLine is the egress row's state in a few words: 未测过, or when it
// was tested and what it found (21:30 检测 · …). A periodic run's result
// carries the report's time.
func egressLine(cr health.CheckReport, s status.Snapshot) string {
	T := i18n.T
	if egressOff(cr) {
		return T.EgressUntested
	}
	at := cr.At
	if at.IsZero() && s.Health.Report != nil {
		at = s.Health.Report.Started
	}
	if at.IsZero() {
		return cr.Summary
	}
	return fmt.Sprintf(T.EgressTestedAt, eventTime(s.Time, at)) + " · " + cr.Summary
}

// egressIPs are the addresses the egress test saw, unmasked (the
// owner's own machine; the copied report keeps them masked): through
// mihomo, with the node, and directly.
func egressIPs(c *ui.Context, ips *health.EgressIPs) {
	T := i18n.T
	ui.Column(c.Key("egress-ips")).Gap(4).Children(func() {
		if ips.Proxied.IsValid() {
			kvWith(c, T.OvEgressVia, func() {
				monoText(c, ips.Proxied.String()).SingleLine().Shrink(0).Selectable()
				if ips.Node != "" {
					small(c, ips.Node).SingleLine().TruncateMiddle().Shrink(1).MinWidth(0)
				}
			})
		}
		if ips.Direct.IsValid() {
			kvWith(c, T.OvEgressDirect, func() {
				monoText(c, ips.Direct.String()).SingleLine().Shrink(0).Selectable()
			})
		}
	})
}

// egressControls are the 出口 IP 实测 row's trailing controls: run the
// test now (检测出口, 重新检测 once there is a result) with its progress
// on the button, then the 定期检测 switch (the helper's setting; turning
// it on tests at once). The ⓘ after the title says what the test sends.
func (a *app) egressControls(c *ui.Context, cr health.CheckReport, s status.Snapshot) {
	T := i18n.T
	ui.Row(c).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		if a.canTestEgress(s) {
			label := T.FixTestEgress
			if !egressOff(cr) {
				label = T.EgressRetest
			}
			if busyButton(c.Key("egress-test"), label, false, a.egress.running, a.egress.running).Clicked() {
				a.testEgress()
			}
		}
		set := a.hd.settings
		if !s.Helper.Connected() || set == nil {
			return // the switch is the helper's setting
		}
		small(c, T.EgressPeriodic).SingleLine().Shrink(0)
		on := set.HealthEgress
		sw := ui.Switch(c.Key("egress-periodic"), &on).Label(T.EgressPeriodic).Disabled(!a.helperOpsEnabled(s) || a.hd.busy != "")
		if sw.Changed() {
			set.HealthEgress = on // shown at once; the helper's answer follows
			a.setEgressPeriodic(on)
		}
	})
}
