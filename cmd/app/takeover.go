package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 接管 page: take over the existing mihomo setup. The helper inspects
// it read-only and writes a plan (takeover.plan); the page shows the plan
// (base subscription and candidates, layers, deliberate changes, the
// semantic-diff verdict); 接管 runs takeover.adopt with the plan's confirm
// hash (backup → import → apply → health before/after → automatic
// rollback when worse). 加固控制接口 runs controller.secure.

type takeoverView struct {
	plans   []rpc.TakeoverPlan // takeover.plans, newest first
	loaded  bool
	loading bool
	// unsupported: the helper has no takeover.* yet.
	unsupported bool
	listErr     string

	plan     *rpc.TakeoverPlan // the selected plan, full (summary + semdiff)
	showText bool              // summary and semdiff texts expanded

	adopt  *rpc.AdoptResult
	secure *rpc.ControllerSecureResult

	// fixture: the plan and result come from DUET_TAKEOVER_FIXTURE (a
	// recorded plan.json / adopt.json, for screenshots); nothing is sent.
	fixture bool
}

// isTooOld reports whether err means the helper lacks the method.
func isTooOld(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && (re.Code == rpc.CodeMethodNotFound || re.Code == rpc.CodeNotImplemented)
}

// loadTakeover lists the plans. Main thread.
func (a *app) loadTakeover() {
	v := &a.hd.take
	if v.fixture || !a.hd.connected || v.loading {
		return
	}
	v.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var plans []rpc.TakeoverPlan
		err := hc.Call(ctx, rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{}, &plans)
		mygo.RunOnMain(func() {
			v.loading, v.loaded = false, true
			v.unsupported = isTooOld(err)
			v.listErr = ""
			if err != nil && !v.unsupported {
				v.listErr = helperError(err)
			}
			v.plans = plans
			// Keep the selection fresh (its state changes after adopt).
			if v.plan != nil {
				for _, p := range plans {
					if p.PlanID == v.plan.PlanID {
						v.plan.State, v.plan.AdoptedAt, v.plan.Backup = p.State, p.AdoptedAt, p.Backup
					}
				}
			}
			a.invalidate()
		})
	}()
}

// loadTakeoverFixture fills the page from dir/plan.json (and
// dir/adopt.json when present). Development aid.
func (a *app) loadTakeoverFixture(dir string) error {
	v := &a.hd.take
	var p rpc.TakeoverPlan
	b, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return fmt.Errorf("plan.json: %w", err)
	}
	v.fixture, v.loaded, v.unsupported, v.listErr = true, true, false, ""
	v.plans = []rpc.TakeoverPlan{p}
	v.plan = &p
	if b, err := os.ReadFile(filepath.Join(dir, "adopt.json")); err == nil {
		var r rpc.AdoptResult
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("adopt.json: %w", err)
		}
		v.adopt = &r
	}
	return nil
}

func (a *app) takeoverPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.hd.take
	pageTitle(c, T.PageTakeover)
	if !v.fixture && !a.helperGate(c, s) {
		return
	}
	a.opNotes(c, s)
	if v.fixture {
		notice(c, iconWarning, T.TakeoverFixture)
	}
	if a.section("plan") {
		a.takeoverIntro(c, s)
	}
	if v.plan != nil {
		a.takeoverPlanCards(c, s, v.plan)
	}
	if v.adopt != nil && a.section("result") {
		a.adoptResultCard(c, v.adopt)
	}
	if a.section("result") {
		a.secureCard(c, s)
	}
}

