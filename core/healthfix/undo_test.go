package healthfix

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// world is everything a fix can change, as the fake engine and the fake
// mihomo/Tailscale hold it.
type world struct {
	Layers   []fakeLayer
	Coexist  bool
	Guard    []string
	Running  string
	Pointed  bool
	Disabled []string
	Mode     string
	Exit     string
}

func snapshot(h *fakeHelper, m *fakeMihomo, ts *fakeTS) world {
	w := world{Layers: slices.Clone(h.layers), Coexist: h.settings.TailscaleCoexist, Running: h.running,
		Pointed: h.pointed, Disabled: slices.Clone(h.disabled), Mode: m.mode, Exit: ts.exit}
	if len(h.settings.LeakGuardGroups) > 0 {
		w.Guard = slices.Clone(h.settings.LeakGuardGroups)
	}
	if len(w.Disabled) == 0 {
		w.Disabled = nil
	}
	return w
}

// seeds are the states a fix starts from: nothing of Twinnel's yet, and
// overrides of the fixes' ids written by hand (a disabled one among them)
// with the managed layers on.
var seeds = map[string]func(h *fakeHelper){
	"empty": func(h *fakeHelper) {
		h.put(rpc.LayerContent{Layer: rpc.Layer{ID: "my-rules", Name: "My rules", Stage: "user", Format: "merge", Enabled: true},
			Content: "+rules:\n  - DOMAIN,example.org,DIRECT\n"})
	},
	"existing": func(h *fakeHelper) {
		h.put(rpc.LayerContent{Layer: rpc.Layer{ID: health.TUNStackLayerID, Name: "My TUN", Stage: "user", Format: "merge", Enabled: true},
			Content: "tun:\n  stack: mixed\n"})
		h.put(rpc.LayerContent{Layer: rpc.Layer{ID: LoopbackLayerID, Name: "Controller", Stage: "user", Format: "verge", Enabled: false},
			Content: "external-controller: 0.0.0.0:9090\n"})
		h.put(rpc.LayerContent{Layer: rpc.Layer{ID: "my-rules", Name: "My rules", Stage: "user", Format: "merge", Enabled: true},
			Content: "+rules:\n  - DOMAIN,example.org,DIRECT\n"})
		h.settings.TailscaleCoexist = true
		h.settings.LeakGuardGroups = []string{"Streaming"}
	},
}

// TestEveryUndoRestores runs every one-click fix with an undo, on every
// platform and from every seed, against the fake engine, then its undo,
// and checks that the layer store, the running config and everything
// else are back to the pre-fix state, and that the running config is what
// the store composes (so the next apply keeps the undo). It also checks
// that the undo's preview says the check fails again.
func TestEveryUndoRestores(t *testing.T) {
	ctx := context.Background()
	ran := map[string]bool{}
	for _, reason := range health.Reasons() {
		for _, plat := range health.AllPlatforms {
			for seedName, seed := range seeds {
				h := newFakeHelper()
				seed(h)
				h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{}) // the running config is the store's
				h.fixed = false
				m := &fakeMihomo{mode: "global"}
				ts := &fakeTS{exit: "peer-1"}
				f := &Fixer{Helper: h, Mihomo: m, Tailscale: ts, Lang: "en", Platform: plat}
				cr := health.CheckReport{ID: "the-check", Result: health.Result{Status: health.StatusFail, Reasons: []string{reason},
					Fix: &health.FixHint{Param: "Google"}}}
				p, err := f.Plan(ctx, cr, reason)
				if err != nil || p.Action == "" || p.Blocked != "" || !p.Undo {
					continue
				}
				t.Run(reason+"@"+plat+"/"+seedName, func(t *testing.T) {
					pre := snapshot(h, m, ts)
					out, err := f.Apply(ctx, p)
					if err != nil {
						t.Fatal(err)
					}
					if out.Undo == nil {
						t.Fatalf("plan says undo, apply returned none")
					}
					if reflect.DeepEqual(snapshot(h, m, ts), pre) {
						t.Fatalf("%s changed nothing", p.Action)
					}
					if h.running != h.compose() {
						t.Fatalf("after the fix the running config is not the store's:\n%s\nvs\n%s", h.running, h.compose())
					}
					// The undo goes through JSON, as the CLI and MCP pass it.
					u := roundTrip(t, out.Undo)
					up := f.UndoPreview(u)
					if len(up.Steps) == 0 || up.Warning == "" {
						t.Fatalf("undo preview %+v", up)
					}
					if text, _ := health.FixTextFor("en", plat, reason); !strings.Contains(up.Warning, text.Plain) {
						t.Errorf("undo warning %q does not name the problem %q", up.Warning, text.Plain)
					}
					if _, err := f.Undo(ctx, u); err != nil {
						t.Fatalf("undo: %v", err)
					}
					post := snapshot(h, m, ts)
					if !reflect.DeepEqual(post, pre) {
						t.Fatalf("undo did not go back:\npre  %+v\npost %+v", pre, post)
					}
					if h.running != h.compose() {
						t.Fatalf("after the undo the running config is not the store's")
					}
					ran[p.Action] = true
				})
			}
		}
	}
	for _, a := range []string{health.FixCoexist, health.FixLeakGuard, health.FixControllerLoopback, health.FixTUNStack,
		health.FixSystemDNS, health.FixModeRule, health.FixExitNodeOff, health.FixDisableAutostart} {
		if !ran[a] {
			t.Errorf("no reason ran %s with an undo", a)
		}
	}
}

