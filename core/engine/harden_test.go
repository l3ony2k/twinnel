package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/engine/enginetest"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

const oldLabel = "com.example.proxy"

// launchFake is a service.Manager and Executor that behave like launchd for
// one machine: plists in a temp dir, a loaded/running state per label,
// and a fake mihomo controller that stops on bootout and starts on
// kickstart with the config its plist names.
type launchFake struct {
	mu      sync.Mutex
	dir     string
	loaded  map[string]bool
	plists  map[string]string // label → plist path it was bootstrapped from
	runs    [][]string
	ctrl    *enginetest.Controller
	cfgOf   map[string]string // label → config file the "process" loads
	failNew bool              // the new label never answers
	failCmd string            // a launchctl verb that fails
}

func (f *launchFake) Render(s service.Spec) ([]byte, error) {
	return []byte(fmt.Sprintf("label=%s\nprogram=%s\nargs=%s\n", s.Label, s.Program, strings.Join(s.Args, " "))), nil
}
func (f *launchFake) DefinitionPath(l string) string { return filepath.Join(f.dir, l+".plist") }
func (f *launchFake) Plan(op service.Op, s service.Spec) ([]service.Step, error) {
	lc := func(ignore bool, a ...string) service.Step {
		return service.Step{Kind: service.StepRun, Argv: append([]string{"launchctl"}, a...), IgnoreError: ignore}
	}
	t := "system/" + s.Label
	switch op {
	case service.OpInstall:
		b, _ := f.Render(s)
		return []service.Step{{Kind: service.StepMkdir, Path: filepath.Join(f.dir, "logs"), Mode: 0o755}, lc(true, "bootout", t),
			{Kind: service.StepWrite, Path: f.DefinitionPath(s.Label), Mode: 0o644, Content: b}, lc(false, "enable", t), lc(false, "bootstrap", "system", f.DefinitionPath(s.Label))}, nil
	case service.OpStart:
		return []service.Step{lc(true, "bootstrap", "system", f.DefinitionPath(s.Label)), lc(false, "kickstart", t)}, nil
	case service.OpStop:
		return []service.Step{lc(false, "bootout", t)}, nil
	case service.OpDisable, service.OpEnable:
		return []service.Step{lc(false, string(op), t)}, nil
	}
	return nil, fmt.Errorf("op %s", op)
}
func (f *launchFake) Status(_ context.Context, l string) (service.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := service.Status{Label: l, Definition: f.DefinitionPath(l), Loaded: f.loaded[l], Running: f.loaded[l]}
	if _, err := os.Stat(st.Definition); err == nil {
		st.Installed = true
	}
	return st, nil
}

func (f *launchFake) Run(_ context.Context, argv []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, argv)
	verb := argv[1]
	if verb == f.failCmd {
		return []byte("boom"), errors.New("exit status 5")
	}
	switch verb {
	case "bootout":
		l := strings.TrimPrefix(argv[2], "system/")
		if !f.loaded[l] {
			return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3")
		}
		f.loaded[l] = false
		f.ctrl.SetDown(true)
	case "bootstrap":
		l := strings.TrimSuffix(filepath.Base(argv[3]), ".plist")
		if f.loaded[l] {
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		if _, err := os.Stat(argv[3]); err != nil {
			return []byte("no such file"), err
		}
		f.loaded[l] = true
	case "kickstart":
		l := strings.TrimPrefix(argv[len(argv)-1], "system/")
		if !f.loaded[l] {
			return []byte("Could not find service"), errors.New("exit status 113")
		}
		if l == store.DefaultMihomoLabel && f.failNew {
			return nil, nil // starts, never answers
		}
		cfg, err := os.ReadFile(f.cfgOf[l])
		if err != nil {
			return nil, err
		}
		f.ctrl.Start(cfg)
	}
	return nil, nil
}

func (f *launchFake) verbs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.runs {
		out = append(out, strings.Join(r[1:], " "))
	}
	return out
}

type hardenHarness struct {
	*harness
	lf    *launchFake
	state string
}

