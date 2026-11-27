//go:build linux

package main

import (
	"context"
	"errors"
	"os/user"
	"time"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/platform/linux"
	"github.com/l3ony2k/twinnel/platform/linux/systemd"
)

// Secrets stay in <state-dir>/secrets.json (0600, root-owned when
// installed) on Linux; a Secret Service backend comes later.
const secureSecrets = "" // --secrets auto when running as root

func newKeychain(string) (store.Secrets, error) {
	return nil, errors.New("--secrets keychain is only available on macOS; Linux keeps secrets in <state-dir>/secrets.json")
}

func platformServices() service.Manager { return systemd.New() }

// lookupGroup is user.LookupGroup (replaced in tests).
var lookupGroup = user.LookupGroup

// tailnetInterface is the device of `ip route get 100.100.100.100`
// (read-only; tailscale0 normally).
func tailnetInterface(ctx context.Context) (string, error) {
	r, err := linux.New(linux.Options{}).RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}

// setupPlatform attaches the Linux pieces:
//   - the systemd unit names replace the macOS labels in settings (only
//     labels still at their macOS default);
//   - the socket group: the macOS default "admin" rarely exists on Linux,
//     so when it does not, sudo, wheel or Twinnel is used (linux.SocketGroup);
//   - the Tailscale interface lookup and the health scheduler over
//     linux.Env. There is no network watcher on Linux yet: health runs on
//     the interval and after applies and service operations.
func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	linuxSettings(e)
	e.Autostarts = autostartSource(o)
	o.group = linuxGroup(o.group)

	switch o.tailnetIface {
	case "", "auto":
		e.TailnetInterface = tailnetInterface
	case "none":
	default:
		name := o.tailnetIface
		e.TailnetInterface = func(context.Context) (string, error) { return name, nil }
	}
	if !o.noHealth {
		e.SetHealth(&health.Scheduler{
			Runner:   &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
			NewEnv:   func() health.Env { return linux.New(healthOptions(e, o)) },
			Interval: o.healthInterval,
		})
		e.EgressProbe = egressProbe(func() health.Env {
			opt := healthOptions(e, o)
			opt.Settings.Egress = true
			return linux.New(opt)
		})
	}
	return func(context.Context) {}
}

// linuxSettings rewrites the macOS default service labels to the Linux
// unit names (twinnel-mihomo, tailscaled, twinnel-helper) once.
func linuxSettings(e *engine.Engine) {
	st, err := e.Store.Settings()
	if err != nil {
		return
	}
	if linux.LinuxLabels(st) {
		if err := e.Store.SaveSettings(st); err != nil {
			e.Log.Warn("could not save the Linux service names", "err", err)
		}
	}
}

// linuxGroup keeps an explicit or existing group and replaces the macOS
// default "admin" when the system has no such group.
func linuxGroup(g string) string {
	if g != "" && g != "admin" {
		return g
	}
	if _, err := lookupGroup("admin"); err == nil {
		return "admin"
	}
	name, _ := linux.SocketGroup(lookupGroup)
	return name
}

// healthOptions points the checks at the mihomo the helper manages, read
// fresh from settings for every run.
func healthOptions(e *engine.Engine, opts *options) linux.Options {
	o := linux.Options{TailscaleStatus: devTailnetStatus(e, opts), Dataplane: health.NetProbes{}}
	settings := health.DefaultSettings()
	o.Settings = &settings
	healthExtras(e, &settings)
	st, err := e.Store.Settings()
	if err != nil {
		return o
	}
	settings.Egress = st.HealthEgress // 定期检测
	if len(st.LeakGuardGroups) > 0 {
		// The groups the user chose in 防泄漏 are the ones the checks guard.
		settings.SensitiveGroups = st.LeakGuardGroups
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
