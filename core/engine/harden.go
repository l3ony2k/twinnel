package engine

// service.harden: move an adopted mihomo (a root LaunchDaemon that runs a
// user-owned binary with a user-writable -d dir, security review finding 5)
// to the managed kernel, a root-owned config dir under the state dir and a
// duet LaunchDaemon, in one restart.
//
// Everything that does not interrupt the network happens first (kernel
// install, copying the config dir, mihomo -t, writing the new plist). Then
// the cut: bootout the old label, bootstrap the new one, wait for /version
// and compare health. Any failure in the cut boots the new label out and
// the old one back in. On success the old label is disabled (its plist and
// the old binary and dir stay on disk for a manual rollback) and settings
// switch to the managed paths.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/config"
	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

// Files in a harden plan dir.
const (
	hardenPlanFile     = "plan.json"
	hardenInternalFile = "plan.internal.json"
	hardenStateFile    = "plan.state.json"
	hardenOldPlist     = "old-service.plist"
	hardenFailedDir    = "failed-config-dir"
)

// Harden plan state while the old service is booted out and the result is
// not known yet. A helper that starts and finds it recovers (RecoverHarden).
const hardenCutting = "cutting"

// Limits of the copy from the old config dir.
const (
	hardenMaxFiles = 20000
	hardenMaxBytes = 1 << 30
)

// hardenInternal is the part of a plan the client does not see.
type hardenInternal struct {
	FromUID   *int   `json:"from_uid,omitempty"`
	NewSecret bool   `json:"new_secret,omitempty"`
	Composed  string `json:"composed_sha256"` // composed config without secret
	// Controller the new mihomo serves (from its config).
	Controller string `json:"controller"`
}

type hardenState struct {
	State       string            `json:"state"`
	ConfirmHash string            `json:"confirm_hash"`
	CutAt       time.Time         `json:"cut_at,omitzero"`
	LastResult  *rpc.HardenResult `json:"last_result,omitempty"`
}

func (e *Engine) hardenTimeouts() (stop, start, poll time.Duration) {
	stop, start, poll = e.HardenStopTimeout, e.HardenStartTimeout, e.HardenPoll
	if stop <= 0 {
		stop = 15 * time.Second
	}
	if start <= 0 {
		start = 45 * time.Second
	}
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	return
}

// hardenTarget is the settings after the move.
func (e *Engine) hardenTarget(st *store.Settings, planID, kernelVersion string) *store.Settings {
	ns := *st
	ns.Mihomo.Binary = ""
	ns.Mihomo.ConfigDir = e.Store.Path("mihomo")
	ns.Mihomo.ConfigFile = filepath.Join(ns.Mihomo.ConfigDir, "config.yaml")
	ns.Mihomo.ConfigDirUID = nil
	ns.Mihomo.ServiceLabel = store.DefaultMihomoLabel
	ns.Mihomo.ServiceAdopted = false
	ns.Mihomo.LogDir = ""
	ns.Hardened = &store.Hardening{
		PlanID: planID, OldLabel: st.Mihomo.ServiceLabel, OldBinary: st.Mihomo.Binary,
		OldConfigDir: st.Mihomo.ConfigDir, OldConfigFile: st.Mihomo.ConfigFile,
		OldConfigUID: st.Mihomo.ConfigDirUID, KernelVersion: kernelVersion,
	}
	return &ns
}

// hardenable refuses setups harden does not apply to.
func (e *Engine) hardenable(st *store.Settings) error {
	if e.Services == nil {
		return rpc.Errorf(rpc.CodeFailed, "no service manager on this platform")
	}
	if !st.Mihomo.ServiceAdopted {
		if st.Mihomo.Binary == "" && st.Mihomo.ConfigDirUID == nil {
			return rpc.ErrInvalid("nothing to harden: mihomo already runs from the managed kernel and a root-owned config dir")
		}
		return rpc.ErrInvalid("harden moves an adopted mihomo service; %s was not adopted", st.Mihomo.ServiceLabel)
	}
	if st.Mihomo.ServiceLabel == store.DefaultMihomoLabel {
		return rpc.ErrInvalid("the adopted service already uses duet's label %s", store.DefaultMihomoLabel)
	}
	if _, ok := st.ActiveSubscription(); !ok {
		return rpc.ErrInvalid("no active subscription: nothing to compose the new config from")
	}
	return nil
}

// hardenOldService checks that the adopted service runs and returns its
// plist path.
func (e *Engine) hardenOldService(ctx context.Context, label string) (string, error) {
	s, err := e.Services.Status(ctx, label)
	if err != nil {
		return "", rpc.Errorf(rpc.CodeFailed, "status of %s: %v", label, err)
	}
	if !s.Loaded || !s.Running {
		return "", rpc.ErrInvalid("%s is not running (loaded=%v); harden moves a running service", label, s.Loaded)
	}
	if !s.Installed || s.Definition == "" {
		return "", rpc.ErrInvalid("%s has no service definition on disk to restore from", label)
	}
	return s.Definition, nil
}

