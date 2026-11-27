// Package setup is the "set up this Mac" flow: from a machine with nothing
// installed to mihomo running Twinnel's composed config (TUN, fake-ip,
// the Tailscale coexist and leak-guard layers), the system DNS pointing at
// it, and tailscaled installed and logged in. The app (新电脑设置) and
// `twinnel setup` drive the same Flow; each step is idempotent, detects
// when it is already done, and can be retried on its own.
//
// Steps, in order (the subscription comes before any GitHub download: it
// is usually reachable directly, and its proxy can then carry the
// downloads that are not):
//
//	helper        install the root helper (one administrator prompt)
//	subscription  paste a URL, or import an export zip (subscription +
//	              overrides; Env.ImportConfig, the config.import hook)
//	mihomo        download the official kernel (sha256-verified, root-owned
//	              <state>/kernels), then the geo databases
//	config        the built-in base settings (TUN, fake-ip, DNS on
//	              127.0.0.1:53, controller on loopback), the Tailscale
//	              coexist and leak-guard layers; validated with mihomo -t
//	apply         write the config (a controller secret is generated), install
//	              and start the mihomo LaunchDaemon, wait until it answers
//	dns           point every active network service's DNS at 127.0.0.1
//	              (the old values are kept and restored on uninstall)
//	tailscale     install the open-source tailscaled (kept when one exists)
//	login         interactive login: open the URL, wait until Running
//	health        run the health checks
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/overrides"
)

// Step identifies one step.
type Step string

const (
	StepHelper       Step = "helper"
	StepSubscription Step = "subscription"
	StepMihomo       Step = "mihomo"
	StepConfig       Step = "config"
	StepApply        Step = "apply"
	StepDNS          Step = "dns"
	StepTailscale    Step = "tailscale"
	StepLogin        Step = "login"
	StepHealth       Step = "health"
)

// Order is the steps in the order they run.
var Order = []Step{StepHelper, StepSubscription, StepMihomo, StepConfig, StepApply, StepDNS, StepTailscale, StepLogin, StepHealth}

// State is a step's state.
type State string

const (
	Pending State = "pending"
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
	Skipped State = "skipped"
	// Waiting: the step needs the user (a subscription to paste, the
	// login page to finish).
	Waiting State = "waiting"
)

// Failure codes a client turns into a choice.
const (
	// CodeDownload: a download failed (GitHub, pkgs.tailscale.com,
	// Homebrew). Offer another download proxy (Options.Proxy).
	CodeDownload = "download"
	// CodeNeedsSubscription: no URL and no import yet.
	CodeNeedsSubscription = "needs-subscription"
	// CodeInvalidConfig: mihomo -t rejected the composed config.
	CodeInvalidConfig = "invalid-config"
	// CodeNotRoot: the helper is a development helper (not root); the
	// step's system changes were only planned.
	CodeNotRoot = "not-root"
	// CodeHealthFail: checks failed (Detail names them).
	CodeHealthFail = "health-fail"
	// CodeUnsupported: the platform lacks this step (marked Skipped).
	CodeUnsupported = "unsupported"
)

// StepState is a step's progress.
type StepState struct {
	ID     Step   `json:"id"`
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"` // a version, a count, the tailnet: one short fact
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
	// URL is the login URL while StepLogin waits.
	URL string `json:"url,omitempty"`
	// Warnings are non-fatal notes (a geo download that failed, a kept
	// Tailscale app that uses the system VPN slot).
	Warnings []Warning `json:"warnings,omitempty"`
}

// Warning is a non-fatal note: Code for a client to word, Text in English.
type Warning struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Warning codes.
const (
	WarnGeo       = "geo"        // the geo databases did not download
	WarnSystemVPN = "system-vpn" // the kept Tailscale is the app (system VPN slot)
)

// Options are the user's choices.
type Options struct {
	// SubscriptionURL is a pasted subscription URL. Never logged.
	SubscriptionURL  string
	SubscriptionName string
	// ImportZip is an export zip (停止使用 → 导出配置) to restore instead of,
	// or along with, SubscriptionURL, through Env.ImportConfig. Never
	// logged: it holds node credentials and may hold subscription URLs.
	ImportZip []byte
	// Proxy for the GitHub and Tailscale downloads: rpc.ProxyAuto,
	// ProxyDirect, ProxySubscription or a proxy URL.
	Proxy         string
	KernelChannel string
	NoDNS         bool // leave the system DNS alone (mihomo's TUN still hijacks DNS)
	NoTailscale   bool // no Tailscale install or login
	// LoginTimeout bounds the wait for the browser login (default 10 min).
	LoginTimeout time.Duration
}

