package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 接管 page's views: what exists and what happens to each piece
// (现在 | 接管后), the 订阅原文 the live config was built from, the
// verdict, the 接管 card with its consequence on the button row, and the
// result with 下一步. Data and operations are in takeover.go.

func (a *app) takeoverPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.hd.take
	pageTitle(c, T.PageTakeover)
	if !a.takeoverAdopted() && a.section("plan") {
		a.takeoverIntro(c)
	}
	if !v.fixture && !a.helperGate(c, s) {
		return
	}
	a.opNotes(c, s)
	if v.fixture {
		notice(c, iconWarning, T.TakeoverFixture)
	}
	if a.section("plan") {
		if v.plan == nil {
			a.takeoverEmpty(c, s)
		}
		a.takeoverPlanList(c, s)
	}
	if p := v.plan; p != nil {
		if a.section("plan") {
			a.takeoverEffectsCard(c, p)
			a.takeoverBaseCard(c, p)
		}
		if a.section("verdict") {
			a.takeoverLayersCard(c, p)
			a.takeoverVerdictCard(c, p)
			a.adoptCard(c, s, p)
		}
	}
	if v.adopt != nil && a.section("result") {
		a.adoptResultCard(c, s, v.adopt)
	}
	if a.takeoverAdopted() && a.section("result") {
		a.secureCard(c, s)
	}
}

// takeoverAdopted: duet took over this setup (a plan is adopted).
func (a *app) takeoverAdopted() bool {
	v := &a.hd.take
	if v.adopt != nil && v.adopt.OK {
		return true
	}
	for _, p := range v.plans {
		if p.State == rpc.PlanAdopted {
			return true
		}
	}
	return false
}

// takeoverIntro: what takeover does, in one line and three points, and
// 了解接管, which opens 关于 at 接管是什么. Drawn until duet took over.
func (a *app) takeoverIntro(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	groupBox(c, t).Gap(10).Children(func() {
		ui.Text(c, T.TkIntroLead).FontWeight(plat.headingWeight).Wrap().LineHeight(lineText)
		ui.Column(c).Gap(6).Children(func() {
			for i, p := range []struct {
				icon *ui.SVG
				text string
			}{
				{iconConfig, T.TkIntroManage},
				{iconService, T.TkIntroKeep},
				{iconRollback, T.TkIntroUndo},
			} {
				ui.Row(c.Key(fmt.Sprint("intro", i))).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, p.icon).FontSize(iconInline).TextColor(t.Accent).MarginY(1).Shrink(0)
					ui.Text(c, p.text).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Shrink(1)
				})
			}
		})
		if actionLink(c.Key("learn"), T.TkLearnMore).Clicked() {
			a.openAboutTakeover()
		}
	})
}

// openAboutTakeover opens 关于 scrolled to 接管是什么.
func (a *app) openAboutTakeover() {
	a.about.reveal = aboutTakeoverAnchor
	a.showPage(pageAbout)
}

// tipHead is a card heading with an ⓘ right after the title (infoTip:
// what the card is for), then trailing at the right. For headedCard and
// listCard.
func (a *app) tipHead(c *ui.Context, title, tip string, more func(), trailing ...func()) func() {
	return func() {
		ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine()
		infoTip(c, title, tip, more)
		ui.Spacer(c)
		for _, f := range trailing {
			if f != nil {
				f()
			}
		}
	}
}

// takeoverEmpty: before any plan, what runs now (the running mihomo's
// service, binary and config, detected as a plan would; the helper's own
// settings are duet's defaults until the takeover), then 生成接管计划
// with its note: making a plan changes nothing.
func (a *app) takeoverEmpty(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.take
	headedCard(c, a.tipHead(c, T.TakeoverFound, T.TipFound, nil), func() {
		if v.unsupported {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText)
				ui.Text(c, T.TakeoverTooOld).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Wrap().LineHeight(lineText).Shrink(1)
			})
		}
		running := func(ok bool) string {
			if ok {
				return T.TkFoundLive
			}
			return T.TkFoundNotRunning
		}
		a.liveFileRow(c, "config", T.TakeoverLive, a.liveConfigPath(s))
		if l := a.live; l.Label != "" {
			kvWith(c.Key("service"), T.TkFoundService, func() {
				monoText(c, l.Label).SingleLine().Shrink(1).Selectable().Tooltip(l.Plist)
			})
		}
		if l := a.live; l.Binary != "" {
			a.liveFileRow(c, "binary", T.TkFoundBinary, l.Binary)
		}
		mv := running(s.Mihomo.Reachable)
		if s.Mihomo.Version != "" {
			mv += " · " + s.Mihomo.Version
		}
		kv(c, "mihomo", mv)
		ts := running(s.Tailscale.Reachable && s.Tailscale.State.Running())
		if s.Tailscale.State.Version != "" {
			ts += " · " + s.Tailscale.State.Version
		}
		kv(c, "Tailscale", ts)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			if v.loading {
				ui.Spinner(c)
			}
			small(c, a.planNote()).SingleLine().Shrink(1)
			// Planning is read-only for the system (the helper writes the
			// plan into its own state dir), so it needs no write permission.
			can := s.Helper.Connected() && d.busy == "" && !v.unsupported && !v.fixture
			if a.opButton(c.Key("plan"), iconRefresh, T.TakeoverPlan, true, T.TakeoverPlan).Disabled(!can).Clicked() {
				a.takeoverMakePlan("")
			}
		})
		if v.listErr != "" {
			errorNote(c, T.ErrorPrefix+v.listErr)
		}
	})
}

// liveFileRow is a file of the running setup: its name, the folder after
// it; 未检测到 once detection found nothing, 加载中 before it finished.
func (a *app) liveFileRow(c *ui.Context, key, label, path string) {
	t := c.Theme()
	T := i18n.T
	kvWith(c.Key(key), label, func() {
		switch {
		case path != "":
			monoText(c, filepath.Base(path)).SingleLine().Shrink(0).Selectable().Tooltip(path)
			mono(meta(c, filepath.Dir(path))).FontSize(plat.smallSize).Shrink(1)
		case a.live.loaded || a.shotMode:
			ui.Text(c, T.TkNotDetected).TextColor(t.TextMuted).SingleLine()
		default:
			ui.Text(c, T.Loading).TextColor(t.TextMuted).SingleLine()
		}
	})
}

