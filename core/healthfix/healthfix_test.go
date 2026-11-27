package healthfix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// fakeHelper is a fake engine: it answers the helper methods the fixes
// use and keeps the state they change, as the helper does. The layer
// store (layers, settings) is what layer.* and settings.* change; the
// running config is what compose.apply composes from the store, and what
// history.rollback puts back without touching the store. health.run
// reports the fixed check as ok once a write that fixes it happened.
type fakeHelper struct {
	calls    []string
	settings rpc.Settings
	layers   []fakeLayer // stored, in order
	running  string      // the running config (composed)
	history  []fakeEntry
	dns      rpc.DNSStatus
	pointed  bool
	disabled []string // autostart entries off
	fixed    bool
	failOn   string
	failAt   int // fail the failOn call number failAt (0: every one)
	seen     map[string]int
}

type fakeLayer struct {
	rpc.LayerContent
}

type fakeEntry struct {
	id      string
	running string
	layers  []fakeLayer
}

func newFakeHelper() *fakeHelper {
	return &fakeHelper{seen: map[string]int{},
		dns: rpc.DNSStatus{Supported: true, Target: []string{"127.0.0.1"}, Services: []rpc.DNSService{{Name: "Wi-Fi", Servers: []string{"192.168.1.1"}}}}}
}

// layer is the content of a stored layer ("" when there is none).
func (h *fakeHelper) layer(id string) (string, bool) {
	for _, l := range h.layers {
		if l.ID == id {
			return l.Content, true
		}
	}
	return "", false
}

func (h *fakeHelper) put(l rpc.LayerContent) {
	for i := range h.layers {
		if h.layers[i].ID == l.ID {
			h.layers[i].LayerContent = l
			return
		}
	}
	h.layers = append(h.layers, fakeLayer{l})
}

// compose is the config the store composes to.
func (h *fakeHelper) compose() string {
	var b strings.Builder
	for _, l := range h.layers {
		if l.Enabled {
			b.WriteString("# " + l.ID + "\n" + l.Content + "\n")
		}
	}
	if h.settings.TailscaleCoexist {
		b.WriteString("# coexist\n")
	}
	if len(h.settings.LeakGuardGroups) > 0 {
		b.WriteString("# guard " + strings.Join(h.settings.LeakGuardGroups, ",") + "\n")
	}
	return b.String()
}

