package tailscale

import (
	"context"

	"tailscale.com/ipn"
)

// UpUnattended connects the node in Unattended Mode (ForceDaemon), as
// `tailscale up --unattended`: on Windows the node then keeps running
// after the client that started it disconnects or its user signs out.
// Other platforms ignore the flag.
func (c *Client) UpUnattended(ctx context.Context) error {
	return c.edit(ctx, &ipn.MaskedPrefs{
		Prefs:          ipn.Prefs{WantRunning: true, ForceDaemon: true},
		WantRunningSet: true, ForceDaemonSet: true,
	})
}

// LoginInSession starts an interactive login from the calling process,
// which must run as the signed-in user (the app, or the CLI), never as
// the helper. On Windows tailscaled keeps one profile per Windows user and
// never gives one to SYSTEM: a login the helper (LocalSystem) starts
// belongs to nobody, and the user's Tailscale tray app switches away from
// it. So on Windows the login runs here, in Unattended Mode, so it stays
// up when this process exits. The login URL then shows in the node's
// state (tailscale.login reads it, as SYSTEM may).
func (c *Client) LoginInSession(ctx context.Context, unattended bool) error {
	up := c.Up
	if unattended {
		up = c.UpUnattended
	}
	if err := up(ctx); err != nil {
		return err
	}
	return c.Login(ctx)
}
