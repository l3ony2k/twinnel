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

	"github.com/l3ony2k/duet/core/config"
	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/core/takeover"
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

	applyMu   sync.Mutex
	adoptMu   sync.Mutex
	adopting  atomic.Bool // suppresses auto-apply while an adopt runs
	serviceMu sync.Mutex
	mu        sync.Mutex
	lastApply *rpc.ApplyResult
	// goodSecret is the controller secret the running mihomo last
	// accepted (in memory only; see secretFallback).
	goodSecret *string
	tailnet    rpc.TailnetStatus
	started    time.Time
	health     healthState
}

// New returns an engine with defaults for everything optional.
func New(st *store.Store, secrets store.Secrets) *Engine {
	return &Engine{
		Store:   st,
		Secrets: secrets,
		Kernels: &kernel.Manager{Dir: st.Path("kernels", "mihomo")},
		TSProbe: &kernel.TailscaleProbe{},
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
	return &http.Client{Transport: tr, Timeout: 2 * time.Minute}
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
	if b, err := os.ReadFile(st.Mihomo.ConfigFile); err == nil {
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
// network changes, health schedule)
// and blocks until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); e.subscriptionLoop(ctx) }()
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
	out.Tailnet.Coexist = st.Tailscale.Coexist
	if out.Tailnet.Peers == nil {
		if info, ok := e.cachedTailnet(); ok {
			out.Tailnet.Peers, out.Tailnet.Suffix, out.Tailnet.Interface = info.PeerShortNames, info.MagicDNSSuffix, info.Interface
		}
	}
	out.Kernel, _ = e.KernelList()
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
	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("tailscale status: %v: %s", err, ee.Stderr)
		}
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	return out, nil
}