func newHardenHarness(t *testing.T) *hardenHarness {
	h := newHarness(t)
	e := h.e
	state := e.Store.Dir
	lf := &launchFake{dir: t.TempDir(), loaded: map[string]bool{oldLabel: true}, ctrl: h.ctrl,
		cfgOf: map[string]string{oldLabel: h.cfgPath, store.DefaultMihomoLabel: filepath.Join(state, "mihomo", "config.yaml")}}
	os.WriteFile(lf.DefinitionPath(oldLabel), []byte("old plist"), 0o644)
	e.Services = lf
	e.ServiceRunner = &ServiceRunner{Exec: lf, Files: service.OSFiles{}}
	e.HardenStopTimeout, e.HardenStartTimeout, e.HardenPoll = 500*time.Millisecond, 500*time.Millisecond, 20*time.Millisecond

	// An adopted dir: geo files, cache.db, a dashboard, things that are
	// not copied, and a symlink out.
	cd := h.confDir
	for name, body := range map[string]string{
		"geoip.metadb": "mmdb", "ASN.mmdb": "asn", "cache.db": "cache",
		"ui/zashboard/index.html": "<html>", "ui/zashboard/assets/a.js": "js",
		"config.yaml.bak-before-ui": "old backup", "test.yaml": "scratch",
	} {
		p := filepath.Join(cd, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	victim := victim(t)
	os.Symlink(victim, filepath.Join(cd, "ui", "evil"))

	uid := os.Getuid()
	if _, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.ServiceLabel, st.Mihomo.ServiceAdopted, st.Mihomo.ConfigDirUID = oldLabel, true, &uid
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: "ui", Content: "external-ui: " + filepath.Join(cd, "ui") + "\nexternal-ui-name: zashboard\n"}); err != nil {
		t.Fatal(err)
	}
	h.apply("adopted state")
	h.ctrl.Start(h.disk())

	// The managed kernel v1.19.34 is installed (a fake binary).
	kdir := filepath.Join(e.Kernels.Dir, "v1.19.34")
	os.MkdirAll(kdir, 0o755)
	if _, err := enginetest.WriteFakeMihomo(kdir); err != nil {
		t.Fatal(err)
	}
	return &hardenHarness{harness: h, lf: lf, state: state}
}

func (h *hardenHarness) plan(p rpc.HardenPlanParams) rpc.HardenPlan {
	h.t.Helper()
	pl, err := h.e.HardenPlan(context.Background(), p)
	if err != nil {
		h.t.Fatal(err)
	}
	return pl
}

func copied(pl rpc.HardenPlan) []string {
	var out []string
	for _, f := range pl.Copy {
		out = append(out, f.Path)
	}
	return out
}

