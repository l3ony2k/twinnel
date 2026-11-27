// Package engine is the helper's business logic, independent of the
// platform and of the RPC transport: compose-and-apply with rollback,
// subscriptions, the Tailscale coexist refresh, kernels and status.
// cmd/helper wires it to a store, a controller, the RPC server and the
// platform service manager.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/dnswatch"
	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/sysdns"
	"github.com/l3ony2k/twinnel/core/takeover"
)

// HelperVersion is reported by status.get.
var HelperVersion = "0.1.0-dev"

// Validator checks a config with the mihomo binary (config.Validate).
type Validator func(ctx context.Context, bin string, cfg []byte, opts ...config.ValidateOption) (*config.Validation, error)

// TailnetSource returns `tailscale status --json`.
type TailnetSource interface {
	Status(ctx context.Context) ([]byte, error)
}

// Engine holds the helper's state. Create it with New.
type Engine struct {
	Store   *store.Store
	Secrets store.Secrets
	Kernels *kernel.Manager
	TSProbe *kernel.TailscaleProbe
	Tailnet TailnetSource
	// TailnetInterface returns the name of the interface that carries
	// Tailscale's resolver 100.100.100.100 (darwin: `route -n get`). It
	// fills TailnetInfo.Interface, so the coexist layer binds the resolver
	// to it. nil leaves Interface empty.
	TailnetInterface func(ctx context.Context) (string, error)
	// Services reads service status (nil: service.status fails).
	Services service.Manager
	// ServiceRunner carries out service.start/stop/... (nil: dry run).
	ServiceRunner *ServiceRunner
	Bus           *rpc.Bus
	Log           *slog.Logger
	// NetChanges delivers network change reasons (darwin.NetWatcher). Run
	// turns each into a health trigger and a tailnet refresh.
	NetChanges <-chan string
	// RotateServiceLogs: the helper rotates mihomo's output files
	// (servicelog.go); macOS and Linux, where launchd/systemd write them.
	RotateServiceLogs bool

	// NewController builds the controller client for an address and secret
	// (default *HTTPController).
	NewController func(addr, secret string) Controller
	// Validate defaults to config.Validate.
	Validate Validator
	// HTTPClient returns the client used for subscription downloads and
	// release checks; proxy is nil for direct.
	HTTPClient func(proxy *url.URL) *http.Client
	// GitHubAPI is the API base for geo/UI digests (default
	// https://api.github.com; tests point it at a fake).
	GitHubAPI string
	// UserAgent for subscription downloads (default "clash.meta", which
	// makes providers return the mihomo format).
	UserAgent string
	// Restart ends the helper process so its service manager starts it
	// again (helper.restart). nil: helper.restart is refused.
	Restart func()
	// Socket is where the helper listens (helper.inventory lists it).
	Socket string
	// VerifyTimeout bounds the wait for /version after a reload.
	VerifyTimeout time.Duration
	Now           func() time.Time

	// TakeoverProbe reads the platform for takeover.plan/adopt (nil: not
	// supported). TakeoverDiscover overrides what discovery finds
	// (development: a copied config).
	TakeoverProbe    takeover.Probe
	TakeoverDiscover takeover.DiscoverOptions
	// AdoptSettle is the wait before the post-adopt health run (default
	// 3s; negative: none).
	AdoptSettle time.Duration
	// service.harden waits at most HardenStopTimeout (default 15s) for the
	// old mihomo to stop and HardenStartTimeout (default 45s) for the new
	// one to answer, polling every HardenPoll (default 250ms).
	HardenStopTimeout, HardenStartTimeout, HardenPoll time.Duration

	// SystemDNS reads and sets the DNS of network services for dns.*
	// (nil: not supported on this platform).
	SystemDNS sysdns.Platform
	// DNSProbe checks that a DNS server answers at addr before dns.point
	// and for the DNS watchdog (default: one UDP query).
	DNSProbe func(ctx context.Context, addr string) error
	// DNSWatchConfig tunes the DNS watchdog (core/dnswatch); DNSWatchOff
	// keeps Run from starting it (development helpers).
	DNSWatchConfig dnswatch.Config
	DNSWatchOff    bool
	// TSControl is tailscaled's LocalAPI for tailscale.login (nil: not
	// supported).
	TSControl TailscaleControl
	// TSLoginInSession: tailscale.login does not start logins, the
	// signed-in user's process does (Windows: tailscaled never gives a
	// profile to SYSTEM; see tailscale.Client.LoginInSession). Reading the
	// login state still works.
	TSLoginInSession bool
	// Autostarts reads the competing clients' autostart entries
	// (autostart.list/change, the competitor-autostart check); nil: not on
	// this platform.
	Autostarts autostart.Source
	// TSInstaller is the template of tailscale.install's installer (nil:
	// <state>/tailscale with the public sources).
	TSInstaller *kernel.TailscaleInstaller
	// TempProxy starts the temporary mihomo of the "subscription" download
	// proxy and returns its stop function (default: the real binary).
	TempProxy func(ctx context.Context, bin string, cfg []byte, port int) (stop func(), err error)

	// Sleep waits between polls (tailscale.install's service wait);
	// replaceable in tests.
	Sleep func(ctx context.Context, d time.Duration) error

	// PortOwner finds the process listening on a TCP port, for the
	// foreign-controller preflight (foreign.LsofPortOwner, /proc,
	// iphlpapi). nil: only a reload refused with SAFE_PATHS reveals another
	// program's mihomo.
	PortOwner func(ctx context.Context, port int) (*foreign.Process, error)

	// ServiceHost keeps Windows' service host (the copy of the helper that
	// runs mihomo as a service) up to date; nil elsewhere.
	ServiceHost ServiceHostKeeper

	// EgressProbe runs the egress test (health.LeakEgress with the test
	// on) once, for health.egress; nil where there is none.
	EgressProbe func(ctx context.Context) health.CheckReport

	applyMu   sync.Mutex
	adoptMu   sync.Mutex
	adopting  atomic.Bool // suppresses auto-apply while an adopt runs
	serviceMu sync.Mutex
	agentMu   sync.Mutex // agent-activity.json
	// tailnetMu makes reading, comparing and saving the tailnet cache one
	// step (tailnet poll, network-change refresh, tailnet.refresh, compose).
	tailnetMu sync.Mutex
	mu        sync.Mutex
	lastApply *rpc.ApplyResult
	// validOK are the keys (validKey) of the last passing mihomo -t runs.
	validOK []string
	// goodSecret is the controller secret the running mihomo last
	// accepted (in memory only; see secretFallback).
	goodSecret *string
	tailnet    rpc.TailnetStatus
	// owner is the last answer of the controller preflight; pendingAuto
	// the newest auto-apply held back while the controller was another
	// program's (foreign.go).
	owner       *foreign.Owner
	pendingAuto *rpc.ApplyParams
	resumed     sync.WaitGroup // a held-back auto-apply running
	started     time.Time
	health      healthState
	dnsWatch    dnsWatchState
}

