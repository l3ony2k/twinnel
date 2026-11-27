//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/platform/darwin"
	"github.com/l3ony2k/twinnel/platform/darwin/keychain"
	"github.com/l3ony2k/twinnel/platform/darwin/launchd"
	"github.com/l3ony2k/twinnel/platform/darwin/takeoverprobe"
)

const secureSecrets = "keychain" // --secrets auto when running as root

func newKeychain(path string) (store.Secrets, error) {
	k := &keychain.Keychain{Path: path}
	if path == "" { // the installed helper: read items from before the rename too
		k.LegacyService, k.LegacyAccount = legacy.KeychainService, legacy.KeychainAccount
	}
	return k, nil
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
	if d := systemDNS(o); d != nil {
		e.SystemDNS = d
	}
	if o.noHealth {
		return func(context.Context) {}
	}
	nw := darwin.NewNetWatcher()
	e.NetChanges = nw.C()
	sched := &health.Scheduler{
		Runner:   &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
		NewEnv:   func() health.Env { return darwin.New(healthOptions(e, o)) },
		Interval: o.healthInterval,
	}
	e.SetHealth(sched)
	e.EgressProbe = egressProbe(func() health.Env {
		opt := healthOptions(e, o)
		opt.Settings.Egress = true
		return darwin.New(opt)
	})
	return func(ctx context.Context) {
		if err := nw.Watch(ctx); err != nil && ctx.Err() == nil {
			e.Log.Warn("network watcher stopped", "err", err)
		}
	}
}

// healthOptions points the checks at the mihomo the helper manages, read
// fresh from settings for every run.
func healthOptions(e *engine.Engine, opts *options) darwin.Options {
	// The checks run the tailscale CLI, which Homebrew installs owned by
	// the user: never as root.
	o := darwin.Options{Run: takeoverprobe.SafeRunner(8 * time.Second), TailscaleStatus: devTailnetStatus(e, opts)}
	settings := health.DefaultSettings()
	o.Settings = &settings
	st, err := e.Store.Settings()
	if err != nil {
		return o
	}
	settings.Egress = st.HealthEgress // 定期检测
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

// systemDNS is dns.*'s platform part: networksetup, with -setdnsservers
// only when the helper runs as root (a development helper reads only).
func systemDNS(o *options) *darwin.SystemDNS {
	if o.noSystemDNS {
		return nil
	}
	read := takeoverprobe.SafeRunner(10 * time.Second)
	return &darwin.SystemDNS{Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "-setdnsservers" {
			if os.Geteuid() != 0 {
				return nil, errors.New("setting the system DNS needs the installed (root) helper")
			}
			return service.ExecExecutor{Timeout: 10 * time.Second}.Run(ctx, append([]string{name}, args...))
		}
		return read(ctx, name, args...)
	}}
}
