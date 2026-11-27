package healthfix

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// fakeHelper answers the helper methods the fixes use and remembers the
// state they change. health.run reports the fixed check as ok once a
// write that fixes it happened.
type fakeHelper struct {
	calls    []string
	settings rpc.Settings
	layers   map[string]string
	dns      rpc.DNSStatus
	fixed    bool
	failOn   string
}

func newFakeHelper() *fakeHelper {
	return &fakeHelper{layers: map[string]string{},
		dns: rpc.DNSStatus{Supported: true, Target: []string{"127.0.0.1"}, Services: []rpc.DNSService{{Name: "Wi-Fi", Servers: []string{"192.168.1.1"}}}}}
}

func (h *fakeHelper) Call(_ context.Context, method string, params, result any) error {
	h.calls = append(h.calls, method)
	if method == h.failOn {
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
			h.settings.LeakGuardGroups = *u.LeakGuardGroups
		}
	case rpc.MethodLayerPut:
		p := params.(rpc.LayerPutParams)
		h.layers[p.ID] = p.Content
	case rpc.MethodLayerDelete:
		delete(h.layers, params.(rpc.IDParams).ID)
	case rpc.MethodDNSStatus:
		set(h.dns)
	case rpc.MethodHistoryList:
		set([]rpc.HistoryEntry{{ID: "h1", Time: time.Unix(100, 0), Kind: "applied"}, {ID: "h2", Time: time.Unix(200, 0), Kind: "applied"}})
	case rpc.MethodAutostartChange:
		p := params.(rpc.AutostartChangeParams)
		r := rpc.AutostartChangeResult{Steps: []string{`C:\Windows\System32\schtasks.exe /Change /TN \synthetic /Disable`}}
		if !p.Preview {
			h.fixed = p.Op == "disable"
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
	case rpc.MethodComposeApply, rpc.MethodControllerSecure, rpc.MethodDNSPoint, rpc.MethodServiceRestart, rpc.MethodHealthEgress:
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

	// Coexist already on: undo rolls back to the newest history entry.
	h = newFakeHelper()
	h.settings.TailscaleCoexist = true
	f.Helper = h
	p, _ = f.Plan(ctx, cr, health.ReasonExcludeMissing)
	out, _ = f.Apply(ctx, p)
	if out.Undo == nil || out.Undo.HistoryID != "h2" {
		t.Fatalf("undo %+v, want history h2", out.Undo)
	}

	// Loopback layer: added, and removed on undo.
	h = newFakeHelper()
	f.Helper = h
	cr.Fix = &health.FixHint{Param: "9097"}
	p, _ = f.Plan(ctx, cr, health.ReasonCtlExposed)
	out, _ = f.Apply(ctx, p)
	if !strings.Contains(h.layers[LoopbackLayerID], "127.0.0.1:9097") {
		t.Fatalf("layer %q", h.layers[LoopbackLayerID])
	}
	f.Undo(ctx, out.Undo)
	if _, ok := h.layers[LoopbackLayerID]; ok {
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
