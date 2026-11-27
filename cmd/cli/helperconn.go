package main

// How every CLI command reaches the helper and, through it, mihomo's
// controller and the user's settings. Commands do not dial, read app.json
// or pick a secret on their own: a new command that talks to mihomo calls
// controllerFor, one that needs the checks' settings calls healthSettings.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/foreign"
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
	// Helper is the helper's status.get, asked with its credentials (nil
	// when it did not answer).
	Helper *rpc.Status
	// Owner is who listens on Controller as the helper's preflight found
	// it (Helper.Mihomo.Owner): nil when the helper did not say, or when
	// the address is not the helper's (TWINNEL_CONTROLLER, --controller).
	Owner *foreign.Owner
}

// errForeignController wraps the refusal of writableController.
var errForeignController = errors.New("the controller belongs to another mihomo")

// writable returns an error when the controller is another program's
// mihomo (Clash Party, Clash Verge Rev…): Twinnel reads it at most and
// never changes it. The helper refuses its own writes the same way.
func (t controllerTarget) writable() error {
	if !t.Owner.IsForeign() {
		return nil
	}
	return fmt.Errorf("%w: %s; Twinnel does not change it (twinnel doctor --fix controller-owner)", errForeignController, ownerText(*t.Owner))
}

// ownerText names a foreign owner for the CLI: address, program, pid and
// executable.
func ownerText(o foreign.Owner) string {
	s := o.Label()
	if o.Exe != "" {
		s += " " + o.Exe
	}
	return s
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
//
// It keeps the helper's status.get (Helper) and, with the helper's
// address, who holds it (Owner); a command that changes mihomo checks
// writable first.
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
	flagAddr = strings.TrimSpace(flagAddr)
	// The helper's status (not when it just did not answer, nor while
	// TWINNEL_CONTROLLER pins a development controller): the owner of its
	// controller address, when that is the one used.
	if t.Source == controllerFromHelper {
		sctx, cancel := context.WithTimeout(ctx, helperAskTimeout)
		if st, err := helperStatus(sctx, socket); err == nil {
			t.Helper = &st
			if flagAddr == "" {
				t.Owner = st.Mihomo.Owner
			}
		}
		cancel()
	}
	if flagAddr != "" {
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
	if len(set.HealthEgressURLs) > 0 {
		s.EgressURLs = append([]string(nil), set.HealthEgressURLs...)
	}
	return s, true
}