func (h *fakeHelper) Call(_ context.Context, method string, params, result any) error {
	h.calls = append(h.calls, method)
	h.seen[method]++
	if method == h.failOn && (h.failAt == 0 || h.failAt == h.seen[method]) {
		return errors.New("refused by the fake")
	}
	set := func(v any) {
		if result == nil {
			return
		}
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, result)
	}
	switch method {
	case rpc.MethodSettingsGet:
		set(h.settings)
	case rpc.MethodSettingsUpdate:
		u := params.(rpc.SettingsUpdate)
		if u.TailscaleCoexist != nil {
			h.settings.TailscaleCoexist = *u.TailscaleCoexist
		}
		if u.LeakGuardGroups != nil {
			h.settings.LeakGuardGroups = slices.Clone(*u.LeakGuardGroups)
		}
	case rpc.MethodLayerGet:
		id := params.(rpc.IDParams).ID
		for _, l := range h.layers {
			if l.ID == id {
				set(l.LayerContent)
				return nil
			}
		}
		return rpc.ErrNotFound("layer", id)
	case rpc.MethodLayerPut:
		p := params.(rpc.LayerPutParams)
		l := rpc.LayerContent{Layer: rpc.Layer{ID: p.ID, Name: p.ID, Stage: "user", Format: "merge", Enabled: true}}
		for _, x := range h.layers {
			if x.ID == p.ID {
				l = x.LayerContent
			}
		}
		if p.Name != "" {
			l.Name = p.Name
		}
		if p.Stage != "" {
			l.Stage = p.Stage
		}
		if p.Format != "" {
			l.Format = p.Format
		}
		if p.Enabled != nil {
			l.Enabled = *p.Enabled
		}
		l.Content = p.Content
		h.put(l)
	case rpc.MethodLayerDelete:
		id := params.(rpc.IDParams).ID
		i := slices.IndexFunc(h.layers, func(l fakeLayer) bool { return l.ID == id })
		if i < 0 {
			return rpc.ErrNotFound("layer", id)
		}
		h.layers = slices.Delete(h.layers, i, i+1)
	case rpc.MethodLayerRestore:
		p := params.(rpc.LayerRestoreParams)
		i := slices.IndexFunc(h.history, func(e fakeEntry) bool { return e.id == p.HistoryID })
		if i < 0 {
			return rpc.ErrNotFound("history entry", p.HistoryID)
		}
		for _, id := range p.IDs {
			j := slices.IndexFunc(h.history[i].layers, func(l fakeLayer) bool { return l.ID == id })
			if j < 0 {
				return rpc.ErrInvalid("layer %s is not in history entry %s", id, p.HistoryID)
			}
			h.put(h.history[i].layers[j].LayerContent)
		}
	case rpc.MethodDNSStatus:
		set(h.dns)
	case rpc.MethodHistoryList:
		var out []rpc.HistoryEntry
		for i, e := range h.history {
			out = append(out, rpc.HistoryEntry{ID: e.id, Time: time.Unix(int64(100*(i+1)), 0), Kind: "applied"})
		}
		set(out)
	case rpc.MethodHistoryRollback:
		id := params.(rpc.IDParams).ID
		i := slices.IndexFunc(h.history, func(e fakeEntry) bool { return e.id == id })
		if i < 0 {
			return rpc.ErrNotFound("history entry", id)
		}
		h.running = h.history[i].running // the store is left as it is
	case rpc.MethodAutostartChange:
		p := params.(rpc.AutostartChangeParams)
		r := rpc.AutostartChangeResult{Steps: []string{`C:\Windows\System32\schtasks.exe /Change /TN \synthetic /Disable`}}
		if !p.Preview {
			h.fixed = p.Op == "disable"
			if h.fixed {
				h.disabled = slices.Clone(p.IDs)
			} else {
				h.disabled = nil
			}
			r.Undo = &rpc.AutostartChangeParams{IDs: p.IDs, Op: "enable"}
		}
		set(r)
	case rpc.MethodTailscaleInstall, rpc.MethodKernelInstall:
		h.fixed = true
	case rpc.MethodControllerMove:
		if p := params.(rpc.ControllerMoveParams); p.DryRun {
			set(rpc.ControllerMoveResult{From: "127.0.0.1:9090", To: "127.0.0.1:9097", DryRun: true})
			break
		}
		h.fixed = true
	case rpc.MethodComposeApply:
		h.fixed = true
		h.running = h.compose()
		h.history = append(h.history, fakeEntry{id: fmt.Sprintf("h%d", len(h.history)+1), running: h.running, layers: slices.Clone(h.layers)})
	case rpc.MethodDNSPoint:
		h.fixed, h.pointed = true, true
	case rpc.MethodDNSRestore:
		h.pointed = false
	case rpc.MethodControllerSecure, rpc.MethodServiceRestart, rpc.MethodHealthEgress:
		h.fixed = true
		if method == rpc.MethodHealthEgress {
			set(h.report())
		}
	case rpc.MethodHealthRun:
		set(h.report())
	}
	return nil
}

func (h *fakeHelper) report() rpc.HealthReport {
	st := health.StatusFail
	if h.fixed {
		st = health.StatusOK
	}
	rep := health.Report{Overall: st, Checks: []health.CheckReport{{ID: "the-check", Result: health.Result{Status: st}}}}
	b, _ := json.Marshal(rep)
	return rpc.HealthReport{Report: b}
}

