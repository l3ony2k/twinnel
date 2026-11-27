//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/priv"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/platform/windows"
	"github.com/l3ony2k/twinnel/platform/windows/scm"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
	"github.com/l3ony2k/twinnel/platform/windows/winsec"
)

// secureSecrets: an elevated helper (the service runs as LocalSystem)
// keeps secrets sealed with DPAPI in <state>\secrets.dpapi.
const secureSecrets = "dpapi"

func newKeychain(string) (store.Secrets, error) {
	return nil, errors.New("--secrets keychain is only available on macOS; Windows uses --secrets dpapi")
}

// platformServices: the SCM. Hosted services (mihomo) get a copy of this
// binary as their service host.
func platformServices() service.Manager {
	exe, _ := os.Executable()
	return scm.New(exe)
}

// platformExecutor carries out the sc.exe steps through the SCM API.
func platformExecutor() service.Executor { return scm.Executor{} }

// listenTransport: the named pipe, with a DACL for LocalSystem,
// Administrators, this process's user (a development helper) and the
// --allow-sid users; every connection is checked again by SID.
func listenTransport(o *options) (rpc.Transport, rpc.Authorizer, error) {
	self, err := priv.CurrentUserSID()
	if err != nil {
		return nil, nil, err
	}
	allowed := append([]string{self}, o.allowSIDs...)
	sddl, err := rpc.PipeSDDL(allowed...)
	if err != nil {
		return nil, nil, fmt.Errorf("--allow-sid: %w", err)
	}
	return &rpc.PipeTransport{SDDL: sddl}, &rpc.SIDAuthorizer{SelfSID: self, Allowed: o.allowSIDs}, nil
}

// prepareStateDir: an elevated helper creates the state dir and resets its
// DACL to LocalSystem + Administrators before anything is written there
// (%ProgramData% lets every user read by default).
func prepareStateDir(dir string) error {
	if !priv.Elevated() {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := winsec.SecureStateDir(dir); err != nil {
		return fmt.Errorf("secure %s: %w", dir, err)
	}
	return nil
}

// runService runs the helper under the Service Control Manager when it was
// started by it, and reports false otherwise (a console run).
func runService(o *options) (bool, error) {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false, nil
	}
	h := &serviceHandler{o: o}
	if err := svc.Run(winpath.HelperService, h); err != nil {
		return true, err
	}
	return true, h.err
}

type serviceHandler struct {
	o   *options
	err error
}

// Execute implements svc.Handler: Running once the pipe listens; Stop and
// Shutdown end run like SIGTERM does on Unix. A failed start or
// helper.restart ends with a service-specific error, so the SCM's
// failure actions (set with failureflag) start the helper again.
func (h *serviceHandler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending, WaitHint: 30000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, h.o, func(*engine.Engine) {
			st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
		})
	}()
	for {
		select {
		case err := <-done:
			h.err = err
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending, WaitHint: 15000}
				cancel()
				if err := <-done; err != nil && !errors.Is(err, errRestart) {
					h.err = err
				}
				return false, 0
			}
		}
	}
}

// tailnetInterface is the adapter holding the route to 100.100.100.100
// (read-only; "Tailscale" normally).
func tailnetInterface(ctx context.Context) (string, error) {
	return windows.New(windows.Options{}).TailscaleInterface(ctx)
}

// setupPlatform attaches the Windows pieces: the Tailscale CLI path in
// settings, the Tailscale adapter lookup and the health scheduler over
// windows.Env. No network watcher yet: health runs on the interval and
// after applies and service operations.
func setupPlatform(e *engine.Engine, o *options) func(ctx context.Context) {
	windowsSettings(e)
	e.Autostarts = autostartSource(o)
	if d := systemDNS(o); d != nil {
		e.SystemDNS = d
	}
	// tailscaled gives SYSTEM's logins to no user: the app or the CLI
	// starts the login in the user's session (tailscale.LoginInSession).
	e.TSLoginInSession = true
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
			NewEnv:   func() health.Env { return windows.New(healthOptions(e, o)) },
			Interval: o.healthInterval,
		})
		e.EgressProbe = egressProbe(func() health.Env {
			opt := healthOptions(e, o)
			opt.Settings.Egress = true
			return windows.New(opt)
		})
	}
	return func(context.Context) {}
}

// systemDNS is dns.*'s platform part: adapters from iphlpapi, hand-set
// servers from the registry, changes through netsh, and only when the
// helper runs elevated (a development helper reads only).
func systemDNS(o *options) *windows.SystemDNS {
	if o.noSystemDNS {
		return nil
	}
	run := windows.ExecRunner(20 * time.Second)
	return &windows.SystemDNS{Sys: windows.NewSys(), Static: windows.NewStaticDNS(), Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if !priv.Elevated() {
			return nil, errors.New("setting the system DNS needs the installed (elevated) helper")
		}
		return run(ctx, name, args...)
	}}
}

// windowsSettings fills the Tailscale CLI path once, when the official
// client is installed where its installer puts it.
func windowsSettings(e *engine.Engine) {
	st, err := e.Store.Settings()
	if err != nil || st.Tailscale.Binary != "" {
		return
	}
	cli := winpath.TailscaleCLI()
	if _, err := os.Stat(cli); err != nil {
		return
	}
	st.Tailscale.Binary = cli
	if err := e.Store.SaveSettings(st); err != nil {
		e.Log.Warn("could not save the Tailscale CLI path", "err", err)
	}
}

// healthOptions points the checks at the mihomo the helper manages, read
// fresh from settings for every run.
func healthOptions(e *engine.Engine, opts *options) windows.Options {
	o := windows.Options{TailscaleStatus: devTailnetStatus(e, opts)}
	settings := health.DefaultSettings()
	o.Settings = &settings
	healthExtras(e, &settings)
	st, err := e.Store.Settings()
	if err != nil {
		return o
	}
	settings.Egress = st.HealthEgress
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