// hardenNewFree checks that the new label, plist and config dir are free.
func (e *Engine) hardenNewFree(ctx context.Context, ns *store.Settings) error {
	label := ns.Mihomo.ServiceLabel
	plist := e.Services.DefinitionPath(label)
	if _, err := os.Lstat(plist); err == nil {
		return rpc.ErrInvalid("%s already exists; remove it (or `duet service uninstall mihomo` on a non-adopted setup) first", plist)
	}
	if s, err := e.Services.Status(ctx, label); err == nil && s.Loaded {
		return rpc.ErrInvalid("%s is already loaded in launchd", label)
	}
	dir := ns.Mihomo.ConfigDir
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		return rpc.ErrInvalid("%s already exists and is not empty; move it aside first", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	return nil
}

// chooseSecret is the controller secret the restarted mihomo starts with.
// fresh is a newly generated secret (NewSecret), else "".
func (e *Engine) chooseSecret(fresh, layerSecret string, current []byte) string {
	if fresh != "" {
		return fresh
	}
	if s := e.controllerSecret(); s != "" {
		return s
	}
	if layerSecret != "" {
		return layerSecret
	}
	s, _ := topLevel(current, "secret")
	return s
}

// hardenConfig composes the config for the new dir, sets the secret and
// validates it with bin, copying geo files from geoDir.
func (e *Engine) hardenConfig(ctx context.Context, ns *store.Settings, bin, geoDir, fresh string, current []byte) (final, composed []byte, err error) {
	comp, layerSecret, err := e.compose(ctx, ns)
	if err != nil {
		return nil, nil, err
	}
	final, err = withSecret(comp.Config, e.chooseSecret(fresh, layerSecret, current))
	if err != nil {
		return nil, nil, err
	}
	opts := []config.ValidateOption{config.WithSafePaths(ns.Mihomo.ConfigDir)}
	if geoDir != "" {
		opts = append(opts, config.WithGeoDir(geoDir))
	}
	v, err := e.validate(ctx, bin, final, opts...)
	if err != nil {
		return nil, nil, err
	}
	errs := v.Errors
	if _, xerrs := exposureCheck(final, ns); len(xerrs) > 0 {
		errs = append(errs, xerrs...)
	}
	if !v.OK || len(errs) > 0 {
		return nil, nil, fmt.Errorf("the new config does not pass: %s", strings.Join(errs, "; "))
	}
	return final, comp.Config, nil
}

// copyItems lists what is copied from the old config dir: the geo
// databases, cache.db (selected nodes, fake-ip pool) and the dir-relative
// paths of the new config (external-ui, provider files). Missing ones are
// left out.
func copyItems(oldDir string, final []byte) []string {
	items := []string{"cache.db"}
	for _, g := range kernel.GeoItems(oldDir, readGeox(final)) {
		items = append(items, g.Rel)
	}
	items = append(items, relocatedValues(final)...)
	var out []string
	for _, it := range items {
		it = filepath.Clean(it)
		if !filepath.IsLocal(it) || slices.Contains(out, it) {
			continue
		}
		// A dir that holds an item already listed covers it.
		out = append(out, it)
	}
	slices.Sort(out)
	var pruned []string
	for _, it := range out {
		if len(pruned) > 0 && inside(pruned[len(pruned)-1], it) {
			continue
		}
		pruned = append(pruned, it)
	}
	return pruned
}

func readGeox(cfg []byte) map[string]string {
	root, err := config.Parse(cfg)
	if err != nil {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "geox-url" {
			m := map[string]string{}
			v := root.Content[i+1]
			for j := 0; j+1 < len(v.Content); j += 2 {
				m[strings.ToLower(v.Content[j].Value)] = v.Content[j+1].Value
			}
			return m
		}
	}
	return nil
}

// copyTree copies items from src into dst (both opened as os.Root, so no
// symlink can lead either side out of its dir). Only regular files and
// dirs are copied; anything else, symlinks included, is reported as
// skipped. With dst nil it only measures.
func copyTree(src, dst *os.Root, items []string) (files []rpc.HardenFile, total int64, skipped []string, err error) {
	count := 0
	var walk func(rel string) (int64, error)
	walk = func(rel string) (int64, error) {
		fi, err := src.Lstat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		count++
		if count > hardenMaxFiles {
			return 0, fmt.Errorf("more than %d files to copy", hardenMaxFiles)
		}
		switch {
		case fi.Mode().IsRegular():
			if total+fi.Size() > hardenMaxBytes {
				return 0, fmt.Errorf("more than %d MiB to copy", hardenMaxBytes>>20)
			}
			if dst != nil {
				if err := copyRootFile(src, dst, rel, fi); err != nil {
					return 0, err
				}
			}
			total += fi.Size()
			return fi.Size(), nil
		case fi.IsDir():
			if dst != nil {
				if err := dst.Mkdir(rel, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
					return 0, err
				}
			}
			d, err := src.Open(rel)
			if err != nil {
				return 0, err
			}
			ents, err := d.ReadDir(-1)
			d.Close()
			if err != nil {
				return 0, err
			}
			var n int64
			for _, en := range ents {
				m, err := walk(filepath.Join(rel, en.Name()))
				if err != nil {
					return 0, err
				}
				n += m
			}
			return n, nil
		}
		skipped = append(skipped, rel+" ("+fi.Mode().Type().String()+")")
		return 0, nil
	}
	for _, it := range items {
		fi, lerr := src.Lstat(it)
		if lerr != nil {
			continue
		}
		if dst != nil {
			if d := filepath.Dir(it); d != "." {
				if err := dst.MkdirAll(d, 0o755); err != nil {
					return nil, 0, nil, err
				}
			}
		}
		n, err := walk(it)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("%s: %w", it, err)
		}
		if fi.Mode().IsRegular() || fi.IsDir() {
			files = append(files, rpc.HardenFile{Path: filepath.ToSlash(it), Size: n, Dir: fi.IsDir()})
		}
	}
	return files, total, skipped, nil
}

func copyRootFile(src, dst *os.Root, rel string, fi fs.FileInfo) error {
	in, err := src.Open(rel)
	if err != nil {
		return err
	}
	defer in.Close()
	// Re-check after open: the entry must still be the regular file that
	// was listed (os.Root already refuses a symlink out of the dir).
	if ofi, err := in.Stat(); err != nil || !ofi.Mode().IsRegular() {
		return fmt.Errorf("%s changed while copying", rel)
	}
	mode := fi.Mode().Perm() &^ 0o022
	out, err := dst.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, fi.Size()+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// hardenDowntime estimates the time without mihomo. Nothing is measured on
// the live system: mihomo stops in well under a second (TUN and DNS
// listener closed), and a start loads the rules and geo databases and
// brings up the TUN, the routes and the DNS listener, which takes one to a
// few seconds, more with large rule sets.
func hardenDowntime(final []byte) rpc.HardenDowntime {
	rules := 0
	if root, err := config.Parse(final); err == nil {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "rules" {
				rules = len(root.Content[i+1].Content)
			}
		}
	}
	d := rpc.HardenDowntime{MinSeconds: 2, MaxSeconds: 8}
	if rules > 20000 {
		d.MaxSeconds += rules / 20000
	}
	d.Note = fmt.Sprintf("estimate, not measured: the old mihomo stops (TUN, routes and the DNS listener go away), the new one loads %d rules and the geo databases and sets up the TUN again. New connections fail and running ones are cut during that time. If the new one does not come up, the rollback takes about as long again (up to the start timeout first).", rules)
	return d
}