// New returns an engine with defaults for everything optional.
func New(st *store.Store, secrets store.Secrets) *Engine {
	return &Engine{
		Store:   st,
		Secrets: secrets,
		Kernels: &kernel.Manager{Dir: st.Path("kernels", "mihomo")},
		TSProbe: &kernel.TailscaleProbe{Managed: st.Path("tailscale", "current", "tailscale")},
		Bus:     rpc.NewBus(0),
		Log:     slog.Default(),
		started: time.Now(),
	}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) sleep(ctx context.Context, d time.Duration) error {
	if e.Sleep != nil {
		return e.Sleep(ctx, d)
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

func (e *Engine) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

func (e *Engine) publish(typ string, data any) {
	if e.Bus != nil {
		e.Bus.Publish(typ, data)
	}
}

func (e *Engine) validate(ctx context.Context, bin string, cfg []byte, opts ...config.ValidateOption) (*config.Validation, error) {
	// A root helper never runs a mihomo binary a user can replace as root
	// (an adopted binary in a home dir): mihomo -t runs as that user.
	c, err := kernel.RunAsFor(bin)
	if err != nil {
		return nil, fmt.Errorf("mihomo binary: %w", err)
	}
	if c != nil {
		cred := *c
		opts = append(opts, config.WithRunAs(cred.UID, cred.GID, func(cmd *exec.Cmd) { kernel.DropTo(cmd, cred) }))
	}
	if e.Validate != nil {
		return e.Validate(ctx, bin, cfg, opts...)
	}
	return config.Validate(ctx, bin, cfg, opts...)
}

func (e *Engine) httpClient(proxy *url.URL) *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient(proxy)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if proxy != nil {
		tr.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: tr, Timeout: 2 * time.Minute, CheckRedirect: noDowngrade}
}

// noDowngrade follows at most 10 redirects and never from https to plain
// http: a subscription or a geo/UI download that started over TLS must not
// be handed to a network attacker by a redirect.
func noDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// controllerSecret is the secret from Secrets, or "".
func (e *Engine) controllerSecret() string {
	if e.Secrets == nil {
		return ""
	}
	s, err := e.Secrets.Get(store.SecretController)
	if err != nil {
		return ""
	}
	return s
}

func (e *Engine) controller(addr, secret string) Controller {
	if e.NewController != nil {
		return e.NewController(addr, secret)
	}
	return &HTTPController{Addr: addr, Secret: secret}
}

// currentController is the controller of the running config, as settings
// describe it. mihomo reads its secret only at start (a hot reload does
// not change it), so the secret it accepts can differ from the one in the
// file after a reload changed it. Candidates, in order: the secret in the
// config file on disk, the one the running mihomo last accepted, the stored
// one, none. A 401 moves on to the next.
func (e *Engine) currentController(st *store.Settings) Controller {
	var cands []string
	add := func(s string) {
		if !slices.Contains(cands, s) {
			cands = append(cands, s)
		}
	}
	if b, err := readConfined(st.Mihomo.ConfigFile); err == nil {
		s, _ := topLevel(b, "secret")
		add(s)
	}
	e.mu.Lock()
	if e.goodSecret != nil {
		add(*e.goodSecret)
	}
	e.mu.Unlock()
	add(e.controllerSecret())
	add("")
	return &secretFallback{e: e, addr: st.Mihomo.Controller, secrets: cands}
}

// secretFallback is a Controller that tries several secrets.
type secretFallback struct {
	e       *Engine
	addr    string
	secrets []string
	picked  int
}

func (f *secretFallback) try(fn func(Controller) error) error {
	var err error
	for i := 0; i < len(f.secrets); i++ {
		j := (f.picked + i) % len(f.secrets)
		err = fn(f.e.controller(f.addr, f.secrets[j]))
		if errors.Is(err, ErrUnauthorized) {
			continue
		}
		if err == nil {
			f.picked = j
			s := f.secrets[j]
			f.e.mu.Lock()
			f.e.goodSecret = &s
			f.e.mu.Unlock()
		}
		return err
	}
	return err
}

func (f *secretFallback) Version(ctx context.Context) (string, error) {
	var v string
	err := f.try(func(c Controller) error {
		var err error
		v, err = c.Version(ctx)
		return err
	})
	return v, err
}

func (f *secretFallback) Reload(ctx context.Context, path string) error {
	return f.try(func(c Controller) error { return c.Reload(ctx, path) })
}

func (f *secretFallback) ReloadWith(ctx context.Context, path string, force bool) error {
	return f.try(func(c Controller) error { return reloadWith(ctx, c, path, force) })
}

// mihomoBinary is the binary used for mihomo -t: the adopted external
// binary, else the managed current kernel.
func (e *Engine) mihomoBinary(st *store.Settings) (string, error) {
	if st.Mihomo.Binary != "" {
		return st.Mihomo.Binary, nil
	}
	if e.Kernels != nil {
		if _, err := os.Stat(e.Kernels.Binary()); err == nil {
			return e.Kernels.Binary(), nil
		}
	}
	return "", errors.New("no mihomo binary: install a kernel or set mihomo.binary in settings.json")
}

// Run starts the background loops (subscription schedule, tailnet poll,
// network changes, health schedule, DNS watchdog, service log rotation)
// and blocks until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.RecoverHarden(ctx)
	var wg sync.WaitGroup
	wg.Add(6)
	go func() { defer wg.Done(); e.serviceLogLoop(ctx) }()
	go func() { defer wg.Done(); e.dnsWatchLoop(ctx) }()
	go func() { defer wg.Done(); e.subscriptionLoop(ctx) }()
	go func() { defer wg.Done(); e.geoAutoLoop(ctx) }()
	go func() { defer wg.Done(); e.tailnetLoop(ctx) }()
	go func() { defer wg.Done(); e.netLoop(ctx) }()
	if s := e.healthSched(); s != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Run(ctx); err != nil && ctx.Err() == nil {
				e.log().Warn("health scheduler", "err", err)
			}
		}()
	}
	wg.Wait()
}