type fakeMihomo struct{ mode string }

func (m *fakeMihomo) Mode(context.Context) (string, error) { return m.mode, nil }
func (m *fakeMihomo) SetMode(_ context.Context, s string) error {
	m.mode = s
	return nil
}

type fakeTS struct{ exit string }

func (t *fakeTS) ExitNode(context.Context) (string, error) { return t.exit, nil }
func (t *fakeTS) SetExitNode(_ context.Context, id string) error {
	t.exit = id
	return nil
}

// TestEveryReasonFixes: for every reason on every platform, the plan has
// its texts; a one-click fix runs against fakes and the check comes back
// ok; a guided or manual one says where to go or why.
func TestEveryReasonFixes(t *testing.T) {
	for _, reason := range health.Reasons() {
		for _, plat := range health.AllPlatforms {
			t.Run(reason+"@"+plat, func(t *testing.T) {
				h := newFakeHelper()
				m := &fakeMihomo{mode: "global"}
				ts := &fakeTS{exit: "peer-1"}
				fixed := false
				f := &Fixer{Helper: h, Mihomo: m, Tailscale: ts, Lang: "zh-Hans", Platform: plat}
				cr := health.CheckReport{ID: "the-check", Result: health.Result{Status: health.StatusFail, Reasons: []string{reason},
					Fix: &health.FixHint{Param: "Google"}}}
				p, err := f.Plan(context.Background(), cr, reason)
				if err != nil {
					t.Fatal(err)
				}
				if p.Text.Plain == "" || p.Text.How == "" {
					t.Fatalf("plan without texts: %+v", p)
				}
				switch p.Kind {
				case health.FixGuided:
					if p.Target == "" {
						t.Fatal("guided without target")
					}
					return
				case health.FixManual:
					if p.Action == "" {
						return
					}
				}
				if p.Blocked != "" {
					t.Fatalf("blocked with every dependency: %s", p.Blocked)
				}
				if len(p.Steps) == 0 {
					t.Fatal("no steps to preview")
				}
				out, err := f.Apply(context.Background(), p)
				if err != nil {
					t.Fatal(err)
				}
				switch p.Action {
				case health.FixModeRule:
					fixed = m.mode == "rule"
				case health.FixExitNodeOff:
					fixed = ts.exit == ""
				case health.FixRerun:
					fixed = slices.Contains(h.calls, rpc.MethodHealthRun)
				default:
					fixed = h.fixed && out.Check != nil && out.Check.Status == health.StatusOK
				}
				if !fixed {
					t.Fatalf("%s did not fix: calls %v, check %+v", p.Action, h.calls, out.Check)
				}
				if p.Undo != (out.Undo != nil) {
					t.Fatalf("plan says undo=%v, apply returned %+v", p.Undo, out.Undo)
				}
				if out.Undo != nil {
					if _, err := f.Undo(context.Background(), out.Undo); err != nil {
						t.Fatalf("undo: %v", err)
					}
				}
			})
		}
	}
}