func ownerText(uid *int) string {
	if uid == nil {
		return "root"
	}
	return "uid " + strconv.Itoa(*uid)
}

func stepTexts(phase string, steps []service.Step, down bool) []rpc.HardenStep {
	var out []rpc.HardenStep
	for _, s := range steps {
		out = append(out, rpc.HardenStep{Phase: phase, Text: s.String(), Network: down})
	}
	return out
}

func filterSteps(steps []service.Step, kinds ...service.StepKind) []service.Step {
	var out []service.Step
	for _, s := range steps {
		if slices.Contains(kinds, s.Kind) {
			out = append(out, s)
		}
	}
	return out
}

// hardenSpec is the new service.
func (e *Engine) hardenSpec(ns *store.Settings) service.Spec {
	return service.MihomoSpec(ns.Mihomo.ServiceLabel, e.Kernels.Binary(), ns.Mihomo.ConfigDir, ns.Mihomo.ConfigFile, e.Store.Path("logs"))
}

// hardenSteps renders the whole sequence (for the plan, and run by apply).
type hardenSteps struct {
	prepare  []service.Step // plist write, enable new
	stopOld  []service.Step
	startNew []service.Step
	disable  []service.Step // old label, after success
	stopNew  []service.Step // rollback
	startOld []service.Step // rollback
}

func (e *Engine) planSteps(st, ns *store.Settings) (*hardenSteps, []byte, error) {
	spec := e.hardenSpec(ns)
	plist, err := e.Services.Render(spec)
	if err != nil {
		return nil, nil, err
	}
	var hs hardenSteps
	inst, err := e.Services.Plan(service.OpInstall, spec)
	if err != nil {
		return nil, nil, err
	}
	hs.prepare = filterSteps(inst, service.StepMkdir, service.StepWrite)
	en, err := e.Services.Plan(service.OpEnable, spec)
	if err != nil {
		return nil, nil, err
	}
	for i := range en {
		en[i].IgnoreError = true // fails harmlessly when it was never disabled
	}
	hs.prepare = append(hs.prepare, en...)
	old := service.Spec{Label: st.Mihomo.ServiceLabel}
	if hs.stopOld, err = e.Services.Plan(service.OpStop, old); err != nil {
		return nil, nil, err
	}
	if hs.startNew, err = e.Services.Plan(service.OpStart, spec); err != nil {
		return nil, nil, err
	}
	if hs.disable, err = e.Services.Plan(service.OpDisable, old); err != nil {
		return nil, nil, err
	}
	if hs.stopNew, err = e.Services.Plan(service.OpStop, service.Spec{Label: spec.Label}); err != nil {
		return nil, nil, err
	}
	for i := range hs.stopNew {
		hs.stopNew[i].IgnoreError = true
	}
	if hs.startOld, err = e.Services.Plan(service.OpStart, old); err != nil {
		return nil, nil, err
	}
	return &hs, plist, nil
}