func TestHardenPlanAndApply(t *testing.T) {
	h := newHardenHarness(t)
	ctx := context.Background()
	oldCfg := h.disk()
	pl := h.plan(rpc.HardenPlanParams{})

	if pl.State != rpc.PlanReady || pl.ConfirmHash == "" || pl.DryRun || pl.Secret != "keep" {
		t.Fatalf("plan: %+v", pl)
	}
	if pl.From.Label != oldLabel || pl.To.Label != store.DefaultMihomoLabel || pl.To.ConfigDir != filepath.Join(h.state, "mihomo") || pl.Kernel.Version != "v1.19.34" || !pl.Kernel.Installed {
		t.Fatalf("from/to: %+v %+v %+v", pl.From, pl.To, pl.Kernel)
	}
	if got := strings.Join(copied(pl), ","); got != "ASN.mmdb,cache.db,geoip.metadb,ui" {
		t.Errorf("copy = %s", got)
	}
	if len(pl.Skipped) != 1 || !strings.Contains(pl.Skipped[0], "ui/evil") {
		t.Errorf("skipped = %q", pl.Skipped)
	}
	if !slices.ContainsFunc(pl.ConfigChanges, func(s string) bool { return strings.Contains(s, "external-ui: "+h.confDir+"/ui → ui") }) {
		t.Errorf("changes = %q", pl.ConfigChanges)
	}
	var cut []string
	for _, s := range pl.Steps {
		if s.Phase == "cut" {
			cut = append(cut, s.Text)
		}
	}
	if len(cut) < 3 || !strings.Contains(cut[0], "bootout system/"+oldLabel) || !strings.Contains(strings.Join(cut, "\n"), "bootstrap system") {
		t.Errorf("cut steps: %q", cut)
	}
	if pl.Downtime.MinSeconds <= 0 || pl.Downtime.MaxSeconds < pl.Downtime.MinSeconds || !strings.Contains(pl.Plist, "-d "+pl.To.ConfigDir) {
		t.Errorf("downtime/plist: %+v %q", pl.Downtime, pl.Plist)
	}
	// Planning changed nothing.
	if len(h.lf.verbs()) != 0 || !bytes.Equal(h.disk(), oldCfg) {
		t.Fatal("plan ran launchctl or wrote the config")
	}
	if _, err := os.Stat(pl.To.ConfigDir); err == nil {
		t.Fatal("plan created the new config dir")
	}
	listed, err := h.e.HardenPlans("")
	if err != nil || len(listed) != 1 || listed[0].ConfirmHash != pl.ConfirmHash {
		t.Fatalf("plans: %+v %v", listed, err)
	}

	if _, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: "nope", SkipHealth: true}); err == nil {
		t.Fatal("wrong confirm hash accepted")
	}
	if _, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash}); err == nil {
		t.Fatal("apply without health checks and without skip_health accepted")
	}
	res, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true})
	if err != nil || !res.OK || res.RolledBack || res.DryRun {
		t.Fatalf("apply: %+v %v", res, err)
	}
	want := []string{
		"enable system/" + store.DefaultMihomoLabel,
		"bootout system/" + oldLabel,
		"bootstrap system " + h.lf.DefinitionPath(store.DefaultMihomoLabel),
		"kickstart system/" + store.DefaultMihomoLabel,
		"disable system/" + oldLabel,
	}
	if got := h.lf.verbs(); !slices.Equal(got, want) {
		t.Errorf("launchctl:\n got %q\nwant %q", got, want)
	}

	st, _ := h.e.Store.Settings()
	m := st.Mihomo
	if m.Binary != "" || m.ConfigDir != pl.To.ConfigDir || m.ConfigFile != pl.To.ConfigFile || m.ConfigDirUID != nil || m.ServiceLabel != store.DefaultMihomoLabel || m.ServiceAdopted {
		t.Fatalf("settings: %+v", m)
	}
	if st.Hardened == nil || st.Hardened.OldLabel != oldLabel || st.Hardened.OldConfigDir != h.confDir || st.Hardened.OldPlist != h.lf.DefinitionPath(oldLabel) || st.Hardened.PlanID != pl.PlanID {
		t.Fatalf("hardened: %+v", st.Hardened)
	}
	// The old service and dir are untouched; the new dir has the copies.
	if b, _ := os.ReadFile(h.lf.DefinitionPath(oldLabel)); string(b) != "old plist" {
		t.Error("old plist changed")
	}
	if !bytes.Equal(h.disk(), oldCfg) {
		t.Error("old config changed")
	}
	newCfg, err := os.ReadFile(pl.To.ConfigFile)
	if err != nil || !bytes.Contains(newCfg, []byte("external-ui: ui\n")) || !bytes.Contains(newCfg, []byte("secret: "+testSecret)) {
		t.Fatalf("new config: %v\n%s", err, newCfg)
	}
	for _, f := range []string{"cache.db", "geoip.metadb", "ui/zashboard/assets/a.js"} {
		if _, err := os.Stat(filepath.Join(pl.To.ConfigDir, f)); err != nil {
			t.Error(err)
		}
	}
	for _, f := range []string{"test.yaml", "ui/evil", "config.yaml.bak-before-ui"} {
		if _, err := os.Lstat(filepath.Join(pl.To.ConfigDir, f)); err == nil {
			t.Errorf("%s was copied", f)
		}
	}
	if fi, _ := os.Stat(pl.To.ConfigDir); fi.Mode().Perm() != 0o700 {
		t.Errorf("new dir mode %v", fi.Mode().Perm())
	}
	if res.DowntimeMS <= 0 {
		t.Errorf("downtime %d", res.DowntimeMS)
	}

	// The next apply composes into the new dir, paths relocated.
	ar := h.apply("after harden")
	if ar.Error != "" {
		t.Fatal(ar.Error)
	}
	_, loaded, _ := h.ctrl.State()
	if !bytes.Contains(loaded, []byte("external-ui: ui\n")) {
		t.Errorf("reloaded config:\n%s", loaded)
	}
	if !bytes.Equal(h.disk(), oldCfg) {
		t.Error("apply after harden wrote the old config")
	}

	// Done: the plan can't run again, and a new plan has nothing to do.
	if _, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true}); err == nil {
		t.Error("applied twice")
	}
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{}); err == nil || !strings.Contains(err.Error(), "nothing to harden") {
		t.Errorf("re-plan: %v", err)
	}
}