func (a *app) takeoverIntro(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.take
	iconCard(c, iconTakeover, T.TakeoverTitle, func() {
		ui.Text(c, T.TakeoverIntro).FontSize(13).Wrap()
		if v.unsupported {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWarning).FontSize(14).TextColor(pal(t).warnText)
				ui.Text(c, T.TakeoverTooOld).FontSize(12).TextColor(pal(t).warnText).Wrap().Shrink(1)
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			// Planning is read-only for the system (the helper writes the
			// plan into its own state dir), so it needs no write permission.
			can := (s.Helper.Connected() && d.busy == "" && !v.unsupported) && !v.fixture
			if a.opButton(c.Key("plan"), iconRefresh, T.TakeoverPlan, v.plan == nil, T.TakeoverPlan).Disabled(!can).Clicked() {
				a.takeoverMakePlan("")
			}
			if v.loading {
				ui.Spinner(c)
			}
		})
		if v.listErr != "" {
			ui.Text(c, T.ErrorPrefix+v.listErr).FontSize(12).TextColor(pal(t).badText).Wrap()
		}
		if len(v.plans) == 0 && v.loaded && !v.unsupported {
			hintText(c, T.TakeoverNoPlans)
		}
	})
	if len(v.plans) > 0 {
		listCard(c, listHead(c, nil, T.TakeoverPlans, nil), func() {
			for _, p := range v.plans {
				a.takeoverPlanRow(c, s, p)
			}
		})
	}
}

func (a *app) takeoverPlanRow(c *ui.Context, s status.Snapshot, p rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	sel := v.plan != nil && v.plan.PlanID == p.PlanID
	row := ui.Row(c.Key(p.PlanID)).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).PaddingY(8).
		Transition(rowMotion).Label(p.PlanID).AccessSelected(sel)
	if sel { // the open plan: a tinted row, as a selected list row
		row.Background(pal(t).accentSoft).Radius(plat.controlRadius).MarginX(-8).PaddingX(8)
	}
	row.Children(func() {
		num(ui.Text(c, p.CreatedAt.Local().Format("01-02 15:04")).SingleLine().Shrink(0))
		tag(c, T.PlanState(p.State))
		ui.Text(c, filepath.Base(p.Base)).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(p.Base)
		ui.Spacer(c)
		ui.Text(c, p.PlanID).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(p.PlanID)
		if !sel && ui.Button(c.Key("open"), T.TakeoverOpen).Disabled(a.hd.busy != "").Clicked() {
			a.takeoverSelect(p.PlanID)
		}
		if p.State != rpc.PlanAdopted && !v.fixture && ui.Button(c.Key("discard"), T.Remove).Disabled(!a.helperWrites(s)).Clicked() {
			id := p.PlanID
			a.confirm(fmt.Sprintf(T.TakeoverDiscardTitle, id), T.TakeoverDiscardBody, T.Remove, func() {
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
	})
}

// takeoverMakePlan asks the helper for a new plan; base picks one of the
// candidates it found (empty: its best).
func (a *app) takeoverMakePlan(base string) {
	T := i18n.T
	v := &a.hd.take
	var p rpc.TakeoverPlan
	a.helperOp(T.TakeoverPlan, 3*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverPlan, rpc.TakeoverPlanParams{Base: base}, &p)
	}, func(err error) {
		if isTooOld(err) {
			v.unsupported = true
		}
		if err == nil {
			v.plan, v.adopt, v.showText = &p, nil, false
		}
		a.loadTakeover()
	})
}

// takeoverSelect loads one plan in full (with its summary and semdiff).
func (a *app) takeoverSelect(id string) {
	T := i18n.T
	v := &a.hd.take
	var ps []rpc.TakeoverPlan
	a.helperOp(T.TakeoverOpen, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{PlanID: id}, &ps)
	}, func(err error) {
		if err == nil && len(ps) == 1 {
			v.plan, v.adopt, v.showText = &ps[0], nil, false
		}
	})
}

func (a *app) takeoverPlanCards(c *ui.Context, s status.Snapshot, p *rpc.TakeoverPlan) {
	if a.section("plan") {
		a.takeoverFoundCards(c, p)
	}
	if a.section("verdict") {
		a.takeoverVerdictCards(c, s, p)
	}
}

