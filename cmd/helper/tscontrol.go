package main

import (
	"context"

	"github.com/l3ony2k/twinnel/core/tailscale"
)

// tsControl is engine.TailscaleControl over tailscaled's LocalAPI (the
// default socket). The helper is root, so writes are always granted.
type tsControl struct{ c *tailscale.Client }

func newTSControl() tsControl { return tsControl{tailscale.New(tailscale.LocalAPI(nil), true)} }

func (t tsControl) LoginState(ctx context.Context) (string, string, string, error) {
	st, err := t.c.State(ctx)
	if err != nil {
		return "", "", "", err
	}
	return st.BackendState, st.AuthURL, st.Tailnet, nil
}

func (t tsControl) Up(ctx context.Context) error         { return t.c.Up(ctx) }
func (t tsControl) StartLogin(ctx context.Context) error { return t.c.Login(ctx) }
