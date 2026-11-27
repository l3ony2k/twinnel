//go:build darwin

package darwin

import (
	"context"

	"github.com/l3ony2k/twinnel/core/health"
)

var _ health.DataplaneEnv = (*Env)(nil)

// DialTCP, DialViaProxy and Ping run the tun-dataplane probes through
// Options.Dataplane, or report health.ErrNotSupported without it.

func (e *Env) DialTCP(ctx context.Context, addr string) error {
	if e.opt.Dataplane == nil {
		return health.ErrNotSupported
	}
	return e.opt.Dataplane.DialTCP(ctx, addr)
}

func (e *Env) DialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	if e.opt.Dataplane == nil {
		return health.ErrNotSupported
	}
	return e.opt.Dataplane.DialViaProxy(ctx, proxy, addr, serverName)
}

func (e *Env) Ping(ctx context.Context, ip string) error {
	if e.opt.Dataplane == nil {
		return health.ErrNotSupported
	}
	return e.opt.Dataplane.Ping(ctx, ip)
}