// takeoverPlanList: earlier plans, newest first. A row is time, base file
// and state; the plan ID and 删除 are in its context menu.
func (a *app) takeoverPlanList(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.hd.take
	if len(v.plans) == 0 {
		return
	}
	trailing := func() {
		if v.plan == nil {
			return
		}
		can := s.Helper.Connected() && a.hd.busy == "" && !v.unsupported && !v.fixture
		if a.opButton(c.Key("replan"), iconRefresh, T.TakeoverPlan, false, T.TakeoverPlan).Disabled(!can).Clicked() {
			a.takeoverMakePlan("")
		}
	}
	listCard(c, a.tipHead(c, T.TakeoverPlans, T.TipPlans, nil, trailing), func() {
		for _, p := range v.plans {
			a.takeoverPlanRow(c, s, p)
		}
	})
}

func (a *app) takeoverPlanRow(c *ui.Context, s status.Snapshot, p rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	sel := v.plan != nil && v.plan.PlanID == p.PlanID
	id := p.PlanID
	row := ui.Row(c.Key(id)).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).PaddingY(listRowPadY).
		Transition(rowMotion).Label(p.CreatedAt.Local().Format("01-02 15:04")).AccessSelected(sel)
	if sel { // the open plan: a tinted row, as a selected list row
		rowFill(c, rowBleed(row), true)
	}
	canDiscard := p.State != rpc.PlanAdopted && !v.fixture && a.helperWrites(s)
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(T.TkCopyPlanID).Chosen() {
			copyText(c, id)
		}
		m.Separator()
		if m.Item(T.TkDiscard).Danger().Disabled(!canDiscard).Chosen() {
			a.takeoverDiscard(id)
		}
	})
	row.Children(func() {
		num(ui.Text(c, p.CreatedAt.Local().Format("01-02 15:04")).SingleLine().Shrink(0))
		monoText(c, filepath.Base(p.Base)).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(p.Base)
		ui.Spacer(c)
		tag(c, T.PlanState(p.State))
		if !sel && pushButton(c.Key("open"), T.TakeoverOpen).Disabled(a.hd.busy != "").Clicked() {
			a.takeoverSelect(id)
		}
	})
}

func (a *app) takeoverDiscard(id string) {
	T := i18n.T
	v := &a.hd.take
	a.confirm(T.TkDiscard, T.TakeoverDiscardBody, T.Remove, func() {
		a.helperOp(T.Remove, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
			return hc.Call(ctx, rpc.MethodTakeoverDiscard, rpc.TakeoverDiscardParams{PlanID: id}, nil)
		}, func(err error) {
			if err == nil && v.plan != nil && v.plan.PlanID == id {
				v.plan, v.adopt = nil, nil
			}
			a.loadTakeover()
		})
	})
}

// --- 现在 | 接管后 ---

// effectRow is one row of the table, worded.
type effectRow struct {
	item, now, nowTip, after, afterTip string
	warn, rewrite                      bool
}

// effectRows words the plan's effects (empty for a helper that sends
// none).
func effectRows(p *rpc.TakeoverPlan) []effectRow {
	T := i18n.T
	var out []effectRow
	for _, e := range p.Effects {
		r := effectRow{warn: e.Warn == rpc.WarnReplaceable}
		switch e.Item {
		case rpc.EffMihomoService:
			r.item, r.now = T.TakeoverMihomoSvc, orDash(e.Subject)
			r.after = T.TkEffKeepService
			if e.Action == rpc.ActNone {
				r.after = T.TkEffNoService
			}
		case rpc.EffMihomoBinary:
			r.item, r.now, r.nowTip = T.TakeoverBinary, filepath.Base(orDash(e.Subject)), e.Subject
			if p.MihomoVersion != "" {
				r.now += " · " + p.MihomoVersion
			}
			r.after = T.TkEffKeep
			if r.warn {
				r.after, r.afterTip = T.TkEffReplaceable, i18n.F(T.TkEffReplaceableTip, e.Subject)
			}
		case rpc.EffConfig:
			r.item, r.now, r.nowTip = T.TakeoverLive, filepath.Base(e.Subject), e.Subject
			r.after, r.rewrite = T.TkEffRewrite, true
		case rpc.EffSubscription:
			r.item = T.TkSubURL
			switch e.Action {
			case rpc.ActStore:
				r.now, r.after = T.TkSubURLFoundNow, T.TkEffStoreSub
				if e.Subject != "" {
					r.now += " · " + e.Subject
				}
			case rpc.ActSkip:
				r.now, r.after = i18n.F(T.TkEffSkipSub, e.Count), T.TkEffNoSub
			default:
				r.now, r.after = T.TkSubURLNone, T.TkEffNoSub
			}
		case rpc.EffSecret:
			r.item, r.now = T.TkSecretItem, T.TakeoverSecretSet
			r.after = T.TkEffKeepSecret
			if e.Action == rpc.ActNone {
				r.now, r.after = T.TkSecretNone, T.TkEffNoSecret // not a risk: a plain chip (H13)
			}
		case rpc.EffTailscale:
			r.item, r.now, r.after = T.TakeoverTSSvc, orDash(e.Subject), T.TkEffUnchanged
		default:
			continue
		}
		out = append(out, r)
	}
	return out
}

// effectChip is the 接管后 cell: a plain chip, or a warning one.
func effectChip(c *ui.Context, text, tip string, warn, rewrite bool) ui.Element {
	t := c.Theme()
	col, border := t.TextMuted, t.Border
	switch {
	case warn:
		col, border = pal(t).warnText, pal(t).warnText.Alpha(0.5)
	case rewrite:
		col, border = t.Accent, t.Accent.Alpha(0.5)
	}
	e := ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(1, 6).Radius(plat.controlRadius-2).Border(1, border).Shrink(1).Children(func() {
		if warn {
			ui.Icon(c, iconWarning).FontSize(plat.smallSize).TextColor(col)
		}
		ui.Text(c, text).FontSize(plat.smallSize).TextColor(col).SingleLine().Shrink(1)
	})
	if tip == "" {
		tip = text // a chip may be cut short
	}
	return e.Tooltip(tip)
}