func (a *app) takeoverFoundCards(c *ui.Context, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	card(c, T.TakeoverFound, func() {
		kv(c, T.TakeoverLive, p.LiveConfig)
		kv(c, "SHA-256", shortDigest(p.LiveSHA256))
		kv(c, T.TakeoverMihomoSvc, orDash(p.MihomoService))
		bin := orDash(p.MihomoBinary)
		if p.MihomoVersion != "" {
			bin += " · " + p.MihomoVersion
		}
		kv(c, T.TakeoverBinary, bin)
		kvWith(c, T.TakeoverController, func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, orDash(p.Controller)).FontSize(13)
				if p.SecretSet {
					tag(c, T.TakeoverSecretSet)
				} else {
					ui.Text(c, T.TakeoverNoSecret).FontSize(12).TextColor(pal(t).warnText)
				}
			})
		})
		kv(c, T.TakeoverTSSvc, orDash(p.TailscaleSvc))
		sub := T.TakeoverSubNoURL
		if p.SubscriptionURL {
			sub = T.TakeoverSubURL
		}
		kv(c, T.Subscription, sub)
	})

	listCard(c, listHead(c, nil, T.TakeoverBase, nil), func() {
		chosenListed := false
		for _, cd := range p.Candidates {
			chosenListed = chosenListed || cd.Chosen || cd.Path == p.Base
		}
		if !chosenListed || len(p.Candidates) == 0 {
			listRow(c).Children(func() {
				kv(c, T.TakeoverBaseChosen, orDash(p.Base))
				if len(p.Candidates) == 0 {
					hintText(c, T.TakeoverNoCandidates)
				}
			})
		}
		for i, cd := range p.Candidates {
			ui.Row(c.Key(fmt.Sprint("cand", i))).Gap(10).AlignItems(ui.Center).PaddingY(10).Children(func() {
				col := t.TextMuted.Alpha(0.6)
				if cd.Explains {
					col = t.Success
				}
				dot(c, col, 8)
				ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, filepath.Base(cd.Path)).SingleLine().Selectable()
					ui.Text(c, filepath.Dir(cd.Path)).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Selectable()
					detail := cd.Error
					if detail == "" {
						detail = fmt.Sprintf(T.TakeoverCandScore, cd.ProxiesSame, cd.ProxiesTotal,
							cd.GroupsSame, cd.GroupsTotal, cd.CommonTail, cd.Rules, cd.HeadRules)
					}
					col := t.TextMuted
					if cd.Error != "" {
						col = pal(t).badText
					}
					num(ui.Text(c, detail).FontSize(plat.smallSize).TextColor(col).Wrap())
				})
				if cd.Chosen || cd.Path == p.Base {
					tag(c, T.TakeoverBaseChosen)
				} else if cd.Error == "" && !v.fixture && p.State == rpc.PlanReady {
					if ui.Button(c.Key("use"), T.TakeoverUseBase).Disabled(a.hd.busy != "").Clicked() {
						a.takeoverMakePlan(cd.Path)
					}
				}
			})
		}
	})
}