func TestHardenRollsBackWhenNewDoesNotAnswer(t *testing.T) {
	h := newHardenHarness(t)
	ctx := context.Background()
	oldCfg := h.disk()
	h.lf.failNew = true
	pl := h.plan(rpc.HardenPlanParams{NewSecret: true})
	if pl.Secret != "new" {
		t.Fatalf("secret = %s", pl.Secret)
	}
	res, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true})
	if err != nil || res.OK || !res.RolledBack || !strings.Contains(res.Error, "did not answer") {
		t.Fatalf("result: %+v %v", res, err)
	}
	got := h.lf.verbs()
	tail := []string{
		"bootout system/" + store.DefaultMihomoLabel,
		"bootstrap system " + h.lf.DefinitionPath(oldLabel),
		"kickstart system/" + oldLabel,
	}
	if len(got) < 3 || !slices.Equal(got[len(got)-3:], tail) || slices.Contains(got, "disable system/"+oldLabel) {
		t.Errorf("launchctl: %q", got)
	}
	if v, err := h.e.currentController(mustSettings(t, h.e)).Version(ctx); err != nil || v == "" {
		t.Errorf("old controller after rollback: %v", err)
	}
	st := mustSettings(t, h.e)
	if st.Mihomo.ServiceLabel != oldLabel || st.Hardened != nil || st.Mihomo.ConfigDir != h.confDir {
		t.Errorf("settings changed: %+v", st.Mihomo)
	}
	if s, _ := h.e.Secrets.Get(store.SecretController); s != testSecret {
		t.Errorf("secret not restored: %q", s)
	}
	if _, err := os.Stat(h.lf.DefinitionPath(store.DefaultMihomoLabel)); err == nil {
		t.Error("new plist left behind")
	}
	if _, err := os.Stat(pl.To.ConfigDir); err == nil {
		t.Error("new config dir left in place")
	}
	dir, _ := h.e.Store.HardenDir(pl.PlanID)
	if _, err := os.Stat(filepath.Join(dir, hardenFailedDir, "config.yaml")); err != nil {
		t.Errorf("failed dir not kept: %v", err)
	}
	if !bytes.Equal(h.disk(), oldCfg) {
		t.Error("old config changed")
	}
	pls, _ := h.e.HardenPlans(pl.PlanID)
	if pls[0].State != rpc.PlanRolledBack {
		t.Errorf("state %s", pls[0].State)
	}
	// A new plan works again after the rollback.
	h.lf.failNew = false
	h.lf.runs = nil
	pl2 := h.plan(rpc.HardenPlanParams{})
	res, err = h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl2.PlanID, ConfirmHash: pl2.ConfirmHash, SkipHealth: true})
	if err != nil || !res.OK {
		t.Fatalf("second try: %+v %v", res, err)
	}
}

func TestHardenStopFailureChangesNothing(t *testing.T) {
	h := newHardenHarness(t)
	h.lf.failCmd = "bootout"
	pl := h.plan(rpc.HardenPlanParams{})
	res, err := h.e.HardenApply(context.Background(), rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true})
	if err != nil || res.OK || res.RolledBack || !strings.Contains(res.Error, "nothing changed") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(h.lf.DefinitionPath(store.DefaultMihomoLabel)); err == nil {
		t.Error("new plist left behind")
	}
	if _, err := os.Stat(pl.To.ConfigDir); err == nil {
		t.Error("new dir left behind")
	}
}

func TestHardenDryRun(t *testing.T) {
	h := newHardenHarness(t)
	h.e.ServiceRunner = &ServiceRunner{DryRun: true}
	pl := h.plan(rpc.HardenPlanParams{})
	if !pl.DryRun {
		t.Fatal("plan not marked dry run")
	}
	res, err := h.e.HardenApply(context.Background(), rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true})
	if err != nil || !res.OK || !res.DryRun {
		t.Fatalf("%+v %v", res, err)
	}
	if len(h.lf.verbs()) != 0 {
		t.Errorf("ran %q", h.lf.verbs())
	}
	skipped := 0
	for _, s := range res.Steps {
		if s.Status == "skipped" {
			skipped++
		}
	}
	if skipped < 4 {
		t.Errorf("steps: %+v", res.Steps)
	}
	if _, err := os.Stat(pl.To.ConfigDir); err == nil {
		t.Error("dry run left the new dir")
	}
	if st := mustSettings(t, h.e); st.Hardened != nil {
		t.Error("dry run changed settings")
	}
	if pls, _ := h.e.HardenPlans(pl.PlanID); pls[0].State != rpc.PlanReady {
		t.Errorf("state %s", pls[0].State)
	}
}