func TestUndoPutsBack(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	f := &Fixer{Helper: h, Lang: "en", Platform: health.Darwin}
	cr := health.CheckReport{ID: "the-check", Result: health.Result{Reasons: []string{health.ReasonExcludeMissing}}}

	// Coexist off: turned on, and off again on undo.
	p, _ := f.Plan(ctx, cr, health.ReasonExcludeMissing)
	if !strings.Contains(strings.Join(p.Steps, "\n"), "Turn on") {
		t.Errorf("steps %v do not say the override is turned on", p.Steps)
	}
	out, err := f.Apply(ctx, p)
	if err != nil || !h.settings.TailscaleCoexist {
		t.Fatalf("apply: %v, coexist %v", err, h.settings.TailscaleCoexist)
	}
	if _, err := f.Undo(ctx, out.Undo); err != nil || h.settings.TailscaleCoexist {
		t.Fatalf("undo: %v, coexist %v", err, h.settings.TailscaleCoexist)
	}

	// Coexist already on: the fix only regenerates the override from the
	// tailnet; nothing stored changes, so there is no undo (a rollback
	// would leave the running config behind the store).
	h = newFakeHelper()
	h.settings.TailscaleCoexist = true
	f.Helper = h
	p, _ = f.Plan(ctx, cr, health.ReasonExcludeMissing)
	if p.Undo {
		t.Error("plan offers an undo with coexist on already")
	}
	out, _ = f.Apply(ctx, p)
	if out.Undo != nil {
		t.Fatalf("undo %+v, want none", out.Undo)
	}

	// Loopback layer: added, and removed on undo.
	h = newFakeHelper()
	f.Helper = h
	cr.Fix = &health.FixHint{Param: "9097"}
	p, _ = f.Plan(ctx, cr, health.ReasonCtlExposed)
	out, _ = f.Apply(ctx, p)
	if c, _ := h.layer(LoopbackLayerID); !strings.Contains(c, "127.0.0.1:9097") {
		t.Fatalf("layer %q", c)
	}
	f.Undo(ctx, out.Undo)
	if _, ok := h.layer(LoopbackLayerID); ok {
		t.Fatal("layer kept after undo")
	}

	// Leak guard: groups added, then the old (empty) list back.
	h = newFakeHelper()
	f.Helper = h
	cr.Fix = &health.FixHint{Param: "Google,Meta"}
	p, _ = f.Plan(ctx, cr, health.ReasonLeakMayGo)
	out, _ = f.Apply(ctx, p)
	if !slices.Equal(h.settings.LeakGuardGroups, []string{"Google", "Meta"}) {
		t.Fatalf("groups %v", h.settings.LeakGuardGroups)
	}
	f.Undo(ctx, out.Undo)
	if len(h.settings.LeakGuardGroups) != 0 {
		t.Fatalf("groups after undo %v", h.settings.LeakGuardGroups)
	}
}

// A failed apply puts the settings back.
func TestFailedApplyReverts(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	h.failOn = rpc.MethodComposeApply
	f := &Fixer{Helper: h, Lang: "en", Platform: health.Darwin}
	cr := health.CheckReport{ID: "the-check", Result: health.Result{Reasons: []string{health.ReasonNamesNone}}}
	p, _ := f.Plan(ctx, cr, health.ReasonNamesNone)
	if _, err := f.Apply(ctx, p); err == nil {
		t.Fatal("want the apply's error")
	}
	if h.settings.TailscaleCoexist {
		t.Fatal("coexist left on after a failed apply")
	}
}

// Without the helper, helper fixes are blocked with a reason, not run.
func TestBlockedWithoutHelper(t *testing.T) {
	f := &Fixer{Lang: "zh-Hans", Platform: health.Darwin}
	cr := health.CheckReport{ID: "system-dns", Result: health.Result{Reasons: []string{health.ReasonDNSNotMihomo}}}
	p, _ := f.Plan(context.Background(), cr, health.ReasonDNSNotMihomo)
	if p.Blocked == "" {
		t.Fatal("not blocked without the helper")
	}
	if _, err := f.Apply(context.Background(), p); err == nil {
		t.Fatal("applied without the helper")
	}
}

// Plans folds reasons that share one fix into one plan.
func TestPlansDedupe(t *testing.T) {
	f := &Fixer{Helper: newFakeHelper(), Lang: "en", Platform: health.Darwin}
	cr := health.CheckReport{ID: "leak-guard", Result: health.Result{Reasons: []string{health.ReasonLeakSelected, health.ReasonLeakMayGo, health.ReasonLeakMode}}}
	ps := f.Plans(context.Background(), cr)
	if len(ps) != 2 {
		t.Fatalf("got %d plans, want 2 (leak guard, mode): %+v", len(ps), ps)
	}
}