// HardenPlan inspects the adopted setup (read-only apart from the plan
// dir) and stores a plan under <state>/harden/<plan_id>/.
func (e *Engine) HardenPlan(ctx context.Context, p rpc.HardenPlanParams) (rpc.HardenPlan, error) {
	if !e.adoptMu.TryLock() {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeBusy, "a takeover or harden operation is running")
	}
	defer e.adoptMu.Unlock()
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.HardenPlan{}, err
	}
	if err := e.hardenable(st); err != nil {
		return rpc.HardenPlan{}, err
	}
	if p.NewSecret && e.Secrets == nil {
		return rpc.HardenPlan{}, rpc.ErrInvalid("no secret store for a new controller secret")
	}
	oldPlist, err := e.hardenOldService(ctx, st.Mihomo.ServiceLabel)
	if err != nil {
		return rpc.HardenPlan{}, err
	}
	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	running, err := e.currentController(st).Version(vctx)
	cancel()
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "the running mihomo does not answer on %s: %v", st.Mihomo.Controller, err)
	}
	e.mu.Lock()
	runningSecret := ""
	if e.goodSecret != nil {
		runningSecret = *e.goodSecret
	}
	e.mu.Unlock()

	plan := rpc.HardenPlan{CreatedAt: e.now().UTC(), DryRun: e.hardenDryRun()}
	ver := p.KernelVersion
	if ver == "" {
		ver = running
	}
	if !kernel.ValidVersion(ver) || !strings.HasPrefix(ver, "v") {
		return rpc.HardenPlan{}, rpc.ErrInvalid("kernel version %q is not a stable release (vX.Y.Z); pass kernel_version", ver)
	}
	if ver != running {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("the kernel changes from %s (running) to %s", running, ver))
	}
	plan.Kernel = rpc.HardenKernel{Version: ver, Installed: e.Kernels.Has(ver), Binary: e.Kernels.Binary()}
	if !plan.Kernel.Installed {
		r, err := e.kernelManager(ctx).CheckVersion(ctx, ver)
		if err != nil {
			return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "look up mihomo %s: %v", ver, err)
		}
		plan.Kernel.Asset, plan.Kernel.Size = r.Asset, r.Size
	}

	stamp := e.now().UTC().Format("20060102-150405")
	id := "harden-" + stamp
	ns := e.hardenTarget(st, id, ver)
	if err := e.hardenNewFree(ctx, ns); err != nil {
		return rpc.HardenPlan{}, err
	}
	current, err := readConfined(st.Mihomo.ConfigFile)
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "read the running config: %v", err)
	}

	// Validate with the binary that runs now when the new kernel is not
	// installed yet (apply validates again with the new one).
	bin := st.Mihomo.Binary
	if plan.Kernel.Installed {
		bin = filepath.Join(e.Kernels.Dir, ver, "mihomo")
	}
	fresh := ""
	if p.NewSecret {
		if fresh, err = newSecret(); err != nil {
			return rpc.HardenPlan{}, err
		}
	}
	final, composed, err := e.hardenConfig(ctx, ns, bin, st.Mihomo.ConfigDir, fresh, current)
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	in := hardenInternal{FromUID: st.Mihomo.ConfigDirUID, NewSecret: p.NewSecret, Composed: store.Hash(composed), Controller: st.Mihomo.Controller}
	if a, ok := topLevel(final, "external-controller"); ok && strings.TrimSpace(a) != "" {
		in.Controller = controllerAddr(a)
	}
	if in.Controller != st.Mihomo.Controller {
		plan.ConfigChanges = append(plan.ConfigChanges, fmt.Sprintf("controller moves from %s to %s", st.Mihomo.Controller, in.Controller))
	}

	// What changes in the config, besides the dir it lives in.
	if comp, _, err := e.compose(ctx, st); err == nil {
		if _, ch, err := relocatePaths(comp.Config, st.Mihomo.ConfigDir); err == nil {
			plan.ConfigChanges = append(plan.ConfigChanges, ch...)
		}
	}
	if stripped, _, err := withoutSecret(current); err == nil {
		if rel, _, err := relocatePaths(stripped, st.Mihomo.ConfigDir); err == nil && !sameYAML(rel, composed) {
			plan.ConfigChanges = append(plan.ConfigChanges, "the composed config differs from the running file in more than paths (subscription, layers or tailnet changed since the last apply; see `duet compose --preview`)")
		}
	}
	newSecretVal, _ := topLevel(final, "secret")
	switch {
	case p.NewSecret:
		plan.Secret = "new"
		plan.ConfigChanges = append(plan.ConfigChanges, "a new controller secret (clients of the controller need it afterwards)")
	case newSecretVal == "":
		plan.Secret = "none"
		plan.Warnings = append(plan.Warnings, "the controller has no secret. After the move the helper manages the config and its next apply generates one, which mihomo only loads at its next restart; plan with new_secret to set one in this restart")
	case newSecretVal != runningSecret:
		plan.Secret = "activate"
		plan.ConfigChanges = append(plan.ConfigChanges, "the stored controller secret (written by controller.secure, not yet loaded) becomes active")
	default:
		plan.Secret = "keep"
	}

	src, err := os.OpenRoot(st.Mihomo.ConfigDir)
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	plan.Copy, plan.CopyBytes, plan.Skipped, err = copyTree(src, nil, copyItems(st.Mihomo.ConfigDir, final))
	src.Close()
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "config dir: %v", err)
	}
	if plan.Copy == nil {
		plan.Copy = []rpc.HardenFile{}
	}

	hs, plist, err := e.planSteps(st, ns)
	if err != nil {
		return rpc.HardenPlan{}, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	plan.Plist = string(plist)
	plan.From = rpc.HardenService{
		Label: st.Mihomo.ServiceLabel, Plist: oldPlist, Binary: st.Mihomo.Binary, Version: running,
		ConfigDir: st.Mihomo.ConfigDir, ConfigFile: st.Mihomo.ConfigFile, Owner: ownerText(st.Mihomo.ConfigDirUID),
	}
	plan.To = rpc.HardenService{
		Label: ns.Mihomo.ServiceLabel, Plist: e.Services.DefinitionPath(ns.Mihomo.ServiceLabel), Binary: e.Kernels.Binary(),
		Version: ver, ConfigDir: ns.Mihomo.ConfigDir, ConfigFile: ns.Mihomo.ConfigFile, Owner: "root",
	}
	plan.Downtime = hardenDowntime(final)
	stopT, startT, _ := e.hardenTimeouts()

	add := func(phase, text string, down bool) {
		plan.Steps = append(plan.Steps, rpc.HardenStep{Phase: phase, Text: text, Network: down})
	}
	if plan.Kernel.Installed {
		add("prepare", fmt.Sprintf("use the installed kernel %s; point %s at it", ver, filepath.Join(e.Kernels.Dir, "current")), false)
	} else {
		add("prepare", fmt.Sprintf("install mihomo %s (%s, %.1f MB from github.com/MetaCubeX/mihomo, sha256 checked) into %s and point current at it", ver, plan.Kernel.Asset, float64(plan.Kernel.Size)/1e6, e.Kernels.Dir), false)
	}
	if p.NewSecret {
		add("prepare", "generate a controller secret and keep it in the helper's secret store", false)
	}
	add("prepare", fmt.Sprintf("copy %d items (%.1f MB) from %s into a root-owned staging dir, write the composed config.yaml (0600) and run mihomo -t with the new kernel on it", len(plan.Copy), float64(plan.CopyBytes)/1e6, st.Mihomo.ConfigDir), false)
	add("prepare", "rename the staging dir to "+ns.Mihomo.ConfigDir, false)
	add("prepare", fmt.Sprintf("keep a copy of %s in the plan dir", oldPlist), false)
	plan.Steps = append(plan.Steps, stepTexts("prepare", hs.prepare, false)...)
	add("prepare", "health checks (before)", false)
	plan.Steps = append(plan.Steps, stepTexts("cut", hs.stopOld, true)...)
	add("cut", fmt.Sprintf("wait until the old controller stops answering (at most %s)", stopT), true)
	plan.Steps = append(plan.Steps, stepTexts("cut", hs.startNew, true)...)
	add("verify", fmt.Sprintf("wait for GET /version on %s (at most %s)", in.Controller, startT), true)
	add("verify", "health checks (after), compared with before; worse → rollback", false)
	plan.Steps = append(plan.Steps, stepTexts("finish", hs.disable, false)...)
	add("finish", fmt.Sprintf("%s and %s stay on disk, untouched (manual rollback: docs/runbook-adopt-macos.md step 8)", oldPlist, st.Mihomo.ConfigDir), false)
	add("finish", fmt.Sprintf("settings: binary → managed kernel, config → %s, service → %s", ns.Mihomo.ConfigFile, ns.Mihomo.ServiceLabel), false)
	plan.Steps = append(plan.Steps, stepTexts("rollback", hs.stopNew, true)...)
	add("rollback", "rm "+e.Services.DefinitionPath(ns.Mihomo.ServiceLabel), true)
	plan.Steps = append(plan.Steps, stepTexts("rollback", hs.startOld, true)...)
	add("rollback", fmt.Sprintf("wait for GET /version from the old mihomo (at most %s); restore the previous secret; move the new config dir into the plan dir", startT), true)

	if plan.DryRun {
		plan.Warnings = append(plan.Warnings, "this helper does not run as root: apply prepares and validates, then only reports the launchctl steps")
	}
	plan.Warnings = append(plan.Warnings, fmt.Sprintf("mihomo restarts: the network is down for about %d–%d s; apply when no agent or download is running", plan.Downtime.MinSeconds, plan.Downtime.MaxSeconds))

	var dir string
	for i := 1; ; i++ {
		plan.PlanID = id
		if i > 1 {
			plan.PlanID = fmt.Sprintf("%s-%d", id, i)
		}
		dir, err = e.Store.CreateHardenDir(plan.PlanID)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i > 20 {
			return rpc.HardenPlan{}, err
		}
	}
	if plan.PlanID != id {
		ns.Hardened.PlanID = plan.PlanID
	}
	fail := func(err error) (rpc.HardenPlan, error) {
		os.RemoveAll(dir)
		return rpc.HardenPlan{}, err
	}
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := store.WriteFileAtomic(filepath.Join(dir, hardenPlanFile), append(b, '\n'), 0o600); err != nil {
		return fail(err)
	}
	ib, _ := json.MarshalIndent(in, "", "  ")
	if err := store.WriteFileAtomic(filepath.Join(dir, hardenInternalFile), append(ib, '\n'), 0o600); err != nil {
		return fail(err)
	}
	plan.ConfirmHash = sha256Hex(append(b, '\n'))[:16]
	plan.State = rpc.PlanReady
	if err := writeHardenState(dir, &hardenState{State: rpc.PlanReady, ConfirmHash: plan.ConfirmHash}); err != nil {
		return fail(err)
	}
	e.log().Info("harden plan", "id", plan.PlanID, "kernel", ver, "copy_bytes", plan.CopyBytes)
	return plan, nil
}

