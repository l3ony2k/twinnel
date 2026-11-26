//go:build darwin

package main

import (
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/platform/darwin"
)

// newHealthSource runs the health checks in-process with the macOS Env,
// read-only. Later the helper serves the same report over RPC.
func newHealthSource(cfg status.Config) status.HealthSource {
	settings := health.DefaultSettings()
	return status.RunnerSource{
		Runner: &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second},
		Env: func() health.Env {
			return darwin.New(darwin.Options{
				Controller: cfg.Controller,
				Secret:     cfg.Secret,
				Settings:   &settings,
			})
		},
	}
}
