package healthfix

import (
	"context"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// MihomoClient adapts a controller client (with writes allowed).
func MihomoClient(c *mihomo.Client) Mihomo { return mihomoAdapter{c} }

type mihomoAdapter struct{ c *mihomo.Client }

func (m mihomoAdapter) Mode(ctx context.Context) (string, error) {
	cfg, err := m.c.Configs(ctx)
	return string(cfg.Mode), err
}

func (m mihomoAdapter) SetMode(ctx context.Context, mode string) error {
	return m.c.SetMode(ctx, mihomo.Mode(mode))
}

// TailscaleClient adapts a LocalAPI client (with writes allowed).
func TailscaleClient(c *tailscale.Client) Tailscale { return tsAdapter{c} }

type tsAdapter struct{ c *tailscale.Client }

func (t tsAdapter) ExitNode(ctx context.Context) (string, error) {
	st, err := t.c.State(ctx)
	if err != nil || st.ExitNode == nil {
		return "", err
	}
	return st.ExitNode.ID, nil
}

func (t tsAdapter) SetExitNode(ctx context.Context, id string) error { return t.c.SetExitNode(ctx, id) }