func (a *app) takeoverVerdictCards(c *ui.Context, s status.Snapshot, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	card(c, T.Layers, func() {
		if len(p.Layers) == 0 {
			ui.Text(c, T.TakeoverNoLayers).FontSize(12).TextColor(t.TextMuted)
		}
		for i, l := range p.Layers {
			ui.Row(c.Key(fmt.Sprint("l", i))).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, l.Name).FontSize(13).Width(180).Shrink(0).SingleLine().Tooltip(l.Name)
				tag(c, T.StageName(l.Stage))
				ui.Text(c, l.Kind).FontSize(11).TextColor(t.TextMuted)
				what := strings.Join(l.Keys, ", ")
				if l.Rules > 0 {
					if what != "" {
						what += " · "
					}
					what += fmt.Sprintf(T.TakeoverRules, l.Rules)
				}
				ui.Text(c, what).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).Tooltip(what)
			})
		}
	})

	card(c, T.TakeoverVerdict, func() {
		verdict, col := T.TakeoverEquivalent, t.Success
		if !p.Equivalent {
			verdict, col = T.TakeoverNotEquivalent, t.Warning
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, col, 10)
			ui.Text(c, verdict).FontSize(plat.headingSize).FontWeight(plat.headingWeight).Wrap().Shrink(1)
		})
		valid, vcol := T.ValidOK, t.Success
		if !p.Validated {
			valid, vcol = T.ValidFail, t.Danger
		}
		kvWith(c, T.ComposeValid, func() { ui.Text(c, valid).TextColor(vcol) })
		impactRow(c, p.Impact)
		for _, e := range p.ValidateErr {
			ui.Text(c, e).FontSize(12).TextColor(pal(t).badText).Wrap().Selectable()
		}
		var deliberate, other []rpc.TakeoverChange
		for _, ch := range p.Changes {
			if ch.Deliberate {
				deliberate = append(deliberate, ch)
			} else {
				other = append(other, ch)
			}
		}
		changeList(c, "deliberate", T.TakeoverDeliberate, deliberate, t.Accent)
		changeList(c, "other", T.TakeoverOtherChanges, other, t.Warning)
		if len(p.Changes) == 0 {
			ui.Text(c, T.TakeoverNoChanges).FontSize(12).TextColor(t.TextMuted)
		}
		for _, w := range p.Warnings {
			if p.Impact != nil && i18n.WarningKind(w) == "adopt-disrupts" {
				continue // the impact row says it
			}
			warningNote(c, w)
		}
		if p.Summary != "" || p.SemDiff != "" {
			chev, label := iconCollapsed, T.TakeoverShowText
			if v.showText {
				chev, label = iconExpanded, T.TakeoverHideText
			}
			ui.Row(c).Children(func() {
				if iconButton(c.Key("text"), chev, label, false).Clicked() {
					v.showText = !v.showText
				}
			})
			if v.showText {
				for _, txt := range []string{p.Summary, p.SemDiff} {
					if txt == "" {
						continue
					}
					ui.Text(c, strings.TrimRight(txt, "\n")).Font("monospace").FontSize(11).Wrap().Selectable().
						Padding(8, 10).Radius(plat.innerRadius).Background(pal(t).code).Border(1, pal(t).groupLine)
				}
			}
		}
	})

	a.adoptCard(c, s, p)
}

func changeList(c *ui.Context, key, title string, list []rpc.TakeoverChange, col ui.Color) {
	if len(list) == 0 {
		return
	}
	t := c.Theme()
	ui.Column(c.Key(key)).Gap(4).Children(func() {
		ui.Text(c, fmt.Sprintf("%s（%d）", title, len(list))).FontWeight(600)
		for i, ch := range list {
			ui.Row(c.Key(fmt.Sprint(i))).Gap(8).AlignItems(ui.Start).Children(func() {
				dot(c, col, 6).Margin(5, 0, 0, 0)
				ui.Column(c).Gap(1).Shrink(1).Children(func() {
					ui.Text(c, ch.Area+" · "+ch.What).FontSize(12).Wrap().Selectable()
					if ch.Why != "" {
						ui.Text(c, ch.Why).FontSize(11).TextColor(t.TextMuted).Wrap()
					}
				})
			})
		}
	})
}

