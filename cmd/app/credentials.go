package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// credsState holds the controller address and secret from the helper
// (controller.credentials). While set they replace cfg.Controller and
// cfg.Secret for the monitor and the local health checks, so the app keeps
// working after controller.secure rotates the secret. cfg stays the
// fallback when there is no helper (or an older one). The secret is never
// logged and never shown. Main thread.
type credsState struct {
	cur      *rpc.ControllerCredentials
	loading  bool
	again    bool // another refresh was asked for while one ran
	tooOld   bool // the helper has no controller.credentials
	fetchErr string
}

// controllerConfig is cfg with the helper's controller credentials applied.
func controllerConfig(cfg status.Config, c *rpc.ControllerCredentials) status.Config {
	if c == nil {
		return cfg
	}
	// Development: TWINNEL_CONTROLLER pins the controller (and its secret
	// comes from the environment too), so a dev or shot run against a
	// scratch helper never reaches the live mihomo through the helper's
	// default settings.
	if os.Getenv("TWINNEL_CONTROLLER") != "" {
		return cfg
	}
	if c.Controller != "" {
		cfg.Controller = c.Controller
	}
	cfg.Secret = c.Secret
	return cfg
}

// fetchCredentials asks the helper for the controller credentials. A nil
// result with a nil error means "use cfg": not connected, or an older
// helper (tooOld).
func fetchCredentials(ctx context.Context, hc helperCaller) (c *rpc.ControllerCredentials, tooOld bool, err error) {
	if hc == nil || !hc.State().Connected() {
		return nil, false, nil
	}
	var r rpc.ControllerCredentials
	if err := hc.Call(ctx, rpc.MethodControllerCredentials, nil, &r); err != nil {
		if isTooOld(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return &r, false, nil
}

func sameCreds(a, b *rpc.ControllerCredentials) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Controller == b.Controller && a.Secret == b.Secret
}

// refreshCredentials refetches the credentials and restarts the monitor
// when they changed. Called on connect, on compose.applied and after
// controller.secure / takeover.adopt. On a fetch error the last known
// credentials stay. Main thread.
func (a *app) refreshCredentials() {
	cs := &a.creds
	if a.hc == nil {
		return
	}
	if cs.loading {
		cs.again = true
		return
	}
	cs.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		c, tooOld, err := fetchCredentials(ctx, hc)
		cancel()
		mygo.RunOnMain(func() {
			cs.loading = false
			cs.tooOld = tooOld
			cs.fetchErr = ""
			switch {
			case err != nil:
				cs.fetchErr = helperError(err)
				log.Printf("controller.credentials: %s", cs.fetchErr) // never the secret
			case c == nil && !tooOld:
				// Not connected: keep the last known credentials (a helper
				// restart should not flip the app to a stale cfg.Secret).
			default:
				changed := !sameCreds(cs.cur, c)
				cs.cur = c
				if changed {
					a.start()
				}
			}
			if cs.again {
				cs.again = false
				a.refreshCredentials()
			}
			a.invalidate()
		})
	}()
}

// credentialsEvent reports whether a batch may have changed the
// controller address or secret.
func credentialsEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		if e.Type == rpc.EventComposeApplied {
			return true
		}
	}
	return false
}