const effectItemWidth = 116

func (a *app) takeoverEffectsCard(c *ui.Context, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	rows := effectRows(p)
	if len(rows) == 0 { // an older helper: what it found, without the column
		a.takeoverFoundLegacy(c, p)
		return
	}
	listCard(c, a.tipHead(c, T.TakeoverFound, T.TipEffects, nil), func() {
		listRow(c).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "").Width(effectItemWidth).Shrink(0)
				meta(c, T.TkNow).Grow(1).Basis(0)
				meta(c, T.TkAfter).Grow(1).Basis(0)
			})
		})
		for i, r := range rows {
			listRow(c.Key(fmt.Sprint("eff", i))).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight - 8).Children(func() {
					ui.Text(c, r.item).TextColor(t.TextMuted).Width(effectItemWidth).Shrink(0).SingleLine().Tooltip(r.item)
					now := ui.Text(c, r.now).SingleLine().Grow(1).Basis(0).Shrink(1).Selectable()
					if r.nowTip != "" {
						now.Tooltip(r.nowTip)
					}
					ui.Row(c).Grow(1).Basis(0).Shrink(1).Children(func() {
						effectChip(c, r.after, r.afterTip, r.warn, r.rewrite)
					})
				})
			})
		}
		// The one fact the person must act on: edits to the file from now
		// on go into an override.
		listRow(c).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText).Shrink(0)
				small(c, i18n.F(T.TkEditsLost, filepath.Base(p.LiveConfig))).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
				if actionLink(c.Key("new-override"), T.TkNewOverride).Clicked() {
					a.showPage(pageConfig)
					a.newLayer()
				}
			})
		})
	})
}

// takeoverFoundLegacy is what an older helper's plan says (no effects).
func (a *app) takeoverFoundLegacy(c *ui.Context, p *rpc.TakeoverPlan) {
	T := i18n.T
	card(c, T.TakeoverFound, func() {
		kvMono(c, T.TakeoverLive, p.LiveConfig)
		kvMono(c, T.TakeoverMihomoSvc, orDash(p.MihomoService))
		bin := orDash(p.MihomoBinary)
		if p.MihomoVersion != "" {
			bin += " · " + p.MihomoVersion
		}
		kvMono(c, T.TakeoverBinary, bin)
		secret := T.TakeoverSecretSet
		if !p.SecretSet {
			secret = T.TkSecretNone
		}
		kv(c, T.TkSecretItem, secret)
		kvMono(c, T.TakeoverTSSvc, orDash(p.TailscaleSvc))
	})
}

// --- 订阅原文 ---

func (a *app) takeoverBaseCard(c *ui.Context, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	canPlan := !v.fixture && p.State == rpc.PlanReady && a.hd.busy == "" && !v.unsupported
	trailing := func() {
		if pushButton(c.Key("pick-base"), T.TkPickFile).Disabled(!canPlan).Clicked() {
			a.takeoverPickBase(p.LiveConfig)
		}
	}
	var chosen *rpc.TakeoverCandidate
	var others []rpc.TakeoverCandidate
	for i := range p.Candidates {
		cd := p.Candidates[i]
		if cd.Chosen || cd.Path == p.Base {
			chosen = &cd
		} else {
			others = append(others, cd)
		}
	}
	listCard(c, a.tipHead(c, T.TakeoverBase, T.TipBase, nil, trailing), func() {
		// The rule: the result must behave like the running config.
		listRow(c).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.TkTarget).TextColor(t.TextMuted).Width(effectItemWidth).Shrink(0)
				ui.Text(c, i18n.F(T.TkLiveRunning, filepath.Base(p.LiveConfig))).SingleLine().Shrink(1).Tooltip(p.LiveConfig)
			})
		})
		listRow(c).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.TakeoverBase).TextColor(t.TextMuted).Width(effectItemWidth).Shrink(0)
				ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, filepath.Base(orDash(p.Base))).SingleLine().Selectable().Tooltip(p.Base)
					if p.Base != "" {
						meta(c, filepath.Dir(p.Base)).Shrink(1)
					}
				})
				if chosen != nil {
					candVerdict(c, *chosen)
				}
			})
			if chosen != nil && chosen.Error == "" {
				if linkDisclosure(c.Key("cand-detail"), v.candOpen, T.TkCandDetail).Clicked() {
					v.candOpen = !v.candOpen
				}
				if v.candOpen {
					num(small(c, fmt.Sprintf(T.TakeoverCandScore, chosen.ProxiesSame, chosen.ProxiesTotal,
						chosen.GroupsSame, chosen.GroupsTotal, chosen.CommonTail, chosen.Rules, chosen.HeadRules))).Wrap().LineHeight(lineText).PaddingStart(effectItemWidth + 10)
				}
			}
		})
		if len(others) == 0 {
			return
		}
		listRow(c).Children(func() {
			if linkDisclosure(c.Key("others"), v.othersOpen, i18n.F(T.TkOtherCandidates, len(others))).Clicked() {
				v.othersOpen = !v.othersOpen
			}
			if !v.othersOpen {
				return
			}
			for i, cd := range others {
				ui.Row(c.Key(fmt.Sprint("cand", i))).Gap(10).AlignItems(ui.Center).PaddingY(6).Children(func() {
					ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
						ui.Text(c, filepath.Base(cd.Path)).SingleLine().Selectable().Tooltip(cd.Path)
						meta(c, filepath.Dir(cd.Path)).Shrink(1)
					})
					candVerdict(c, cd)
					if cd.Error == "" && pushButton(c.Key("use"), T.TakeoverUseBase).Disabled(!canPlan).Clicked() {
						a.takeoverMakePlan(cd.Path)
					}
				})
			}
		})
	})
}