// lastApplyFromHistory is the newest config the helper applied, read
// from history: what status.get reports as the last apply after a helper
// restart (an upgrade), until this run applies something. Backups and
// entries from before Twinnel are not applies; nil when there is none.
func (e *Engine) lastApplyFromHistory() *rpc.ApplyResult {
	hist, err := e.Store.History()
	if err != nil {
		return nil
	}
	for _, h := range hist { // newest first
		if h.BeforeTwinnel || h.Kind != store.HistoryApplied && h.Kind != store.HistoryRollback {
			continue
		}
		la := &rpc.ApplyResult{Reason: h.Reason, ReasonCode: h.ReasonCode, ReasonArgs: h.ReasonArgs,
			Written: true, Reloaded: true, HistoryID: h.ID, Time: h.Time.UTC()}
		la.Valid, la.Changed = true, true
		return la
	}
	return nil
}

// Status assembles status.get.
func (e *Engine) Status(ctx context.Context) (rpc.Status, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.Status{}, err
	}
	out := rpc.Status{
		HelperVersion: HelperVersion, PID: os.Getpid(), UID: os.Getuid(),
		StateDir: e.Store.Dir, StartedAt: e.started.UTC(), Layers: len(st.Layers),
	}
	out.Mihomo.ConfigPath = st.Mihomo.ConfigFile
	out.Mihomo.Controller = st.Mihomo.Controller
	if bin, err := e.mihomoBinary(st); err == nil {
		out.Mihomo.Binary = bin
	}
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	v, err := e.currentController(st).Version(vctx)
	cancel()
	if err != nil {
		out.Mihomo.Error = err.Error()
	} else {
		out.Mihomo.Reachable, out.Mihomo.Version = true, v
	}
	if o := e.recentOwner(ctx); o.Verdict != foreign.Unknown {
		out.Mihomo.Owner = &o
	}
	if s, ok := st.ActiveSubscription(); ok {
		out.ActiveSubscription = s.ID
	}
	out.Subscriptions = e.subscriptionInfos(st)
	e.mu.Lock()
	if e.lastApply != nil {
		la := *e.lastApply
		out.LastApply = &la
	}
	out.Tailnet = e.tailnet
	e.mu.Unlock()
	if out.LastApply == nil {
		out.LastApply = e.lastApplyFromHistory()
	}
	out.Tailnet.Coexist = st.Tailscale.Coexist
	if out.Tailnet.Peers == nil {
		if info, ok := e.cachedTailnet(); ok {
			out.Tailnet.Peers, out.Tailnet.Suffix, out.Tailnet.Interface = info.PeerShortNames, info.MagicDNSSuffix, info.Interface
		}
	}
	out.Kernel, _ = e.KernelList()
	out.DNSWatch = e.DNSWatch()
	if e.ServiceHost != nil {
		out.ServiceHost = e.ServiceHost.State(ctx)
	}
	return out, nil
}