func roundTrip(t *testing.T, u *Undo) *Undo {
	t.Helper()
	var out Undo
	b, err := json.Marshal(u)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return &out
}

// B4: the tun-stack fix over an override written by hand, undone. The
// store holds the hand-written override again, and an apply after the
// undo (auto-apply on a tailnet change) keeps the pre-fix config.
func TestTUNStackUndoOverExisting(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	seeds["existing"](h)
	h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{})
	pre := h.running
	f := &Fixer{Helper: h, Lang: "zh-Hans", Platform: health.Windows}
	cr := health.CheckReport{ID: "tun-dataplane", Result: health.Result{Status: health.StatusFail, Reasons: []string{health.ReasonDataplaneTCP}}}
	p, _ := f.Plan(ctx, cr, health.ReasonDataplaneTCP)
	out, err := f.Apply(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if l := h.layers[0]; l.Name != "My TUN" || !strings.Contains(l.Content, "gvisor") {
		t.Fatalf("fix wrote %+v (want the user's name kept)", l)
	}
	if _, err := f.Undo(ctx, out.Undo); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.layer(health.TUNStackLayerID); c != "tun:\n  stack: mixed\n" || h.running != pre {
		t.Fatalf("after undo: layer %q, running ok %v", c, h.running == pre)
	}
	h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{})
	if h.running != pre {
		t.Fatal("the next apply brought the fix back")
	}
	if w := f.UndoPreview(out.Undo).Warning; !strings.Contains(w, "再出现") {
		t.Errorf("warning %q", w)
	}
}

// An undo whose apply fails leaves the fix in the store too: the store
// never disagrees with what runs.
func TestUndoApplyFailsKeepsFix(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	seeds["existing"](h)
	h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{})
	f := &Fixer{Helper: h, Lang: "en", Platform: health.Darwin}
	cr := health.CheckReport{ID: "tun-dataplane", Result: health.Result{Reasons: []string{health.ReasonDataplaneTCP}}}
	p, _ := f.Plan(ctx, cr, health.ReasonDataplaneTCP)
	out, err := f.Apply(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fixed := h.running
	h.failOn, h.seen = rpc.MethodComposeApply, map[string]int{}
	if _, err := f.Undo(ctx, out.Undo); err == nil {
		t.Fatal("undo error lost")
	}
	if h.running != fixed || h.compose() != fixed {
		t.Fatal("a failed undo left the store and the running config apart")
	}
}

// The undo printed by older versions (a history id) puts the override
// back from that entry, then applies: never a bare rollback.
func TestLegacyHistoryUndo(t *testing.T) {
	ctx := context.Background()
	h := newFakeHelper()
	seeds["existing"](h)
	h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{})
	pre, before := h.running, h.history[len(h.history)-1].id
	// What the old fix did: write the override, apply.
	h.Call(ctx, rpc.MethodLayerPut, rpc.LayerPutParams{ID: health.TUNStackLayerID, Content: health.TUNStackLayer}, nil)
	h.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{}, &rpc.ApplyResult{})
	f := &Fixer{Helper: h, Lang: "en", Platform: health.Windows}
	u := &Undo{Action: health.FixTUNStack, HistoryID: before}
	if up := f.UndoPreview(u); len(up.Steps) != 2 || up.Warning == "" {
		t.Fatalf("preview %+v", up)
	}
	if _, err := f.Undo(ctx, u); err != nil {
		t.Fatal(err)
	}
	if h.running != pre || h.compose() != pre || slices.Contains(h.calls, rpc.MethodHistoryRollback) {
		t.Fatalf("legacy undo: running back %v, store back %v, calls %v", h.running == pre, h.compose() == pre, h.calls)
	}
	// An old coexist undo stood for nothing stored: refused, not rolled back.
	if _, err := f.Undo(ctx, &Undo{Action: health.FixCoexist, HistoryID: before}); err == nil {
		t.Fatal("old coexist undo ran")
	}
}

func TestSettingsLike(t *testing.T) {
	on := true
	groups := []string{"A"}
	got, err := settingsLike(&rpc.SettingsUpdate{TailscaleCoexist: &on, LeakGuardGroups: &groups}, rpc.Settings{})
	if err != nil || got.TailscaleCoexist == nil || *got.TailscaleCoexist || got.LeakGuardGroups == nil || len(*got.LeakGuardGroups) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
