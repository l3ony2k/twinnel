//go:build darwin

package main

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/platform/darwin"
	"github.com/l3ony2k/duet/platform/darwin/keychain"
	"github.com/l3ony2k/duet/platform/darwin/launchd"
	"github.com/l3ony2k/duet/platform/darwin/takeoverprobe"
)

const hasKeychain = true

func newKeychain(path string) (store.Secrets, error) {
	return &keychain.Keychain{Path: path}, nil
}

func platformServices() service.Manager { return launchd.New() }

// tailnetInterface is the interface of `route -n get 100.100.100.100`
// (read-only).
func tailnetInterface(ctx context.Context) (string, error) {
	r, err := darwin.New(darwin.Options{}).RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}

// setupPlatform attaches the darwin pieces: the Tailscale interface lookup,
// the network watcher and the health scheduler over darwin.Env. The
// returned function runs the watcher until ctx ends.
func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	switch o.tailnetIface {
	case "", "auto":
		e.TailnetInterface = tailnetInterface
	case "none":
	default:
		name := o.tailnetIface
		e.TailnetInterface = func(context.Context) (string, error) { return name, nil }
	}
	setupTakeover(e, o)
	if o.noHealth {
		return func(context.Context) {}
	}
	nw := darwin.NewNetWatcher()
	e.NetChanges = nw.C()
	sched := &health.Scheduler{
		Runner:   &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
		NewEnv:   func() health.Env { return darwin.New(healthOptions(e)) },
		Interval: o.healthInterval,
	}
	e.SetHealth(sched)
	return func(ctx context.Context) {
		if err := nw.Watch(ctx); err != nil && ctx.Err() == nil {
			e.Log.Warn("network watcher stopped", "err", err)
		}
	}
}

// healthOptions points the checks at the mihomo the helper manages, read
// fresh from settings for every run.
func healthOptions(e *engine.Engine) darwin.Options {
	// The checks run the tailscale CLI, which Homebrew installs owned by
	// the user: never as root.
	o := darwin.Options{Run: takeoverprobe.SafeRunner(8 * time.Second)}
	st, err := e.Store.Settings()
	if err != nil {
		return o
	}
	o.Controller = st.Mihomo.Controller
	o.ConfigPath = st.Mihomo.ConfigFile
	o.TailscaleBin = st.Tailscale.Binary
	if e.Secrets != nil {
		if s, err := e.Secrets.Get(store.SecretController); err == nil {
			o.Secret = s
		}
	}
	return o
}

// setupTakeover attaches the read-only takeover probe.
func setupTakeover(e *engine.Engine, o *options) {
	home := o.takeoverHome
	if home == "" {
		home = consoleUserHome()
	}
	p := takeoverprobe.New(home)
	if o.takeoverServices != "" {
		p.ServiceDirs = []string{o.takeoverServices}
	}
	if st, err := e.Store.Settings(); err == nil {
		p.TailscaleBin = st.Tailscale.Binary
	}
	if o.tailnetStatus != "" && e.Tailnet != nil {
		p.TailnetStatus = e.Tailnet.Status
	}
	if e.TailnetInterface != nil {
		p.TailnetIface = e.TailnetInterface
	}
	e.TakeoverProbe = p
}

// consoleUserHome is the home of the user logged in at the console (the
// owner of /dev/console), else the helper's own home.
func consoleUserHome() string {
	if fi, err := os.Stat("/dev/console"); err == nil {
		if sys, ok := fi.Sys().(*syscall.Stat_t); ok && sys.Uid != 0 {
			if u, err := user.LookupId(strconv.Itoa(int(sys.Uid))); err == nil {
				return u.HomeDir
			}
		}
	}
	home, _ := os.UserHomeDir()
	return home
}