func (e *Engine) hardenDryRun() bool {
	r := e.ServiceRunner
	return r == nil || r.DryRun || r.Exec == nil || r.Files == nil
}

func writeHardenState(dir string, hs *hardenState) error {
	b, err := json.MarshalIndent(hs, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(filepath.Join(dir, hardenStateFile), append(b, '\n'), 0o600)
}

func readJSONFile(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// loadHarden reads a plan dir.
func (e *Engine) loadHarden(id string) (string, *rpc.HardenPlan, *hardenInternal, *hardenState, error) {
	if !store.ValidID(id) {
		return "", nil, nil, nil, rpc.ErrInvalid("invalid plan_id %q", id)
	}
	dir, _ := e.Store.HardenDir(id)
	var hs hardenState
	if err := readJSONFile(filepath.Join(dir, hardenStateFile), &hs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, nil, nil, rpc.ErrNotFound("harden plan", id)
		}
		return "", nil, nil, nil, rpc.Errorf(rpc.CodeFailed, "plan %s: %v", id, err)
	}
	var plan rpc.HardenPlan
	var in hardenInternal
	if err := readJSONFile(filepath.Join(dir, hardenPlanFile), &plan); err != nil {
		return "", nil, nil, nil, rpc.Errorf(rpc.CodeFailed, "plan %s: %v", id, err)
	}
	if err := readJSONFile(filepath.Join(dir, hardenInternalFile), &in); err != nil {
		return "", nil, nil, nil, rpc.Errorf(rpc.CodeFailed, "plan %s: %v", id, err)
	}
	return dir, &plan, &in, &hs, nil
}

// waitController polls /version until it answers (up=true) or fails
// (up=false), or the timeout passes.
func (e *Engine) waitController(ctx context.Context, ctrl Controller, up bool, timeout, poll time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		vctx, cancel := context.WithTimeout(ctx, min(2*time.Second, timeout))
		v, err := ctrl.Version(vctx)
		cancel()
		if (err == nil) == up {
			return v, nil
		}
		if time.Now().After(deadline) {
			if up {
				return "", fmt.Errorf("no answer after %s: %v", timeout, err)
			}
			return v, fmt.Errorf("still answering after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}

// HardenApply carries out a plan. See the file comment for the sequence.
// A refusal before anything changed is an RPC error; later failures return
// a result with OK false and the steps taken.
func (e *Engine) HardenApply(ctx context.Context, p rpc.HardenApplyParams) (rpc.HardenResult, error) {
	res := rpc.HardenResult{PlanID: p.PlanID, Steps: []rpc.AdoptStep{}}
	step := func(name, status, detail string) {
		res.Steps = append(res.Steps, rpc.AdoptStep{Name: name, Status: status, Detail: detail})
		e.log().Info("harden", "plan", p.PlanID, "step", name, "status", status, "detail", detail)
	}
	if !store.ValidID(p.PlanID) {
		return res, rpc.ErrInvalid("invalid plan_id %q", p.PlanID)
	}
	if !e.adoptMu.TryLock() {
		return res, rpc.Errorf(rpc.CodeBusy, "a takeover or harden operation is running")
	}
	defer e.adoptMu.Unlock()
	dir, plan, in, hst, err := e.loadHarden(p.PlanID)
	if err != nil {
		return res, err
	}
	if hst.State != rpc.PlanReady {
		return res, rpc.ErrInvalid("plan %s is %s; make a new plan", p.PlanID, hst.State)
	}
	b, err := os.ReadFile(filepath.Join(dir, hardenPlanFile))
	if err != nil || p.ConfirmHash == "" || p.ConfirmHash != hst.ConfirmHash || sha256Hex(b)[:16] != hst.ConfirmHash {
		return res, rpc.ErrInvalid("confirm_hash does not match plan %s; show the plan again and confirm", p.PlanID)
	}
	if e.healthSched() == nil && !p.SkipHealth {
		return res, rpc.ErrInvalid("health checks are not available in this helper, so a bad start could not be detected beyond /version; pass skip_health to apply anyway")
	}

	// Writes to the config must wait, and the auto-apply loops stay quiet,
	// until settings point at the new dir (or the old service runs again).
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	e.adopting.Store(true)
	defer e.adopting.Store(false)

	st, err := e.Store.Settings()
	if err != nil {
		return res, err
	}
	if err := e.hardenable(st); err != nil {
		return res, err
	}
	if st.Mihomo.ServiceLabel != plan.From.Label || st.Mihomo.Binary != plan.From.Binary || st.Mihomo.ConfigDir != plan.From.ConfigDir || st.Mihomo.ConfigFile != plan.From.ConfigFile || ownerText(st.Mihomo.ConfigDirUID) != plan.From.Owner {
		return res, rpc.ErrInvalid("settings changed since plan %s was made; re-plan", p.PlanID)
	}
	oldPlist, err := e.hardenOldService(ctx, st.Mihomo.ServiceLabel)
	if err != nil {
		return res, err
	}
	if oldPlist != plan.From.Plist {
		return res, rpc.ErrInvalid("%s now loads from %s, the plan was made for %s; re-plan", st.Mihomo.ServiceLabel, oldPlist, plan.From.Plist)
	}
	ver := plan.Kernel.Version
	ns := e.hardenTarget(st, p.PlanID, ver)
	if err := e.hardenNewFree(ctx, ns); err != nil {
		return res, err
	}
	oldCtrl := e.currentController(st)
	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = oldCtrl.Version(vctx)
	cancel()
	if err != nil {
		return res, rpc.Errorf(rpc.CodeFailed, "the running mihomo does not answer: %v", err)
	}
	e.mu.Lock()
	oldSecret := ""
	if e.goodSecret != nil {
		oldSecret = *e.goodSecret
	}
	e.mu.Unlock()
	// The controller the old mihomo answers on, with the secret it uses.
	oldFixed := e.controller(st.Mihomo.Controller, oldSecret)
	dry := e.hardenDryRun()
	res.DryRun = dry
	stopT, startT, poll := e.hardenTimeouts()
	step("verify", "ok", fmt.Sprintf("%s runs from %s; plan %s confirmed", plan.From.Label, oldPlist, p.PlanID))

	finish := func(state string) (rpc.HardenResult, error) {
		r := res
		hst.State, hst.LastResult = state, &r
		if err := writeHardenState(dir, hst); err != nil {
			e.log().Warn("harden: plan state", "err", err)
		}
		e.publish(rpc.EventServiceChanged, map[string]any{"name": "mihomo", "op": "harden", "plan_id": p.PlanID, "state": state, "dry_run": dry})
		return res, nil
	}
	// Undo of the prepare phase (nothing outside the state dir and the
	// new plist was touched yet).
	var undo []func()
	unprepare := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	failPrepare := func(name string, err error) (rpc.HardenResult, error) {
		step(name, "failed", err.Error())
		unprepare()
		res.Error = name + ": " + err.Error()
		return finish(rpc.PlanFailed)
	}

	// ---- prepare: kernel ----
	if err := e.hardenKernel(ctx, ver); err != nil {
		return failPrepare("kernel", err)
	}
	bin := e.Kernels.Binary()
	if geteuid() == 0 {
		if err := kernel.RequireTrusted(bin); err != nil {
			return failPrepare("kernel", err)
		}
	}
	if got, err := kernel.ProbeVersion(ctx, bin); err != nil {
		return failPrepare("kernel", err)
	} else if got != ver {
		return failPrepare("kernel", fmt.Errorf("%s reports %s, expected %s", bin, got, ver))
	}
	step("kernel", "ok", bin+" → "+ver)

	// ---- prepare: secret ----
	fresh := ""
	if in.NewSecret {
		prev := e.controllerSecret()
		if fresh, err = newSecret(); err == nil {
			err = e.Secrets.Set(store.SecretController, fresh)
		}
		if err != nil {
			return failPrepare("secret", err)
		}
		undo = append(undo, func() {
			var err error
			if prev != "" {
				err = e.Secrets.Set(store.SecretController, prev)
			} else {
				err = e.Secrets.Delete(store.SecretController)
			}
			if err != nil {
				step("restore secret", "failed", err.Error())
			}
		})
		step("secret", "ok", "new controller secret stored")
	}

	// ---- prepare: config dir ----
	current, err := readConfined(st.Mihomo.ConfigFile)
	if err != nil {
		return failPrepare("config", err)
	}
	final, composed, err := e.hardenConfig(ctx, ns, bin, st.Mihomo.ConfigDir, fresh, current)
	if err != nil {
		return failPrepare("config", err)
	}
	if store.Hash(composed) != in.Composed {
		res.Warnings = append(res.Warnings, "the composed config changed since the plan (subscription, layers or tailnet peers)")
	}
	staging := e.Store.Path("mihomo.staging-" + p.PlanID)
	os.RemoveAll(staging)
	undo = append(undo, func() { os.RemoveAll(staging) })
	files, total, skipped, err := e.stageConfigDir(st.Mihomo.ConfigDir, staging, final)
	if err != nil {
		return failPrepare("copy", err)
	}
	step("copy", "ok", fmt.Sprintf("%d items, %d bytes into %s", len(files), total, staging))
	if len(skipped) > 0 {
		res.Warnings = append(res.Warnings, "not copied (not a regular file or dir): "+strings.Join(skipped, ", "))
	}
	v, err := e.validate(ctx, bin, final, config.WithSafePaths(ns.Mihomo.ConfigDir), config.WithGeoDir(staging))
	if err == nil && !v.OK {
		err = fmt.Errorf("mihomo -t: %s", strings.Join(v.Errors, "; "))
	}
	if err != nil {
		return failPrepare("validate", err)
	}
	step("validate", "ok", "mihomo -t with "+ver+" passed")
	os.Remove(ns.Mihomo.ConfigDir) // an empty leftover (hardenNewFree checked)
	if err := os.Rename(staging, ns.Mihomo.ConfigDir); err != nil {
		return failPrepare("config dir", err)
	}
	undo = append(undo, func() { os.RemoveAll(ns.Mihomo.ConfigDir) })
	step("config dir", "ok", ns.Mihomo.ConfigDir)

	if ob, err := os.ReadFile(oldPlist); err != nil {
		return failPrepare("backup plist", err)
	} else if err := store.WriteFileAtomic(filepath.Join(dir, hardenOldPlist), ob, 0o600); err != nil {
		return failPrepare("backup plist", err)
	}
	step("backup plist", "ok", filepath.Join(dir, hardenOldPlist))

	hs, _, err := e.planSteps(st, ns)
	if err != nil {
		return failPrepare("steps", err)
	}
	newPlist := e.Services.DefinitionPath(ns.Mihomo.ServiceLabel)

	if dry {
		for _, group := range []struct {
			name  string
			steps []service.Step
		}{{"write plist", hs.prepare}, {"stop old", hs.stopOld}, {"start new", hs.startNew}, {"disable old", hs.disable}} {
			for _, s := range group.steps {
				step(group.name, "skipped", "dry run: "+s.String())
			}
		}
		unprepare()
		step("dry run", "ok", "prepared and validated; the config dir and kernel were set up and the dir removed again; nothing was stopped or started")
		res.OK = true
		return res, nil
	}

	r := e.ServiceRunner
	run := func(steps []service.Step) error {
		e.serviceMu.Lock()
		defer e.serviceMu.Unlock()
		return service.Apply(ctx, steps, r.Exec, r.Files)
	}
	if err := run(hs.prepare); err != nil {
		r.Files.Remove(newPlist)
		return failPrepare("write plist", err)
	}
	undo = append(undo, func() { r.Files.Remove(newPlist) })
	step("write plist", "ok", newPlist)

	var before map[string]health.Status
	if !p.SkipHealth {
		var checks []rpc.AdoptCheck
		checks, before = e.adoptHealth(ctx)
		res.HealthBefore = checks
		step("health before", "ok", checkLine(checks))
	}

	// ---- cut ----
	hst.State, hst.CutAt = hardenCutting, e.now().UTC()
	if err := writeHardenState(dir, hst); err != nil {
		return failPrepare("plan state", err)
	}
	cutStart := time.Now()
	newSecretVal, _ := topLevel(final, "secret")
	newCtrl := e.controller(in.Controller, newSecretVal)

	rollback := func(why string) (rpc.HardenResult, error) {
		step("rollback", "warn", why)
		if err := run(hs.stopNew); err != nil {
			step("stop new", "failed", err.Error())
		}
		if err := r.Files.Remove(newPlist); err != nil {
			step("remove new plist", "failed", err.Error())
		}
		rbErr := run(hs.startOld)
		if rbErr != nil {
			step("start old", "failed", rbErr.Error())
		}
		_, werr := e.waitController(ctx, oldFixed, true, startT, poll)
		res.DowntimeMS = time.Since(cutStart).Milliseconds()
		// The new dir goes into the plan dir for inspection.
		if err := os.Rename(ns.Mihomo.ConfigDir, filepath.Join(dir, hardenFailedDir)); err != nil {
			os.RemoveAll(ns.Mihomo.ConfigDir)
		}
		unprepare() // secret, plist, leftovers
		if werr != nil {
			step("old answers", "failed", werr.Error())
			res.Error = fmt.Sprintf("%s; the rollback did not bring the old mihomo back (%v). Run: sudo launchctl bootstrap system %s", why, werr, oldPlist)
			return finish(rpc.PlanFailed)
		}
		step("old answers", "ok", fmt.Sprintf("old mihomo back after %d ms", res.DowntimeMS))
		res.RolledBack = true
		res.Error = why + "; rolled back to " + plan.From.Label
		if !p.SkipHealth {
			res.HealthAfter, _ = e.adoptHealth(ctx)
		}
		e.triggerHealth("harden rolled back")
		return finish(rpc.PlanRolledBack)
	}

	if err := run(hs.stopOld); err != nil {
		// bootout failed: if the old one still answers, nothing changed.
		vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, verr := oldFixed.Version(vctx)
		cancel()
		if verr == nil {
			step("stop old", "failed", err.Error())
			unprepare()
			res.Error = "stop old: " + err.Error() + " (the old mihomo still runs; nothing changed)"
			return finish(rpc.PlanFailed)
		}
		return rollback("stop old: " + err.Error())
	}
	step("stop old", "ok", "launchctl bootout system/"+plan.From.Label)
	if _, err := e.waitController(ctx, oldFixed, false, stopT, poll); err != nil {
		return rollback("the old controller " + err.Error())
	}
	if err := run(hs.startNew); err != nil {
		return rollback("start new: " + err.Error())
	}
	step("start new", "ok", "launchctl bootstrap system "+newPlist)
	got, err := e.waitController(ctx, newCtrl, true, startT, poll)
	if err != nil {
		return rollback("the new mihomo did not answer on " + in.Controller + ": " + err.Error())
	}
	res.DowntimeMS = time.Since(cutStart).Milliseconds()
	step("new answers", "ok", fmt.Sprintf("%s after %d ms", got, res.DowntimeMS))
	if got != ver {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the new controller reports %s, expected %s", got, ver))
	}
	if !p.SkipHealth {
		settle := e.AdoptSettle
		if settle == 0 {
			settle = 3 * time.Second
		}
		var after map[string]health.Status
		var worse []string
		for try := 0; try < 2; try++ {
			if settle > 0 {
				select {
				case <-time.After(settle):
				case <-ctx.Done():
				}
			}
			res.HealthAfter, after = e.adoptHealth(ctx)
			if worse = worseChecks(before, after); len(worse) == 0 {
				break
			}
		}
		res.Worse = worse
		if len(worse) > 0 {
			return rollback("health got worse: " + strings.Join(worse, ", "))
		}
		step("health after", "ok", checkLine(res.HealthAfter))
	}

	// ---- finish ----
	if err := run(hs.disable); err != nil {
		step("disable old", "failed", err.Error())
		res.Warnings = append(res.Warnings, fmt.Sprintf("could not disable %s (%v): it would load again at boot next to %s. Run: sudo launchctl disable system/%s", plan.From.Label, err, ns.Mihomo.ServiceLabel, plan.From.Label))
	} else {
		step("disable old", "ok", "launchctl disable system/"+plan.From.Label+" (plist kept)")
	}
	now := e.now().UTC()
	_, err = e.Store.UpdateSettings(func(cur *store.Settings) error {
		cur.Mihomo.Binary = ""
		cur.Mihomo.ConfigDir, cur.Mihomo.ConfigFile, cur.Mihomo.ConfigDirUID = ns.Mihomo.ConfigDir, ns.Mihomo.ConfigFile, nil
		cur.Mihomo.ServiceLabel, cur.Mihomo.ServiceAdopted, cur.Mihomo.LogDir = ns.Mihomo.ServiceLabel, false, ""
		cur.Mihomo.Controller = in.Controller
		h := *ns.Hardened
		h.At, h.OldPlist = now, oldPlist
		cur.Hardened = &h
		return nil
	})
	if err != nil {
		step("settings", "failed", err.Error())
		res.Error = "mihomo runs from the new service, but settings.json could not be updated: " + err.Error() + ". Fix it by hand (see the runbook) before the next apply"
		return finish(rpc.PlanFailed)
	}
	step("settings", "ok", "settings point at "+ns.Mihomo.ConfigFile)
	if _, err := e.Store.AddHistory(store.HistoryApplied, "harden "+p.PlanID, composed, e.now(), st.HistoryKeep); err != nil {
		e.log().Warn("history", "err", err)
	}
	res.OK = true
	e.triggerHealth("hardened")
	return finish(rpc.PlanApplied)
}

// hardenKernel makes version the current managed kernel, installing it if
// needed.
func (e *Engine) hardenKernel(ctx context.Context, version string) error {
	if !kernelMu.TryLock() {
		return errors.New("a kernel install is running")
	}
	defer kernelMu.Unlock()
	if e.Kernels.Has(version) {
		return e.Kernels.Switch(version)
	}
	m := e.kernelManager(ctx)
	r, err := m.CheckVersion(ctx, version)
	if err != nil {
		return err
	}
	if _, err := m.Install(ctx, r); err != nil {
		return err
	}
	if l, err := e.KernelList(); err == nil {
		e.publish(rpc.EventKernelChanged, l)
	}
	return nil
}

// stageConfigDir creates staging (root-owned 0700), copies the items from
// the old dir and writes the config.
func (e *Engine) stageConfigDir(oldDir, staging string, final []byte) ([]rpc.HardenFile, int64, []string, error) {
	if err := os.Mkdir(staging, 0o700); err != nil {
		return nil, 0, nil, err
	}
	src, err := os.OpenRoot(oldDir)
	if err != nil {
		return nil, 0, nil, err
	}
	defer src.Close()
	dst, err := os.OpenRoot(staging)
	if err != nil {
		return nil, 0, nil, err
	}
	defer dst.Close()
	files, total, skipped, err := copyTree(src, dst, copyItems(oldDir, final))
	if err != nil {
		return nil, 0, nil, err
	}
	f, err := dst.OpenFile("config.yaml", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, 0, nil, err
	}
	if _, err := f.Write(final); err != nil {
		f.Close()
		return nil, 0, nil, err
	}
	if err := f.Close(); err != nil {
		return nil, 0, nil, err
	}
	return files, total, skipped, nil
}

// HardenPlans lists stored harden plans, newest first, or one of them.
func (e *Engine) HardenPlans(id string) ([]rpc.HardenPlan, error) {
	ids := []string{id}
	if id == "" {
		var err error
		if ids, err = e.Store.HardenIDs(); err != nil {
			return nil, err
		}
		slices.Reverse(ids)
	}
	out := []rpc.HardenPlan{}
	for _, i := range ids {
		_, plan, _, hs, err := e.loadHarden(i)
		if err != nil {
			if id != "" {
				return nil, err
			}
			continue
		}
		plan.State, plan.ConfirmHash = hs.State, hs.ConfirmHash
		out = append(out, *plan)
	}
	return out, nil
}

// RecoverHarden runs at helper start. A plan left in the cutting state
// means the helper stopped between booting out the old service and the
// result: if neither mihomo service runs, the old one is booted back in.
func (e *Engine) RecoverHarden(ctx context.Context) {
	ids, err := e.Store.HardenIDs()
	if err != nil || e.Services == nil {
		return
	}
	for _, id := range ids {
		dir, plan, _, hs, err := e.loadHarden(id)
		if err != nil || hs.State != hardenCutting {
			continue
		}
		oldS, _ := e.Services.Status(ctx, plan.From.Label)
		newS, _ := e.Services.Status(ctx, plan.To.Label)
		msg := "the helper stopped during the cut"
		switch {
		case newS.Running:
			msg += "; the new service runs, but settings were not switched: check it and follow the runbook (step 8)"
		case oldS.Running:
			msg += "; the old service runs again"
		case e.hardenDryRun():
			msg += "; neither service runs (dry run: not restarting anything)"
		default:
			steps, err := e.Services.Plan(service.OpStart, service.Spec{Label: plan.From.Label})
			if err == nil {
				err = service.Apply(ctx, steps, e.ServiceRunner.Exec, e.ServiceRunner.Files)
			}
			if err != nil {
				msg += "; neither service ran and starting the old one failed: " + err.Error()
			} else {
				msg += "; neither service ran, the old one was started again"
			}
		}
		e.log().Warn("harden recovery", "plan", id, "detail", msg)
		hs.State = rpc.PlanFailed
		if hs.LastResult == nil {
			hs.LastResult = &rpc.HardenResult{PlanID: id}
		}
		hs.LastResult.Error = msg
		writeHardenState(dir, hs)
	}
}

// sameYAML compares two configs after a parse and encode round trip
// (formatting and comments aside).
func sameYAML(a, b []byte) bool {
	norm := func(x []byte) []byte {
		root, err := config.Parse(x)
		if err != nil {
			return x
		}
		out, err := config.Encode(root)
		if err != nil {
			return x
		}
		return out
	}
	return string(norm(a)) == string(norm(b))
}