func TestHardenRefusals(t *testing.T) {
	h := newHardenHarness(t)
	ctx := context.Background()
	// The new plist exists.
	np := h.lf.DefinitionPath(store.DefaultMihomoLabel)
	os.WriteFile(np, []byte("x"), 0o644)
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{}); err == nil {
		t.Error("existing new plist accepted")
	}
	os.Remove(np)
	// The new dir is not empty.
	nd := filepath.Join(h.state, "mihomo")
	os.MkdirAll(nd, 0o700)
	os.WriteFile(filepath.Join(nd, "x"), nil, 0o600)
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{}); err == nil {
		t.Error("non-empty new dir accepted")
	}
	os.RemoveAll(nd)
	// The old service does not run.
	h.lf.loaded[oldLabel] = false
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{}); err == nil {
		t.Error("stopped service accepted")
	}
	h.lf.loaded[oldLabel] = true
	// Not a release version.
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{KernelVersion: "alpha-1234567"}); err == nil {
		t.Error("alpha accepted")
	}
	// Settings changed between plan and apply.
	pl := h.plan(rpc.HardenPlanParams{})
	h.e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.ConfigFile = filepath.Join(h.confDir, "other.yaml")
		return nil
	})
	if _, err := h.e.HardenApply(ctx, rpc.HardenApplyParams{PlanID: pl.PlanID, ConfirmHash: pl.ConfirmHash, SkipHealth: true}); err == nil {
		t.Error("changed settings accepted")
	}
	// Not adopted.
	h.e.Store.UpdateSettings(func(st *store.Settings) error { st.Mihomo.ServiceAdopted = false; return nil })
	if _, err := h.e.HardenPlan(ctx, rpc.HardenPlanParams{}); err == nil {
		t.Error("non-adopted accepted")
	}
}

func TestHardenRecoverStartsOld(t *testing.T) {
	h := newHardenHarness(t)
	pl := h.plan(rpc.HardenPlanParams{})
	dir, _ := h.e.Store.HardenDir(pl.PlanID)
	writeHardenState(dir, &hardenState{State: hardenCutting, ConfirmHash: pl.ConfirmHash})
	h.lf.loaded[oldLabel] = false
	h.ctrl.SetDown(true)
	h.e.RecoverHarden(context.Background())
	if got := h.lf.verbs(); !slices.Equal(got, []string{"bootstrap system " + h.lf.DefinitionPath(oldLabel), "kickstart system/" + oldLabel}) {
		t.Errorf("launchctl %q", got)
	}
	pls, _ := h.e.HardenPlans(pl.PlanID)
	if pls[0].State != rpc.PlanFailed {
		t.Errorf("state %s", pls[0].State)
	}
}

func TestRelocatePaths(t *testing.T) {
	in := []byte("external-ui: /old/conf/ui\nproxy-providers:\n  a: {type: http, url: 'https://x', path: /old/conf/p/a.yaml}\n  b: {type: file, path: /elsewhere/b.yaml}\nrule-providers:\n  r:\n    type: http\n    path: ./rules/r.yaml\n")
	out, ch, err := relocatePaths(in, "/old/conf/")
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) != 2 || !bytes.Contains(out, []byte("external-ui: ui")) || !bytes.Contains(out, []byte("path: p/a.yaml")) || !bytes.Contains(out, []byte("/elsewhere/b.yaml")) {
		t.Fatalf("%q\n%s", ch, out)
	}
	if got := strings.Join(relocatedValues(out), ","); got != "ui,p/a.yaml,rules/r.yaml" {
		t.Errorf("values %s", got)
	}
	same, ch, _ := relocatePaths([]byte("external-ui: /old/conf\n"), "/old/conf")
	if ch != nil || !bytes.Equal(same, []byte("external-ui: /old/conf\n")) {
		t.Errorf("the dir itself: %q", ch)
	}
}

func mustSettings(t *testing.T, e *Engine) *store.Settings {
	t.Helper()
	st, err := e.Store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	return st
}