func (a *app) adoptCard(c *ui.Context, s status.Snapshot, p *rpc.TakeoverPlan) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	card(c, T.TakeoverAdopt, func() {
		switch p.State {
		case rpc.PlanAdopted:
			at := ""
			if !p.AdoptedAt.IsZero() {
				at = p.AdoptedAt.Local().Format("2006-01-02 15:04")
			}
			ui.Text(c, fmt.Sprintf(T.TakeoverAdoptedAt, at)).FontSize(13).TextColor(pal(t).okText)
			if p.Backup != "" {
				kv(c, T.TakeoverBackup, p.Backup)
			}
			return
		case rpc.PlanRolledBack, rpc.PlanFailed:
			ui.Text(c, T.PlanState(p.State)+" · "+T.TakeoverReplan).FontSize(13).TextColor(pal(t).warnText).Wrap()
			return
		}
		ui.Text(c, T.TakeoverAdoptHint).FontSize(12).TextColor(t.TextMuted).Wrap()
		for i, step := range T.TakeoverAdoptSteps {
			ui.Text(c.Key(fmt.Sprint("s", i)), fmt.Sprintf("%d. %s", i+1, step)).FontSize(12).Wrap()
		}
		ok := a.helperWrites(s) && p.Validated && !v.fixture
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if !p.Validated {
				ui.Text(c, T.TakeoverNotValid).FontSize(12).TextColor(pal(t).badText).Grow(1).Wrap()
			} else if !p.Equivalent {
				ui.Text(c, T.TakeoverCheckChanges).FontSize(12).TextColor(pal(t).warnText).Grow(1).Wrap()
			} else {
				ui.Spacer(c)
			}
			if a.opButton(c.Key("adopt"), iconTakeover, T.TakeoverAdopt, true, T.TakeoverAdopt).Disabled(!ok).Clicked() {
				plan := *p
				a.confirm(T.TakeoverAdoptTitle, fmt.Sprintf(T.TakeoverAdoptBody, plan.LiveConfig), T.TakeoverAdopt, func() {
					a.takeoverAdopt(plan)
				})
			}
		})
	})
}

func (a *app) takeoverAdopt(p rpc.TakeoverPlan) {
	T := i18n.T
	v := &a.hd.take
	var r rpc.AdoptResult
	a.helperOp(T.TakeoverAdopt, 10*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverAdopt, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}, &r)
	}, func(err error) {
		if err == nil {
			v.adopt = &r
			if !r.OK {
				a.hd.msg, a.hd.msgErr = T.ErrorPrefix+orDash(r.Error), true
			}
		}
		a.loadTakeover()
		a.refreshCredentials()
		a.runHealth()
	})
}

func (a *app) adoptResultCard(c *ui.Context, r *rpc.AdoptResult) {
	t := c.Theme()
	T := i18n.T
	card(c, T.TakeoverResult, func() {
		head, col := T.TakeoverDone, t.Success
		switch {
		case r.RolledBack:
			head, col = T.TakeoverRolledBack, t.Warning
		case !r.OK:
			head, col = T.TakeoverFailed, t.Danger
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, col, 10)
			ui.Text(c, head).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		})
		if r.Error != "" {
			ui.Text(c, T.ErrorPrefix+r.Error).FontSize(12).TextColor(pal(t).badText).Wrap().Selectable()
		}
		ui.Column(c).Gap(3).Children(func() {
			for i, st := range r.Steps {
				ui.Row(c.Key(fmt.Sprint("st", i))).Gap(8).AlignItems(ui.Start).Children(func() {
					icon, icol := stepIcon(t, st.Status)
					ui.Icon(c, icon).FontSize(13).TextColor(icol)
					ui.Text(c, st.Name).FontSize(12).Width(160).Shrink(0)
					ui.Text(c, st.Detail).FontSize(11).TextColor(t.TextMuted).Wrap().Shrink(1).Selectable()
				})
			}
		})
		if r.HistoryID != "" {
			kv(c, T.TakeoverHistory, r.HistoryID)
		}
		if r.Backup != "" {
			kv(c, T.TakeoverBackup, r.Backup)
		}
		if len(r.HealthBefore)+len(r.HealthAfter) > 0 {
			ui.Divider(c)
			healthCompare(c, r)
		}
		for _, w := range r.Warnings {
			warningNote(c, w)
		}
	})
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

