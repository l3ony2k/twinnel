package windows

import (
	"context"

	"github.com/l3ony2k/twinnel/core/health"
)

// Probes is the tun-dataplane probes of a real Windows machine
// (Options.Dataplane): health.NetProbes, with Ping through IcmpSendEcho,
// which needs no raw socket.
func Probes() health.DataplaneEnv { return probes{} }

type probes struct{ health.NetProbes }

func (probes) Ping(ctx context.Context, ip string) error { return icmpEcho(ctx, ip) }
