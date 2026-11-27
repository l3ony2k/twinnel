//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/platform/darwin"
)

// statusFile reads tailscale.StatusFileEnv's file in place of
// `tailscale status --json` (development: synthetic shots), or is nil.
func statusFile() func(context.Context) ([]byte, error) {
	f := os.Getenv(tailscale.StatusFileEnv)
	if f == "" {
		return nil
	}
	return func(context.Context) ([]byte, error) { return os.ReadFile(f) }
}

// devConfigPath pins the mihomo config the in-process checks read on a
// dev or shot run (DUET_CONTROLLER set): DUET_MIHOMO_CONFIG, else the
// default path under $HOME (the scratch home). Without it the checks
// would find the live LaunchDaemon's config and put its contents in
// the shots. Empty (discover) on a normal run.
func devConfigPath() string {
	if p := os.Getenv("DUET_MIHOMO_CONFIG"); p != "" {
		return p
	}
	if os.Getenv("DUET_CONTROLLER") == "" {
		return ""
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "mihomo", "config.yaml")
}

// newHealthSource runs the health checks in-process with the macOS Env,
// read-only. Later the helper serves the same report over RPC.
func newHealthSource(cfg status.Config) status.HealthSource {
	settings := health.DefaultSettings()
	return status.RunnerSource{
		Runner: &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
		Env: func() health.Env {
			return darwin.New(darwin.Options{
				Controller:      cfg.Controller,
				Secret:          cfg.Secret,
				ConfigPath:      devConfigPath(),
				Settings:        &settings,
				TailscaleStatus: statusFile(),
			})
		},
	}
}

// newEgressProbe runs the opt-in egress test once (health.LeakEgress with
// Settings.Egress on): one HTTPS request to an IP-echo address directly
// and one through mihomo. The 检测出口 button on the health page.
func newEgressProbe(cfg status.Config) func(context.Context) *health.CheckReport {
	return func(ctx context.Context) *health.CheckReport {
		settings := health.DefaultSettings()
		settings.Egress = true
		r := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 20 * time.Second, Only: []string{health.LeakEgress{}.ID()}}
		rep := r.Run(health.WithLang(ctx, i18n.Tag()), darwin.New(darwin.Options{
			Controller:      cfg.Controller,
			Secret:          cfg.Secret,
			ConfigPath:      devConfigPath(),
			Settings:        &settings,
			TailscaleStatus: statusFile(),
		}))
		if len(rep.Checks) == 0 {
			return nil
		}
		return &rep.Checks[0]
	}
}