// healthCompare is a table of the checks: before, after, after rollback.
func healthCompare(c *ui.Context, r *rpc.AdoptResult) {
	t := c.Theme()
	T := i18n.T
	worse := map[string]bool{}
	for _, id := range r.Worse {
		worse[id] = true
	}
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
	ui.Text(c, T.TakeoverHealth).FontWeight(600)
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "").Width(200).Shrink(0)
		for _, h := range cols {
			ui.Text(c, h).FontSize(11).TextColor(t.TextMuted).Width(90).Shrink(0)
		}
	})
	cell := func(ch rpc.AdoptCheck, ok bool) {
		if !ok {
			ui.Text(c, "—").FontSize(12).TextColor(t.TextMuted).Width(90).Shrink(0)
			return
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Width(90).Shrink(0).Children(func() {
			dot(c, checkColor(t, ch.Status), 8)
			ui.Text(c, ch.Status).FontSize(12)
		})
	}
	for _, id := range ids {
		ui.Row(c.Key("h-" + id)).Gap(10).AlignItems(ui.Center).Children(func() {
			name := id
			if worse[id] {
				name += " ↓"
			}
			col := t.Text
			if worse[id] {
				col = t.Danger
			}
			ui.Text(c, name).FontSize(12).TextColor(col).Width(200).Shrink(0).SingleLine().Tooltip(name)
			b, ok := before[id]
			cell(b, ok)
			af, ok := after[id]
			cell(af, ok)
			if len(r.HealthAfterRollback) > 0 {
				rb, ok := back[id]
				cell(rb, ok)
			}
		})
	}
	if len(r.Worse) > 0 {
		ui.Text(c, fmt.Sprintf(T.TakeoverWorse, strings.Join(r.Worse, ", "))).FontSize(12).TextColor(pal(t).badText).Wrap()
	}
}

func checkColor(t *ui.Theme, s string) ui.Color {
	switch s {
	case "ok":
		return t.Success
	case "warn":
		return t.Warning
	case "fail":
		return t.Danger
	default:
		return t.TextMuted
	}
}

// secureCard offers controller.secure: a new controller secret, kept in
// the helper's secret store and applied. It is its own step because every
// other client of the controller (a web panel, scripts) needs the new
// secret afterwards.
func (a *app) secureCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.take
	iconCard(c, iconSecure, T.SecureTitle, func() {
		ui.Text(c, T.SecureHint).FontSize(12).TextColor(t.TextMuted).Wrap()
		if p := v.plan; p != nil && !p.SecretSet && v.secure == nil {
			ui.Text(c, T.TakeoverNoSecret).FontSize(12).TextColor(pal(t).warnText)
		}
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			can := a.helperWrites(s) && !v.unsupported && !v.fixture
			if a.opButton(c.Key("secure"), iconSecure, T.SecureRun, false, T.SecureRun).Disabled(!can).Clicked() {
				a.confirm(T.SecureTitle+"？", T.SecureBody, T.SecureRun, a.controllerSecure)
			}
		})
		if r := v.secure; r != nil {
			msg := T.SecureDone
			if r.Replaced {
				msg = T.SecureReplaced
			}
			ui.Text(c, msg).FontSize(12).TextColor(pal(t).okText).Wrap()
			if r.Apply != nil && r.Apply.HistoryID != "" {
				kv(c, T.TakeoverHistory, r.Apply.HistoryID)
			}
			if r.Apply != nil && r.Apply.Error != "" {
				ui.Text(c, T.ErrorPrefix+r.Apply.Error).FontSize(12).TextColor(pal(t).badText).Wrap()
			}
			for _, w := range r.Warnings {
				warningNote(c, w)
			}
		}
	})
}

func (a *app) controllerSecure() {
	T := i18n.T
	v := &a.hd.take
	var r rpc.ControllerSecureResult
	a.helperOp(T.SecureRun, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodControllerSecure, nil, &r)
	}, func(err error) {
		if isTooOld(err) {
			v.unsupported = true
		}
		v.secure = nil
		if err == nil {
			v.secure = &r
		}
		a.refreshCredentials() // the new secret, without waiting for the event
	})
}

// takeoverEvent reports whether a batch changes the plan list.
func takeoverEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		if e.Type == rpc.EventTakeoverChanged || e.Type == rpc.EventComposeApplied {
			return true
		}
	}
	return false
}