// LoginState is what the tailnet login step reads.
type LoginState struct {
	Backend string // ipn backend state: Running, NeedsLogin, Stopped, NoState...
	AuthURL string
	Tailnet string
}

// Tailnet is the local Tailscale (LocalAPI) for the login step.
type Tailnet interface {
	LoginState(ctx context.Context) (LoginState, error)
	// StartLogin starts an interactive login and connects (WantRunning).
	StartLogin(ctx context.Context) error
}

// Env is how the flow reaches the machine.
type Env struct {
	// Connect dials the helper; an error means it is not installed or not
	// running.
	Connect func(ctx context.Context) (Caller, error)
	// InstallHelper installs it (`twinnel helper install`; the app runs
	// the CLI, which asks for the administrator password). nil: cannot.
	InstallHelper func(ctx context.Context) error
	Tailnet       Tailnet
	// OpenURL opens the login page (the app: the browser; the CLI: prints
	// it and tries `open`).
	OpenURL func(url string) error
	// Sleep waits between polls; replaceable in tests.
	Sleep func(ctx context.Context, d time.Duration) error
	// Lang words the health report.
	Lang string
	// ImportConfig restores Options.ImportZip. HOOK: the helper's
	// config.import RPC is built on feat/import-config; until it lands
	// this is nil and an import fails with ErrNoImport. It reports whether
	// the zip brought a subscription that is now active and fetched.
	ImportConfig func(ctx context.Context, hc Caller, zip []byte) (ImportResult, error)
}

// Caller is the part of a helper connection the flow uses.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// ImportResult is what Env.ImportConfig restored.
type ImportResult struct {
	// Subscription: an active subscription with content came with the
	// zip, so no URL is needed.
	Subscription bool
	// LeakGuardGroups from the zip, if any (used by the config step).
	LeakGuardGroups []string
}

// ErrNoImport: this build has no config.import yet.
var ErrNoImport = errors.New("importing a backup is not available in this version yet")

// Flow runs the steps. Use it from one goroutine at a time (Run, RunStep);
// Steps may be read from any.
type Flow struct {
	Env  Env
	Opts Options
	// OnChange is called after every state change (from the running
	// goroutine).
	OnChange func()

	mu       sync.Mutex
	steps    []StepState
	hc       Caller
	imported *ImportResult
}

// New returns a flow with every step pending.
func New(env Env, o Options) *Flow {
	f := &Flow{Env: env, Opts: o}
	for _, id := range Order {
		f.steps = append(f.steps, StepState{ID: id, State: Pending})
	}
	return f
}

// Steps returns a copy of the steps' states.
func (f *Flow) Steps() []StepState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]StepState, len(f.steps))
	for i, s := range f.steps {
		s.Warnings = append([]Warning(nil), s.Warnings...)
		out[i] = s
	}
	return out
}

// Step returns one step's state.
func (f *Flow) Step(id Step) StepState {
	for _, s := range f.Steps() {
		if s.ID == id {
			return s
		}
	}
	return StepState{ID: id}
}

func (f *Flow) set(id Step, fn func(*StepState)) {
	f.mu.Lock()
	for i := range f.steps {
		if f.steps[i].ID == id {
			fn(&f.steps[i])
		}
	}
	f.mu.Unlock()
	if f.OnChange != nil {
		f.OnChange()
	}
}

// Complete reports whether every step is done or skipped.
func (f *Flow) Complete() bool {
	for _, s := range f.Steps() {
		if s.State != Done && s.State != Skipped {
			return false
		}
	}
	return true
}