// candVerdict is a candidate's one chip: it rebuilds the running config,
// or it is incomplete (the reason, if any, in its tooltip).
func candVerdict(c *ui.Context, cd rpc.TakeoverCandidate) {
	T := i18n.T
	if cd.Explains {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Icon(c, iconOK).FontSize(iconInline).TextColor(c.Theme().Success)
			small(c, T.TkExplains).SingleLine()
		})
		return
	}
	e := effectChip(c, T.TkIncomplete, "", true, false)
	if cd.Error != "" {
		e.Tooltip(cd.Error)
	}
}

// takeoverPickBase: 换一个文件…. The helper accepts only a file next to
// or under the live config's folder (takeover.AllowedBase).
func (a *app) takeoverPickBase(live string) {
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      parent,
			Title:       i18n.Cur().TkPickFile,
			DefaultPath: filepath.Dir(live),
			Filters:     []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() { a.takeoverMakePlan(paths[0]) })
	}()
}

// --- 覆写 and the verdict ---

func (a *app) takeoverLayersCard(c *ui.Context, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	headedCard(c, a.tipHead(c, T.Layers, T.TipLayers, nil), func() {
		if len(p.Layers) == 0 {
			small(c, T.TakeoverNoLayers)
		}
		for i, l := range p.Layers {
			ui.Row(c.Key(fmt.Sprint("l", i))).Gap(10).AlignItems(ui.Center).Children(func() {
				name := T.TakeoverLayerName(l.NameCode, l.NameArgs, l.Name)
				ui.Text(c, name).Width(180).Shrink(0).SingleLine().Tooltip(name)
				if stage := T.StageName(l.Stage); stage != name {
					tag(c, stage)
				}
				if l.Kind == "managed" {
					tag(c, T.LayerManaged)
				}
				what := strings.Join(l.Keys, T.ListSep)
				if l.Rules > 0 {
					if what != "" {
						what += " · "
					}
					what += i18n.F(T.TakeoverRules, l.Rules)
				}
				ui.Text(c, what).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(what)
			})
		}
	})
}

func (a *app) takeoverVerdictCard(c *ui.Context, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	headedCard(c, a.tipHead(c, T.TakeoverVerdict, T.TipVerdict, nil), func() {
		verdict, col := T.TakeoverEquivalent, t.Success
		if !p.Equivalent {
			verdict, col = T.TakeoverNotEquivalent, t.Warning
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, col, 10)
			ui.Text(c, verdict).FontSize(plat.headingSize).FontWeight(plat.headingWeight).Wrap().LineHeight(lineText).Shrink(1)
		})
		valid, vcol := T.ValidOK, t.Success
		if !p.Validated {
			valid, vcol = T.ValidFail, t.Danger
		}
		kvWith(c, T.ComposeValid, func() { ui.Text(c, valid).TextColor(vcol) })
		a.takeoverImpactRow(c, p.Impact)
		for _, e := range p.ValidateErr {
			ui.Text(c, e).FontSize(plat.calloutSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Selectable()
		}
		var deliberate, other []rpc.TakeoverChange
		for _, ch := range p.Changes {
			if ch.Deliberate {
				deliberate = append(deliberate, ch)
			} else {
				other = append(other, ch)
			}
		}
		a.changeList(c, "deliberate", T.TakeoverDeliberate, deliberate, t.Accent)
		a.changeList(c, "other", T.TakeoverOtherChanges, other, t.Warning)
		if len(p.Changes) == 0 {
			small(c, T.TakeoverNoChanges)
		}
		for _, w := range planWarnings(p) {
			warningNote(c, w)
		}
		if p.Summary != "" || p.SemDiff != "" {
			label := T.TakeoverShowText
			if v.showText {
				label = T.TakeoverHideText
			}
			if linkDisclosure(c.Key("text"), v.showText, label).Clicked() {
				v.showText = !v.showText
			}
			if v.showText {
				for _, txt := range []string{p.Summary, p.SemDiff} {
					if txt != "" {
						codeBlock(c, txt)
					}
				}
			}
		}
	})
}