// ServiceStatus reports one of the managed services.
func (e *Engine) ServiceStatus(ctx context.Context, name string) (rpc.ServiceStatus, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.ServiceStatus{}, err
	}
	var label string
	switch name {
	case "mihomo":
		label = st.Mihomo.ServiceLabel
	case "tailscaled":
		label = st.Tailscale.ServiceLabel
	case "helper":
		label = st.HelperLabel
	default:
		return rpc.ServiceStatus{}, rpc.ErrInvalid("unknown service %q (mihomo, tailscaled, helper)", name)
	}
	out := rpc.ServiceStatus{Name: name, Label: label}
	if e.Services == nil {
		return out, errors.New("no service manager on this platform")
	}
	s, err := e.Services.Status(ctx, label)
	out.Installed, out.Loaded, out.Running, out.PID = s.Installed, s.Loaded, s.Running, s.PID
	out.LastExitCode, out.Runs, out.Program, out.Definition = s.LastExitCode, s.Runs, s.Program, s.Definition
	if err != nil {
		out.Error = err.Error()
	}
	return out, nil
}

// CLITailnet runs `tailscale status --json` (read-only).
type CLITailnet struct {
	Probe *kernel.TailscaleProbe
}

// Status implements TailnetSource.
func (c CLITailnet) Status(ctx context.Context) ([]byte, error) {
	p := c.Probe
	if p == nil {
		p = &kernel.TailscaleProbe{}
	}
	bin := p.Find()
	if bin == "" {
		return nil, errors.New("tailscale CLI not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := kernel.Output(ctx, bin, "status", "--json")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("tailscale status: %v: %s", err, ee.Stderr)
		}
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	return out, nil
}
