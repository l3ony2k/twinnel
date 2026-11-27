//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/platform/darwin"
)

// statusFile reads tailscale.StatusFileEnv's file in place of
// `tailscale status --json` (development), or is nil.
func statusFile() func(context.Context) ([]byte, error) {
	f := os.Getenv(tailscale.StatusFileEnv)
	if f == "" {
		return nil
	}
	return func(context.Context) ([]byte, error) { return os.ReadFile(f) }
}

// devConfigPath pins the mihomo config the in-process checks read on a
// dev run (DUET_CONTROLLER set): DUET_MIHOMO_CONFIG, else the default
// path under $HOME (the scratch home). Without it the checks would find
// the live LaunchDaemon's config. Empty (discover) on a normal run. Shot
// runs never get here: they read a fixture (probes.go).
func devConfigPath() string {
	if p := os.Getenv("DUET_MIHOMO_CONFIG"); p != "" {
		return p
	}
	if os.Getenv("DUET_CONTROLLER") == "" {
		return ""
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "mihomo", "config.yaml")
}

// healthEnv is the macOS Env: read-only commands (route, netstat,
// ifconfig, scutil, networksetup), the tailscale CLI and GETs to the
// controller.
func (realProbes) healthEnv(cfg status.Config, settings health.Settings) health.Env {
	return darwin.New(darwin.Options{
		Controller:      cfg.Controller,
		Secret:          cfg.Secret,
		ConfigPath:      devConfigPath(),
		Settings:        &settings,
		TailscaleStatus: statusFile(),
	})
}