// planWarnings are the plan's warnings the page does not already show as
// state (the 接管后 table has the binary, the profiles and the secret;
// the impact row has the reload).
func planWarnings(p *rpc.TakeoverPlan) []string {
	var out []string
	for _, w := range p.Warnings {
		switch i18n.WarningKind(w) {
		case "adopt-disrupts":
			if p.Impact != nil {
				continue
			}
		case "binary-writable", "adopt-profiles":
			if len(p.Effects) > 0 {
				continue
			}
		}
		if len(p.Effects) > 0 && strings.HasPrefix(w, "the controller has no secret") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// impactLines words a reload impact from its fields (not its English
// notes, which go behind 复制详情).
func impactLines(im *rpc.ReloadImpact) []string {
	T := i18n.T
	var out []string
	if im.TUNRebuild {
		out = append(out, T.TkImpactTUN)
	}
	if im.DNSListener {
		out = append(out, T.TkImpactDNS)
	}
	if len(im.Listeners) > 0 {
		out = append(out, i18n.F(T.TkImpactListeners, strings.Join(im.Listeners, T.ListSep)))
	}
	if len(im.ProviderConnsReset) > 0 {
		out = append(out, i18n.F(T.TkImpactProviders, strings.Join(im.ProviderConnsReset, T.ListSep)))
	}
	if im.SelectionReset {
		out = append(out, T.TkImpactSelection)
	}
	if len(im.RestartNeeded) > 0 {
		out = append(out, i18n.F(T.TkImpactRestart, strings.Join(im.RestartNeeded, T.ListSep)))
	}
	return out
}

// takeoverImpactRow: what the adopt's reload does. A bounded blip is a
// warning, not danger (H13); the English notes are behind 复制详情.
func (a *app) takeoverImpactRow(c *ui.Context, im *rpc.ReloadImpact) {
	if im == nil {
		return
	}
	t := c.Theme()
	T := i18n.T
	kvWith(c, T.ImpactLabel, func() {
		ui.Column(c).Gap(4).Grow(1).Shrink(1).Children(func() {
			text, icon, col := T.ImpactGentle, iconOK, t.TextMuted
			if im.Disruptive {
				text, icon, col = T.ImpactDisruptive, iconWarning, pal(t).warnText
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
				ui.Text(c, text).Wrap().LineHeight(lineText).Shrink(1)
				if len(im.Notes) > 0 {
					ui.Spacer(c)
					if actionLink(c.Key("impact-copy"), T.TkCopyDetails).Clicked() {
						copyText(c, strings.Join(im.Notes, "\n"))
					}
				}
			})
			for _, l := range impactLines(im) {
				small(c, l).Wrap().LineHeight(lineText)
			}
		})
	})
}

// changeList is a list of behavior differences. Tailscale rule edits
// show as a list (5, then 另有 N 条); DNS entries show samples of their
// effect and a disclosure with every key.
func (a *app) changeList(c *ui.Context, key, title string, list []rpc.TakeoverChange, col ui.Color) {
	if len(list) == 0 {
		return
	}
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	ui.Column(c.Key(key)).Gap(6).Children(func() {
		ui.Text(c, fmt.Sprintf(T.CountF, title, len(list))).FontWeight(600)
		for i, ch := range list {
			ui.Row(c.Key(fmt.Sprint(i))).Gap(8).AlignItems(ui.Start).Children(func() {
				dot(c, col, 6).Margin(6, 0, 0, 0)
				ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
					if ch.Code == "tailscale-rules" && len(ch.Args) == 2 {
						a.rulesChange(c, ch)
					} else {
						ui.Text(c, ch.Area+" · "+T.TakeoverChange(ch.Code, ch.Args, ch.What)).FontSize(plat.calloutSize).Wrap().LineHeight(lineText).Selectable()
					}
					if ch.Why != "" {
						small(c, T.TakeoverWhy(ch.WhyCode, ch.WhyArgs, ch.Why)).Wrap().LineHeight(lineText)
					}
					for _, s := range changeSamples(ch) {
						ui.Text(c, s).FontSize(plat.smallSize).Font("monospace").TextColor(t.Text).Wrap().LineHeight(lineText).Selectable()
					}
					if len(ch.Keys) > 3 {
						if v.keysOpen == nil {
							v.keysOpen = map[string]bool{}
						}
						open := v.keysOpen[ch.Area]
						label := i18n.F(T.TkShowKeys, len(ch.Keys))
						if open {
							label = T.TkHideKeys
						}
						if linkDisclosure(c.Key("keys"), open, label).Clicked() {
							v.keysOpen[ch.Area] = !open
						}
						if open {
							codeBlock(c, strings.Join(ch.Keys, "\n"))
						}
					}
				})
			})
		}
	})
}

// rulesChange: "N 条 Tailscale 规则改动" with the rules as a list.
func (a *app) rulesChange(c *ui.Context, ch rpc.TakeoverChange) {
	T := i18n.T
	v := &a.hd.take
	rules := strings.Split(ch.Args[1], "; ")
	head := T.TakeoverChange(ch.Code, []string{ch.Args[0], ""}, ch.What)
	head = strings.TrimRight(strings.TrimSpace(head), "：: ")
	ui.Text(c, ch.Area+" · "+head).FontSize(plat.calloutSize).Wrap().LineHeight(lineText)
	n := len(rules)
	if n > 5 && !v.rulesOpen {
		n = 5
	}
	for _, r := range rules[:n] {
		ui.Text(c, r).FontSize(plat.smallSize).Font("monospace").Wrap().LineHeight(lineText).Selectable()
	}
	if len(rules) > 5 {
		label := i18n.F(T.TkMoreN, len(rules)-5)
		if v.rulesOpen {
			label = T.TkHideKeys
		}
		if linkDisclosure(c.Key("more-rules"), v.rulesOpen, label).Clicked() {
			v.rulesOpen = !v.rulesOpen
		}
	}
}

// changeSamples shows what a DNS change does with a few of its keys:
// single-label names and the tailnet suffix to the resolver, which names
// get real addresses, which devices are answered locally.
func changeSamples(ch rpc.TakeoverChange) []string {
	T := i18n.T
	if len(ch.Keys) == 0 {
		return nil
	}
	resolver := "100.100.100.100"
	if len(ch.WhyArgs) > 0 && ch.WhyCode == "coexist-nameserver-policy" {
		resolver = ch.WhyArgs[0]
	}
	pick := func(match func(string) bool, max int) []string {
		var out []string
		for _, k := range ch.Keys {
			if match(k) && len(out) < max {
				out = append(out, k)
			}
		}
		return out
	}
	names := func(l []string) string { return strings.Join(l, T.ListSep) }
	switch ch.Area {
	case "dns.nameserver-policy":
		var out []string
		for _, k := range ch.Keys {
			if k == "*" {
				out = append(out, i18n.F(T.TkSampleSingleLabel, resolver))
			}
		}
		for _, k := range pick(func(k string) bool { return strings.HasSuffix(k, ".ts.net") }, 2) {
			out = append(out, i18n.F(T.TkSampleArrow, k, resolver))
		}
		return out
	case "dns.fake-ip-filter":
		l := pick(func(string) bool { return true }, 3)
		for i, k := range l {
			if k == "*" {
				l[i] = "nas, laptop"
			}
		}
		return []string{i18n.F(T.TkSampleFakeIP, names(l))}
	case "hosts":
		return []string{i18n.F(T.TkSampleHosts, names(pick(func(k string) bool { return !strings.Contains(k, ".") }, 4)))}
	}
	return nil
}

// --- 接管 ---

// rewriteRows are the 接管后 rows that change something (the confirm
// repeats only those).
func rewriteRows(p *rpc.TakeoverPlan) []effectRow {
	var out []effectRow
	for _, r := range effectRows(p) {
		if r.rewrite {
			out = append(out, r)
		}
	}
	return out
}

