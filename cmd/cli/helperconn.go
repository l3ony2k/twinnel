package main

// How every CLI command reaches the helper and, through it, mihomo's
// controller and the user's settings. Commands do not dial, read app.json
// or pick a secret on their own: a new command that talks to mihomo calls
// controllerFor, one that needs the checks' settings calls healthSettings.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// helperAskTimeout bounds the helper lookups of read commands: they still
// work, from their fallbacks, while the helper is down.
const helperAskTimeout = 3 * time.Second

// dialHelper connects to the helper at socket; the error says where it
// looked.
func dialHelper(ctx context.Context, socket string) (*rpc.Client, error) {
	c, err := rpc.Dial(ctx, nil, socket)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the helper at %s: %w", socket, err)
	}
	return c, nil
}

// Replaceable in tests (no test may reach a real helper).
var (
	// helperRPC makes one call to the helper at socket.
	helperRPC = func(ctx context.Context, socket, method string, params, result any) error {
		c, err := dialHelper(ctx, socket)
		if err != nil {
			return err
		}
		defer c.Close()
		return c.Call(ctx, method, params, result)
	}
	// appConfigPath is app.json, the fallback for the controller.
	appConfigPath = status.AppConfigPath
)

// helperAsk is helperRPC with helperAskTimeout.
func helperAsk(ctx context.Context, socket, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, helperAskTimeout)
	defer cancel()
	return helperRPC(ctx, socket, method, params, result)
}

// Where a controllerTarget came from.
const (
	controllerFromHelper = "helper" // controller.credentials
	controllerFromEnv    = "env"    // TWINNEL_CONTROLLER (development)
	controllerFromApp    = "app"    // app.json, else 127.0.0.1:9090
)

// controllerFlagUsage is the help of every command's --controller.
const controllerFlagUsage = "mihomo controller `HOST:PORT` (default: the helper's, else the app's setting, else 127.0.0.1:9090)"

// controllerTarget is the mihomo controller a command talks to.
type controllerTarget struct {
	status.Config // Controller and Secret; the rest of app.json too
	// Source is controllerFrom*. --controller changes the address only.
	Source string
}

// controllerFor resolves the controller for a command, the same rule as
// the app's (status.Config.WithHelperCredentials):
//
//  1. app.json and the environment (TWINNEL_CONTROLLER,
//     TWINNEL_MIHOMO_SECRET), else 127.0.0.1:9090: the fallback;
//  2. the helper's controller.credentials over them: the secret mihomo
//     actually loaded (app.json's goes stale, and has none on Windows or
//     after controller.secure) — unless TWINNEL_CONTROLLER pins it;
//  3. flagAddr (--controller) over the address. Its secret stays the
//     resolved one unless TWINNEL_MIHOMO_SECRET is set.
func controllerFor(ctx context.Context, socket, flagAddr string) controllerTarget {
	cfg, err := status.LoadConfig(appConfigPath(), os.Getenv)
	if err != nil {
		cfg, _ = status.LoadConfig("", os.Getenv)
	}
	t := controllerTarget{Config: cfg, Source: controllerFromApp}
	if strings.TrimSpace(os.Getenv(status.EnvController)) != "" {
		t.Source = controllerFromEnv
	} else {
		var cr rpc.ControllerCredentials
		if err := helperAsk(ctx, socket, rpc.MethodControllerCredentials, nil, &cr); err == nil {
			t.Config = cfg.WithHelperCredentials(cr.Controller, cr.Secret, os.Getenv)
			t.Source = controllerFromHelper
		}
	}
	if flagAddr = strings.TrimSpace(flagAddr); flagAddr != "" {
		t.Controller = flagAddr
		if s := os.Getenv(status.EnvSecret); s != "" {
			t.Secret = s
		}
	}
	return t
}

// healthSettings are the check settings of a local run (doctor, MCP's
// doctor): the defaults with the user's choices from the helper's
// settings (防泄漏's groups, 定期检测), exactly what the helper's own runs
// use. ok is false when the helper did not answer (the defaults stay).
func healthSettings(ctx context.Context, socket string) (s health.Settings, ok bool) {
	s = health.DefaultSettings()
	s.Binaries = doctorBinaries()
	var set rpc.Settings
	if err := helperAsk(ctx, socket, rpc.MethodSettingsGet, nil, &set); err != nil {
		return s, false
	}
	s.ApplyUserChoices(set.LeakGuardGroups, set.HealthEgress)
	return s, true
}
