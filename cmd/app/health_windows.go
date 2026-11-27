//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
	"github.com/l3ony2k/twinnel/platform/windows"
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
// dev run (TWINNEL_CONTROLLER set): TWINNEL_MIHOMO_CONFIG, else
// %USERPROFILE%\.config\mihomo\config.yaml (the scratch home). Empty
// (discover) on a normal run.
func devConfigPath() string {
	if p := os.Getenv("TWINNEL_MIHOMO_CONFIG"); p != "" {
		return p
	}
	if os.Getenv("TWINNEL_CONTROLLER") == "" {
		return ""
	}
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".config", "mihomo", "config.yaml")
}

// healthEnv is the Windows Env: adapters, routes and NRPT through
// iphlpapi and the registry, the tailscale CLI and GETs to the controller.
func (realProbes) healthEnv(cfg status.Config, settings health.Settings) health.Env {
	return windows.New(windows.Options{
		Dataplane:       windows.Probes(),
		Controller:      cfg.Controller,
		Secret:          cfg.Secret,
		ConfigPath:      devConfigPath(),
		Settings:        &settings,
		TailscaleStatus: statusFile(),
	})
}