// adoptCard: the decision and its consequence on one row (H5): how many
// pieces duet rewrites and the reload's impact, then 接管.
func (a *app) adoptCard(c *ui.Context, s status.Snapshot, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	headedCard(c, a.tipHead(c, T.TakeoverAdopt, T.TipAdopt, a.openAboutTakeover), func() {
		switch p.State {
		case rpc.PlanAdopted:
			at := ""
			if !p.AdoptedAt.IsZero() {
				at = p.AdoptedAt.Local().Format("2006-01-02 15:04")
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.Success)
				ui.Text(c, fmt.Sprintf(T.TakeoverAdoptedAt, at)).Grow(1)
				if p.Backup != "" && actionLink(c.Key("backup"), T.ShowInFinder).Clicked() {
					mygo.Shell.ShowItemInFolder(p.Backup)
				}
			})
			return
		case rpc.PlanRolledBack, rpc.PlanFailed, rpc.PlanUndone:
			ui.Text(c, T.PlanState(p.State)+" · "+T.TakeoverReplan).TextColor(pal(t).warnText).Wrap().LineHeight(lineText)
			return
		}
		ok := a.helperWrites(s) && p.Validated && !v.fixture
		rw := rewriteRows(p)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case !p.Validated:
				ui.Text(c, T.TakeoverNotValid).FontSize(plat.calloutSize).TextColor(pal(t).badText).Grow(1).Wrap().LineHeight(lineText)
			case !p.Equivalent:
				ui.Text(c, T.TakeoverCheckChanges).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Grow(1).Wrap().LineHeight(lineText)
			default:
				ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).Wrap().Children(func() {
					if len(rw) > 0 {
						effectChip(c, i18n.F(T.TkRewriteCount, len(rw)), "", false, true)
					}
					if im := p.Impact; im != nil {
						text := T.ImpactGentle
						if im.Disruptive {
							text = T.ImpactDisruptive
						}
						// A one-second blip is information, not a risk (H13).
						effectChip(c, text, strings.Join(impactLines(im), "\n"), false, false)
					}
				})
			}
			if a.opButton(c.Key("adopt"), iconTakeover, T.TakeoverAdopt, true, T.TakeoverAdopt).Disabled(!ok).Clicked() {
				plan := *p
				a.confirm(T.TakeoverAdoptTitle, adoptConfirmBody(&plan), T.TakeoverAdopt, func() {
					a.takeoverAdopt(plan)
				})
			}
		})
	})
}

// adoptConfirmBody lists only what duet rewrites, and the reload.
func adoptConfirmBody(p *rpc.TakeoverPlan) string {
	T := i18n.T
	rw := rewriteRows(p)
	if len(rw) == 0 {
		return fmt.Sprintf(T.TakeoverAdoptBody, p.LiveConfig)
	}
	var lines []string
	for _, r := range rw {
		lines = append(lines, r.item+T.LabelSep+r.nowTip+" · "+r.after)
	}
	if p.Impact != nil && p.Impact.Disruptive {
		lines = append(lines, T.ImpactDisruptive)
	}
	return i18n.F(T.TkAdoptConfirmBody, strings.Join(lines, "\n"))
}

// --- the result ---

// adoptFailing counts the checks still failing after the adopt.
func adoptFailing(r *rpc.AdoptResult) (ids []string) {
	for _, ch := range r.HealthAfter {
		if ch.Status == "fail" {
			ids = append(ids, ch.ID)
		}
	}
	return ids
}

func (a *app) adoptResultCard(c *ui.Context, s status.Snapshot, r *rpc.AdoptResult) {
	t := c.Theme()
	T := i18n.T
	failing := adoptFailing(r)
	card(c, T.TakeoverResult, func() {
		head, icon, col := T.TakeoverDone, iconOK, t.Success
		switch {
		case r.RolledBack:
			head, icon, col = T.TakeoverRolledBack, iconWarning, pal(t).warnText
		case !r.OK:
			head, icon, col = T.TakeoverFailed, iconError, t.Danger
		case len(failing) > 0:
			head, icon, col = i18n.F(T.TkDoneNeedsWork, len(failing)), iconWarning, pal(t).warnText
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(iconInline + 2).TextColor(col)
			ui.Text(c, head).FontSize(plat.headingSize).FontWeight(plat.headingWeight).Grow(1).Wrap().LineHeight(lineText)
			if actionLink(c.Key("copy-result"), T.TkCopyDetails).Clicked() {
				copyText(c, adoptResultText(r))
			}
		})
		ui.Column(c).Gap(3).Children(func() {
			for i, st := range r.Steps {
				ui.Row(c.Key(fmt.Sprint("st", i))).Gap(8).AlignItems(ui.Center).Children(func() {
					icon, icol := stepIcon(t, st.Status)
					ui.Icon(c, icon).FontSize(iconInline).TextColor(icol)
					ui.Text(c, stepName(st)).FontSize(plat.calloutSize).Width(120).Shrink(0)
					small(c, stepPhrase(st)).SingleLine().Shrink(1).Tooltip(st.Detail)
				})
			}
		})
		if len(r.HealthBefore)+len(r.HealthAfter) > 0 {
			ui.Divider(c)
			healthCompare(c, r)
		}
		for _, w := range adoptNotes(r) {
			switch w.kind {
			case noteInfo:
				infoNote(c, w.text)
			default:
				warningNote(c, w.text)
			}
		}
		if r.OK {
			a.adoptSecretRow(c, r)
			a.adoptNext(c, s, r, failing)
		}
	})
}

type noteKind int

const (
	noteWarn noteKind = iota
	noteInfo
)

// adoptNote is one line under the result's health table.
type adoptNote struct {
	kind noteKind
	text string // a helper text for warningNote, or worded (noteInfo, the impact)
}

