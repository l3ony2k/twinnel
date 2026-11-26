//go:build darwin

package main

import (
	"context"
	"os"
	"time"

	"github.com/l3ony2k/duet/core/health"
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
				Settings:        &settings,
				TailscaleStatus: statusFile(),
			})
		},
	}
}
