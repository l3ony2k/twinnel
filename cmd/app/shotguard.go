package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// shotGuard refuses a shot run (TWINNEL_SHOTS set) that could reach the
// owner's live stack or show their machine in a picture. It must name
//
//   - a scratch helper socket (TWINNEL_SOCKET, never the installed helper's);
//   - a fake controller (TWINNEL_CONTROLLER, never port 9090);
//   - a synthetic tailnet (TWINNEL_TAILSCALE_STATUS_FILE, a readable file);
//   - a scratch HOME (set, absolute, not the user's real home), since the
//     app's config, logs and takeover paths live under it.
//
// The health checks and the live-setup detection then read fixtures
// (probes.go). A normal run passes.
func shotGuard(getenv func(string) string) error {
	if getenv("TWINNEL_SHOTS") == "" {
		return nil
	}
	if s := getenv("TWINNEL_SOCKET"); s == "" || filepath.Clean(s) == rpc.DefaultSocket {
		return errors.New("shots need TWINNEL_SOCKET set to a scratch helper's socket, not the installed helper's")
	}
	c := getenv("TWINNEL_CONTROLLER")
	if c == "" {
		return errors.New("shots need TWINNEL_CONTROLLER set to a fake mihomo controller")
	}
	if _, port, err := net.SplitHostPort(c); err != nil || port == "9090" {
		return errors.New("shots refuse TWINNEL_CONTROLLER " + c + ": port 9090 is the live mihomo")
	}
	f := getenv(tailscale.StatusFileEnv)
	if f == "" {
		return errors.New("shots need " + tailscale.StatusFileEnv + " set to a synthetic tailscale status (cmd/app/testdata/shots-tailscale-status.json)")
	}
	if _, err := os.Stat(f); err != nil {
		return fmt.Errorf("shots: %s: %w", tailscale.StatusFileEnv, err)
	}
	if f := getenv(envHealthFixture); f != "" {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("shots: %s: %w", envHealthFixture, err)
		}
	}
	home := getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return errors.New("shots need HOME set to a scratch directory")
	}
	if real := realHome(); real != "" && filepath.Clean(home) == filepath.Clean(real) {
		return errors.New("shots refuse HOME " + home + ": it is the real home; set HOME to a scratch directory")
	}
	return nil
}

// realHome is the user's home from the user database, which ignores
// $HOME; "" when unknown. A variable so tests can pin it.
var realHome = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.HomeDir
}