// adoptNotes are the result's warnings as the page shows them: the reload
// impact worded from its fields (the helper's notes are English), a
// config rebuilt from a newer tailnet as information, and nothing that
// 下一步 already offers (binary-writable → 改用 duet 管理的内核).
func adoptNotes(r *rpc.AdoptResult) []adoptNote {
	T := i18n.T
	var out []adoptNote
	for _, w := range r.Warnings {
		switch i18n.WarningKind(w) {
		case "binary-writable", "new-secret":
			continue // 下一步; the secret row
		case "reload-disrupts":
			text, _ := T.Msg(rpc.Msg{Code: "reload-disrupts"})
			var im *rpc.ReloadImpact
			if r.Apply != nil {
				im = r.Apply.Impact
			}
			if lines := T.ImpactLines(im); len(lines) > 0 {
				text += T.LabelSep + strings.Join(lines, T.ListSep)
			}
			out = append(out, adoptNote{noteInfo, text})
			continue
		case "adopt-differs":
			text, _ := T.Msg(rpc.Msg{Code: "adopt-differs"})
			out = append(out, adoptNote{noteInfo, text})
			continue
		}
		out = append(out, adoptNote{noteWarn, w})
	}
	return out
}

// adoptSecretText says whether the controller has a secret after the
// takeover, and where it came from (walkthrough 2, N19).
func adoptSecretText(r *rpc.AdoptResult, set bool) string {
	T := i18n.T
	for _, w := range r.Warnings {
		if i18n.WarningKind(w) == "new-secret" {
			return T.TkResSecretNew
		}
	}
	if set {
		return T.TkResSecretKept
	}
	return T.TkResSecretNone
}

// adoptSecretRow is the result's 控制接口密钥 line.
func (a *app) adoptSecretRow(c *ui.Context, r *rpc.AdoptResult) {
	t := c.Theme()
	T := i18n.T
	set := a.creds.cur != nil && a.creds.cur.Secret != ""
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		icon, col := iconOK, t.Success
		if !set {
			icon, col = iconSkip, t.TextMuted
		}
		ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
		ui.Text(c, T.TkSecretItem).FontSize(plat.calloutSize).Width(120).Shrink(0)
		small(c, adoptSecretText(r, set)).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
	})
}

// stepName is a step in the UI language (its code; an unknown one as
// the helper named it).
func stepName(st rpc.AdoptStep) string {
	code := st.Code
	if code == "" {
		code = strings.ReplaceAll(st.Name, " ", "-")
	}
	if n, ok := i18n.T.TkStepNames[code]; ok {
		return n
	}
	return st.Name
}

// stepPhrase is one short phrase for a step's outcome; paths, hashes and
// the English detail are in its tooltip and 复制详情.
func stepPhrase(st rpc.AdoptStep) string {
	T := i18n.T
	switch st.Status {
	case "ok":
		code := st.Code
		if code == "" {
			code = strings.ReplaceAll(st.Name, " ", "-")
		}
		if p, ok := T.TkStepOK[code]; ok {
			return p
		}
		return ""
	case "skipped":
		return T.TkStepSkipped
	}
	return T.TkStepFailed
}

// adoptResultText is the result for 复制详情: the helper's own texts.
func adoptResultText(r *rpc.AdoptResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "plan %s ok=%v rolled_back=%v\n", r.PlanID, r.OK, r.RolledBack)
	if r.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", r.Error)
	}
	for _, st := range r.Steps {
		fmt.Fprintf(&b, "%s: %s %s\n", st.Name, st.Status, st.Detail)
	}
	if r.HistoryID != "" {
		fmt.Fprintf(&b, "history: %s\n", r.HistoryID)
	}
	if r.Backup != "" {
		fmt.Fprintf(&b, "backup: %s\n", r.Backup)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	return b.String()
}

func stepIcon(t *ui.Theme, s string) (*ui.SVG, ui.Color) {
	switch s {
	case "ok":
		return iconOK, t.Success
	case "failed":
		return iconError, t.Danger
	case "warn":
		return iconWarning, t.Warning
	default:
		return iconSkip, t.TextMuted
	}
}

// checkWord is a check status in words, with its glyph.
func checkWord(t *ui.Theme, s string) (string, *ui.SVG, ui.Color) {
	T := i18n.T
	switch s {
	case "ok":
		return T.TkCheckOK, iconOK, t.Success
	case "warn":
		return T.TkCheckWarn, iconWarning, t.Warning
	case "fail":
		return T.TkCheckFail, iconError, t.Danger
	}
	return T.TkCheckSkip, iconSkip, t.TextMuted
}

// checkRank orders statuses: higher is worse (skip below warn).
func checkRank(s string) int {
	switch s {
	case "ok":
		return 0
	case "skip", "":
		return 1
	case "warn":
		return 2
	}
	return 3
}

