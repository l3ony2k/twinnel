package setup

import (
	"context"

	"github.com/l3ony2k/twinnel/core/tailscale"
)

// LocalTailnet is Tailnet over the Tailscale LocalAPI client (writes must
// be allowed on it).
func LocalTailnet(c *tailscale.Client) Tailnet { return localTailnet{c} }

type localTailnet struct{ c *tailscale.Client }

func (l localTailnet) LoginState(ctx context.Context) (LoginState, error) {
	st, err := l.c.State(ctx)
	if err != nil {
		return LoginState{}, err
	}
	return LoginState{Backend: st.BackendState, AuthURL: st.AuthURL, Tailnet: st.Tailnet}, nil
}

// StartLogin is `tailscale up` on a new node: connect, then ask for an
// interactive login (the node then reports the URL to open).
func (l localTailnet) StartLogin(ctx context.Context) error {
	if err := l.c.Up(ctx); err != nil {
		return err
	}
	return l.c.Login(ctx)
}