func (f *Flow) sleep(ctx context.Context, d time.Duration) error {
	if f.Env.Sleep != nil {
		return f.Env.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (f *Flow) helper(ctx context.Context) (Caller, error) {
	f.mu.Lock()
	hc := f.hc
	f.mu.Unlock()
	if hc != nil {
		return hc, nil
	}
	if f.Env.Connect == nil {
		return nil, errors.New("no helper connection")
	}
	hc, err := f.Env.Connect(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.hc = hc
	f.mu.Unlock()
	return hc, nil
}

// Detect marks the steps that are already done (a re-run, or a machine
// set up halfway), without changing anything.
func (f *Flow) Detect(ctx context.Context) {
	hc, err := f.helper(ctx)
	if err != nil {
		return
	}
	f.set(StepHelper, func(s *StepState) { s.State = Done })
	for _, id := range Order[1:] {
		if id == StepHealth {
			continue
		}
		if done, detail := f.detect(ctx, hc, id); done {
			f.set(id, func(s *StepState) { s.State, s.Detail, s.Error, s.Code = Done, detail, "", "" })
		}
	}
	if f.Opts.NoDNS {
		f.skip(StepDNS, "")
	}
	if f.Opts.NoTailscale {
		f.skip(StepTailscale, "")
		f.skip(StepLogin, "")
	}
}

func (f *Flow) skip(id Step, code string) {
	f.set(id, func(s *StepState) { s.State, s.Code, s.Error = Skipped, code, "" })
}

func (f *Flow) detect(ctx context.Context, hc Caller, id Step) (bool, string) {
	switch id {
	case StepSubscription:
		var subs []rpc.Subscription
		if hc.Call(ctx, rpc.MethodSubList, nil, &subs) == nil {
			for _, s := range subs {
				if s.Active && s.Hash != "" {
					return true, s.Name
				}
			}
		}
	case StepMihomo:
		var k rpc.KernelList
		if hc.Call(ctx, rpc.MethodKernelList, nil, &k) == nil && (k.Current != "" || k.External) {
			return true, k.Current
		}
	case StepConfig:
		var st rpc.Settings
		var layers []rpc.Layer
		if hc.Call(ctx, rpc.MethodSettingsGet, nil, &st) == nil && hc.Call(ctx, rpc.MethodLayerList, nil, &layers) == nil &&
			st.TailscaleCoexist && len(st.LeakGuardGroups) > 0 && hasBaseLayer(layers) {
			return true, ""
		}
	case StepApply:
		var st rpc.Status
		if hc.Call(ctx, rpc.MethodStatus, nil, &st) == nil && st.Mihomo.Reachable && st.LastApply != nil && st.LastApply.Error == "" {
			return true, st.Mihomo.Version
		}
	case StepDNS:
		var d rpc.DNSStatus
		if hc.Call(ctx, rpc.MethodDNSStatus, nil, &d) == nil {
			if !d.Supported {
				return false, ""
			}
			return d.Pointed, ""
		}
	case StepTailscale:
		var ti rpc.TailscaleInfo
		if hc.Call(ctx, rpc.MethodTailscaleInfo, nil, &ti) == nil && ti.Installed {
			return true, ti.Version
		}
	case StepLogin:
		if f.Env.Tailnet != nil {
			if ls, err := f.Env.Tailnet.LoginState(ctx); err == nil && ls.Backend == "Running" {
				return true, ls.Tailnet
			}
		}
	}
	return false, ""
}

func hasBaseLayer(layers []rpc.Layer) bool {
	for _, l := range layers {
		if !l.Managed && l.Stage == store.StageBase && l.Enabled {
			return true
		}
	}
	return false
}

// Run runs every step that is not done, in order, and stops at the first
// that fails or waits for the user.
func (f *Flow) Run(ctx context.Context) error {
	for _, id := range Order {
		s := f.Step(id)
		if s.State == Done || s.State == Skipped {
			continue
		}
		if err := f.RunStep(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// StepError is a failed (or waiting) step.
type StepError struct {
	Step Step
	Code string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("%s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

// RunStep runs one step (a retry runs it again).
func (f *Flow) RunStep(ctx context.Context, id Step) error {
	f.set(id, func(s *StepState) { s.State, s.Error, s.Code, s.URL, s.Warnings = Running, "", "", "", nil })
	detail, err := f.run(ctx, id)
	if err != nil {
		var se *StepError
		if !errors.As(err, &se) {
			se = &StepError{Step: id, Err: err}
		}
		se.Step = id
		state := Failed
		switch se.Code {
		case CodeNeedsSubscription:
			state = Waiting
		case CodeUnsupported:
			state = Skipped
		}
		f.set(id, func(s *StepState) { s.State, s.Error, s.Code, s.Detail = state, cleanErr(se.Err), se.Code, detail })
		if state == Skipped {
			return nil
		}
		return se
	}
	f.set(id, func(s *StepState) { s.State, s.Detail, s.URL = Done, detail, "" })
	return nil
}

// cleanErr is an error for the screen: the helper's code prefix and
// "rpc error" noise removed.
func cleanErr(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}

func fail(code string, format string, args ...any) error {
	return &StepError{Code: code, Err: fmt.Errorf(format, args...)}
}

func (f *Flow) warn(id Step, code, text string) {
	f.set(id, func(s *StepState) { s.Warnings = append(s.Warnings, Warning{Code: code, Text: text}) })
}

func (f *Flow) run(ctx context.Context, id Step) (string, error) {
	if id == StepHelper {
		return f.runHelper(ctx)
	}
	hc, err := f.helper(ctx)
	if err != nil {
		return "", fmt.Errorf("the helper is not connected: %w", err)
	}
	switch id {
	case StepSubscription:
		return f.runSubscription(ctx, hc)
	case StepMihomo:
		return f.runMihomo(ctx, hc)
	case StepConfig:
		return f.runConfig(ctx, hc)
	case StepApply:
		return f.runApply(ctx, hc)
	case StepDNS:
		return f.runDNS(ctx, hc)
	case StepTailscale:
		return f.runTailscale(ctx, hc)
	case StepLogin:
		return f.runLogin(ctx)
	case StepHealth:
		return f.runHealth(ctx, hc)
	}
	return "", fmt.Errorf("unknown step %q", id)
}

func (f *Flow) runHelper(ctx context.Context) (string, error) {
	if _, err := f.helper(ctx); err == nil {
		return "", nil
	}
	if f.Env.InstallHelper == nil {
		return "", errors.New("the helper is not installed")
	}
	if err := f.Env.InstallHelper(ctx); err != nil {
		return "", err
	}
	// launchd starts it; give it a moment to listen.
	var last error
	for range 40 {
		if _, last = f.helper(ctx); last == nil {
			return "", nil
		}
		if err := f.sleep(ctx, 500*time.Millisecond); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("the helper was installed but does not answer: %w", last)
}

// SubscriptionID is the id the flow gives a pasted subscription.
const SubscriptionID = "main"

func (f *Flow) runSubscription(ctx context.Context, hc Caller) (string, error) {
	url := strings.TrimSpace(f.Opts.SubscriptionURL)
	if url == "" && len(f.Opts.ImportZip) == 0 {
		return "", fail(CodeNeedsSubscription, "paste a subscription URL or import an export")
	}
	if len(f.Opts.ImportZip) > 0 {
		if f.Env.ImportConfig == nil {
			return "", ErrNoImport
		}
		r, err := f.Env.ImportConfig(ctx, hc, f.Opts.ImportZip)
		if err != nil {
			return "", err
		}
		f.mu.Lock()
		f.imported = &r
		f.mu.Unlock()
		if url == "" {
			if !r.Subscription {
				return "", fail(CodeNeedsSubscription, "the backup has no subscription: paste its URL")
			}
			return "", nil
		}
	}
	name := f.Opts.SubscriptionName
	if name == "" {
		name = "Subscription"
	}
	day := 1440
	if err := hc.Call(ctx, rpc.MethodSubPut, rpc.SubPutParams{ID: SubscriptionID, Name: name, URL: url, IntervalMinutes: &day}, nil); err != nil {
		return "", err
	}
	if err := hc.Call(ctx, rpc.MethodSubActivate, rpc.IDParams{ID: SubscriptionID}, nil); err != nil {
		return "", err
	}
	var r rpc.SubRefreshResult
	if err := hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: SubscriptionID}, &r); err != nil {
		return "", err
	}
	return name, nil
}

// downloadErr marks a download failure so the client offers a proxy.
func downloadErr(err error) error {
	return &StepError{Code: CodeDownload, Err: err}
}

func (f *Flow) runMihomo(ctx context.Context, hc Caller) (string, error) {
	var k rpc.KernelList
	if err := hc.Call(ctx, rpc.MethodKernelList, nil, &k); err == nil && (k.Current != "" || k.External) {
		return k.Current, nil
	}
	if err := hc.Call(ctx, rpc.MethodKernelInstall, rpc.KernelInstallParams{Channel: f.Opts.KernelChannel, Proxy: f.Opts.Proxy}, &k); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == rpc.CodeInvalidParams {
			return "", err
		}
		return "", downloadErr(err)
	}
	// The geo databases: mihomo -t and mihomo itself load them for
	// GEOIP/GEOSITE rules, and would otherwise download them at start.
	var g rpc.AccessoryUpdateResult
	if err := hc.Call(ctx, rpc.MethodGeoUpdate, rpc.AccessoryUpdateParams{}, &g); err != nil {
		f.warn(StepMihomo, WarnGeo, "geo databases: "+cleanErr(err))
	}
	return k.Current, nil
}

// BaseLayerID is the built-in base settings' override id.
const BaseLayerID = "base"

// BaseSettings is the built-in base layer (overrides/base-tun-fakeip.yaml).
func BaseSettings() ([]byte, error) {
	return overrides.Templates.ReadFile("base-tun-fakeip.yaml")
}

func (f *Flow) runConfig(ctx context.Context, hc Caller) (string, error) {
	var layers []rpc.Layer
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &layers); err != nil {
		return "", err
	}
	if !hasBaseLayer(layers) {
		base, err := BaseSettings()
		if err != nil {
			return "", err
		}
		on := true
		if err := hc.Call(ctx, rpc.MethodLayerPut, rpc.LayerPutParams{ID: BaseLayerID, Name: "Base settings", Stage: store.StageBase, Enabled: &on, Content: string(base)}, nil); err != nil {
			return "", err
		}
	}
	var st rpc.Settings
	if err := hc.Call(ctx, rpc.MethodSettingsGet, nil, &st); err != nil {
		return "", err
	}
	u := rpc.SettingsUpdate{}
	if !st.TailscaleCoexist {
		on := true
		u.TailscaleCoexist = &on
	}
	if len(st.LeakGuardGroups) == 0 {
		groups := health.DefaultSettings().SensitiveGroups
		f.mu.Lock()
		if im := f.imported; im != nil && len(im.LeakGuardGroups) > 0 {
			groups = im.LeakGuardGroups
		}
		f.mu.Unlock()
		u.LeakGuardGroups = &groups
	}
	if u.TailscaleCoexist != nil || u.LeakGuardGroups != nil {
		if err := hc.Call(ctx, rpc.MethodSettingsUpdate, u, nil); err != nil {
			return "", err
		}
	}
	var pr rpc.ComposeResult
	if err := hc.Call(ctx, rpc.MethodComposePreview, nil, &pr); err != nil {
		return "", err
	}
	if !pr.Valid {
		return "", fail(CodeInvalidConfig, "mihomo rejected the composed config: %s", strings.Join(pr.ValidateErrors, "; "))
	}
	return "", nil
}

func (f *Flow) runApply(ctx context.Context, hc Caller) (string, error) {
	var r rpc.ApplyResult
	if err := hc.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{ReasonCode: store.ReasonSetup}, &r); err != nil {
		return "", err
	}
	var st rpc.Status
	if err := hc.Call(ctx, rpc.MethodStatus, nil, &st); err == nil && st.Mihomo.Reachable {
		return st.Mihomo.Version, nil
	}
	var svc rpc.ServiceStatus
	if err := hc.Call(ctx, rpc.MethodServiceStatus, rpc.ServiceParams{Name: "mihomo"}, &svc); err != nil || !svc.Installed {
		var op rpc.ServiceOpResult
		if err := hc.Call(ctx, rpc.MethodServiceInstall, rpc.ServiceParams{Name: "mihomo"}, &op); err != nil {
			return "", err
		}
		if op.DryRun {
			return "", fail(CodeNotRoot, "the helper is not running as root: the mihomo service was only planned")
		}
	} else if !svc.Running {
		if err := hc.Call(ctx, rpc.MethodServiceStart, rpc.ServiceParams{Name: "mihomo"}, nil); err != nil {
			return "", err
		}
	}
	for range 60 {
		if err := hc.Call(ctx, rpc.MethodStatus, nil, &st); err == nil && st.Mihomo.Reachable {
			return st.Mihomo.Version, nil
		}
		if err := f.sleep(ctx, 500*time.Millisecond); err != nil {
			return "", err
		}
	}
	return "", errors.New("mihomo was started but its controller does not answer (see the helper log)")
}

func (f *Flow) runDNS(ctx context.Context, hc Caller) (string, error) {
	if f.Opts.NoDNS {
		return "", &StepError{Code: CodeUnsupported, Err: errors.New("left alone (--no-dns)")}
	}
	var d rpc.DNSStatus
	if err := hc.Call(ctx, rpc.MethodDNSStatus, nil, &d); err != nil {
		return "", err
	}
	if !d.Supported {
		return "", &StepError{Code: CodeUnsupported, Err: errors.New("setting the system DNS is not supported on this platform yet")}
	}
	if d.Pointed {
		return strings.Join(d.Target, ", "), nil
	}
	if err := hc.Call(ctx, rpc.MethodDNSPoint, nil, &d); err != nil {
		return "", err
	}
	return strings.Join(d.Target, ", "), nil
}

func (f *Flow) runTailscale(ctx context.Context, hc Caller) (string, error) {
	if f.Opts.NoTailscale {
		return "", &StepError{Code: CodeUnsupported, Err: errors.New("skipped (--no-tailscale)")}
	}
	var r rpc.TailscaleInstallResult
	if err := hc.Call(ctx, rpc.MethodTailscaleInstall, rpc.TailscaleInstallParams{Proxy: f.Opts.Proxy}, &r); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && (re.Code == rpc.CodeFailed) {
			return "", downloadErr(err)
		}
		return "", err
	}
	if r.UsesSystemVPN {
		f.warn(StepTailscale, WarnSystemVPN, "the Tailscale app uses the system VPN slot, which conflicts with mihomo's TUN; the open-source tailscaled avoids that")
	}
	if r.Service != nil && r.Service.DryRun {
		return r.Version, fail(CodeNotRoot, "the helper is not running as root: the tailscaled service was only planned")
	}
	return r.Version, nil
}

func (f *Flow) runLogin(ctx context.Context) (string, error) {
	if f.Opts.NoTailscale {
		return "", &StepError{Code: CodeUnsupported, Err: errors.New("skipped (--no-tailscale)")}
	}
	tn := f.Env.Tailnet
	if tn == nil {
		return "", errors.New("no Tailscale connection")
	}
	// tailscaled may still be starting.
	var ls LoginState
	var err error
	for range 30 {
		if ls, err = tn.LoginState(ctx); err == nil {
			break
		}
		if serr := f.sleep(ctx, time.Second); serr != nil {
			return "", serr
		}
	}
	if err != nil {
		return "", fmt.Errorf("tailscaled does not answer: %w", err)
	}
	if ls.Backend == "Running" {
		return ls.Tailnet, nil
	}
	if err := tn.StartLogin(ctx); err != nil {
		return "", err
	}
	timeout := f.Opts.LoginTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	opened := ""
	for waited := time.Duration(0); waited < timeout; waited += time.Second {
		ls, err = tn.LoginState(ctx)
		if err == nil {
			if ls.Backend == "Running" {
				return ls.Tailnet, nil
			}
			if ls.AuthURL != "" && ls.AuthURL != opened {
				opened = ls.AuthURL
				f.set(StepLogin, func(s *StepState) { s.State, s.URL = Waiting, ls.AuthURL })
				if f.Env.OpenURL != nil {
					_ = f.Env.OpenURL(ls.AuthURL)
				}
			}
		}
		if serr := f.sleep(ctx, time.Second); serr != nil {
			return "", serr
		}
	}
	return "", errors.New("the login did not finish in time; open the link again and retry")
}

func (f *Flow) runHealth(ctx context.Context, hc Caller) (string, error) {
	// The coexist layer with the tailnet's peers, now that it is up.
	var tr rpc.TailnetRefreshResult
	_ = hc.Call(ctx, rpc.MethodTailnetRefresh, nil, &tr)
	var hr rpc.HealthReport
	if err := hc.Call(ctx, rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "setup", Lang: f.Env.Lang}, &hr); err != nil {
		return "", err
	}
	var rep health.Report
	if err := json.Unmarshal(hr.Report, &rep); err != nil {
		return "", fmt.Errorf("health report: %w", err)
	}
	c := rep.Counts()
	var failed []string
	for _, ch := range rep.Checks {
		if ch.Status == health.StatusFail {
			failed = append(failed, ch.Title)
		}
	}
	detail := fmt.Sprintf("%d/%d", c[health.StatusOK], len(rep.Checks))
	if len(failed) > 0 {
		return detail, fail(CodeHealthFail, "%s", strings.Join(failed, ", "))
	}
	return detail, nil
}