// healthCompare is a table of the checks by title: before, after, after
// rollback; a row that got better shows ↑, worse ↓.
func healthCompare(c *ui.Context, r *rpc.AdoptResult) {
	t := c.Theme()
	T := i18n.T
	index := func(list []rpc.AdoptCheck) map[string]rpc.AdoptCheck {
		m := map[string]rpc.AdoptCheck{}
		for _, ch := range list {
			m[ch.ID] = ch
		}
		return m
	}
	before, after, back := index(r.HealthBefore), index(r.HealthAfter), index(r.HealthAfterRollback)
	var ids []string
	seen := map[string]bool{}
	for _, list := range [][]rpc.AdoptCheck{r.HealthBefore, r.HealthAfter, r.HealthAfterRollback} {
		for _, ch := range list {
			if !seen[ch.ID] {
				seen[ch.ID] = true
				ids = append(ids, ch.ID)
			}
		}
	}
	cols := []string{T.TakeoverHealthBefore, T.TakeoverHealthAfter}
	if len(r.HealthAfterRollback) > 0 {
		cols = append(cols, T.TakeoverHealthRollback)
	}
	const nameW, cellW = 220, 96
	ui.Text(c, T.TakeoverHealth).FontWeight(600)
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "").Width(nameW).Shrink(0)
		for _, h := range cols {
			meta(c, h).Width(cellW)
		}
	})
	cell := func(ch rpc.AdoptCheck, ok bool) {
		if !ok {
			ui.Text(c, "—").FontSize(plat.calloutSize).TextColor(t.TextMuted).Width(cellW).Shrink(0)
			return
		}
		word, icon, col := checkWord(t, ch.Status)
		ui.Row(c).Gap(5).AlignItems(ui.Center).Width(cellW).Shrink(0).Children(func() {
			ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
			ui.Text(c, word).FontSize(plat.calloutSize)
		})
	}
	lang := i18n.Tag()
	for _, id := range ids {
		b, okb := before[id]
		af, oka := after[id]
		better := okb && oka && checkRank(af.Status) < checkRank(b.Status)
		worse := okb && oka && checkRank(af.Status) > checkRank(b.Status)
		row := ui.Row(c.Key("h-" + id)).Gap(10).AlignItems(ui.Center).PaddingY(2)
		if better {
			row.Background(t.Success.Alpha(0.08)).Radius(plat.controlRadius).MarginX(-6).PaddingX(6)
		}
		row.Children(func() {
			name := health.TitleOf(lang, id)
			if name == "" {
				name = id
			}
			ui.Row(c).Gap(4).AlignItems(ui.Center).Width(nameW).Shrink(0).Children(func() {
				ui.Text(c, name).FontSize(plat.calloutSize).SingleLine().Shrink(1).Tooltip(id)
				switch {
				case better:
					ui.Text(c, "↑").FontSize(plat.calloutSize).TextColor(t.Success).Tooltip(T.TkCheckBetter)
				case worse:
					ui.Text(c, "↓").FontSize(plat.calloutSize).TextColor(t.Danger).Tooltip(T.TkCheckWorse)
				}
			})
			cell(b, okb)
			cell(af, oka)
			if len(r.HealthAfterRollback) > 0 {
				rb, ok := back[id]
				cell(rb, ok)
			}
		})
	}
}

// adoptNext is the result's 下一步: the checks still failing, hardening
// the controller, the managed core when the binary is replaceable, and
// 撤销接管.
func (a *app) adoptNext(c *ui.Context, s status.Snapshot, r *rpc.AdoptResult, failing []string) {
	T := i18n.T
	ui.Divider(c)
	ui.Text(c, T.TkNext).FontWeight(600)
	row := func(key, label string, do func()) {
		ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight - 8).Children(func() {
			ui.Icon(c, iconCollapsed).FontSize(iconInline).TextColor(c.Theme().TextMuted)
			if actionLink(c.Key("go"), label).Clicked() {
				do()
			}
		})
	}
	if len(failing) > 0 {
		row("checks", i18n.F(T.TkNextChecks, len(failing)), func() { a.showPage(pageHealth) })
	}
	if a.creds.cur == nil || a.creds.cur.Secret == "" {
		row("secure", T.TkNextSecure, func() {
			a.confirm(i18n.F(T.AskF, T.SecureTitle), T.SecureBody, T.SecureRun, a.controllerSecure)
		})
	}
	for _, w := range r.Warnings {
		if i18n.WarningKind(w) == "binary-writable" {
			row("kernel", T.TkNextKernel, func() { a.showPage(pageKernel) })
			break
		}
	}
	row("undo", T.TkNextUndo, func() { a.showPage(pageLeave) })
}

// secureCard: 加固控制接口 as a state row (当前密钥：已设 / 未设) and its
// button; shown once duet took over.
func (a *app) secureCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	headedCard(c, a.tipHead(c, T.SecureTitle, T.TipSecure, nil), func() {
		set := a.creds.cur != nil && a.creds.cur.Secret != ""
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.TkSecretState).TextColor(t.TextMuted).Width(labelWidth).Shrink(0)
			if set {
				ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.Success)
				ui.Text(c, T.TkSecretSetWeak)
			} else {
				ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText)
				ui.Text(c, T.TkSecretNone)
			}
			ui.Spacer(c)
			can := a.helperWrites(s) && !v.unsupported && !v.fixture
			if a.opButton(c.Key("secure"), iconSecure, T.SecureRun, !set, T.SecureRun).Disabled(!can).Clicked() {
				a.confirm(i18n.F(T.AskF, T.SecureTitle), T.SecureBody, T.SecureRun, a.controllerSecure)
			}
		})
		if r := v.secure; r != nil {
			msg := T.SecureDone
			if r.Replaced {
				msg = T.SecureReplaced
			}
			ui.Text(c, msg).FontSize(plat.calloutSize).TextColor(pal(t).okText).Wrap().LineHeight(lineText)
			if r.Apply != nil && r.Apply.Error != "" {
				errorNote(c, T.ErrorPrefix+r.Apply.Error)
			}
			for _, w := range r.Warnings {
				warningNote(c, w)
			}
		}
	})
}

// actionLink is an in-app action that reads as text (accent, no
// underline): a button with the arrow cursor, not ui.Link, which is for
// real links.
func actionLink(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	b := ui.Row(c).AlignItems(ui.Center).Focusable().Role(ui.RoleButton).Label(label).Shrink(0)
	b.Children(func() {
		ui.Text(c, label).FontSize(plat.smallSize).TextColor(t.Accent).SingleLine()
	})
	return b
}

// planNote is the line next to 生成接管计划: what it does, or while the
// helper makes the plan, what it is doing and (after planElapsedAfter) for
// how long, ticking once a second.
func (a *app) planNote() string {
	T := i18n.T
	d := &a.hd
	if d.busy != T.TakeoverPlan || d.spanOp != T.TakeoverPlan || d.span.start.IsZero() {
		return "" // what planning does is in the ⓘ (copy rule: no reassurance line)
	}
	if !d.planTick {
		d.planTick = true
		time.AfterFunc(time.Second, func() {
			mygo.RunOnMain(func() {
				d.planTick = false
				a.invalidate()
			})
		})
	}
	el := time.Since(d.span.start)
	if el < planElapsedAfter {
		return T.TkPlanRunning
	}
	return T.TkPlanRunning + " " + fmt.Sprintf(T.TkPlanElapsed, int(el/time.Second))
}

// planElapsedAfter: a plan usually takes about a second; show the time
// only when it runs longer.
const planElapsedAfter = 2 * time.Second
