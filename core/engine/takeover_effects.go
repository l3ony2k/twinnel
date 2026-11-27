package engine

import (
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/takeover"
)

// takeoverEffects says what adopt does to each piece of the existing
// setup, as rows a client shows next to what it found (现在 | 接管后).
// They follow Adopt and importPlan: the service definition and the binary
// are kept (the service is marked adopted, its plist never replaced), the
// config is rewritten in place after an exact backup, a single
// subscription profile's URL goes into the secret store (not refreshed on
// a timer: IntervalMinutes 0), the live controller secret is kept, and
// Tailscale is not touched.
func takeoverEffects(p *takeover.Plan, ps *planState) []rpc.TakeoverEffect {
	m := p.Discovery.Mihomo
	if m == nil {
		return nil
	}
	var out []rpc.TakeoverEffect
	if m.Service != nil {
		out = append(out, rpc.TakeoverEffect{Item: rpc.EffMihomoService, Action: rpc.ActKeep, Subject: m.Service.Label})
	} else {
		out = append(out, rpc.TakeoverEffect{Item: rpc.EffMihomoService, Action: rpc.ActNone})
	}
	bin := rpc.TakeoverEffect{Item: rpc.EffMihomoBinary, Action: rpc.ActKeep, Subject: m.Binary}
	if ps.BinaryUnsafe {
		bin.Warn = rpc.WarnReplaceable
	}
	out = append(out, bin,
		rpc.TakeoverEffect{Item: rpc.EffConfig, Action: rpc.ActRewrite, Subject: p.Live, Warn: rpc.WarnEditsLost})
	switch n := len(m.Profiles); {
	case ps.Profile != nil:
		out = append(out, rpc.TakeoverEffect{Item: rpc.EffSubscription, Action: rpc.ActStore, Subject: ps.Profile.Name})
	case n > 1:
		out = append(out, rpc.TakeoverEffect{Item: rpc.EffSubscription, Action: rpc.ActSkip, Warn: rpc.WarnManyProfile, Count: n})
	default:
		out = append(out, rpc.TakeoverEffect{Item: rpc.EffSubscription, Action: rpc.ActNone})
	}
	sec := rpc.TakeoverEffect{Item: rpc.EffSecret, Action: rpc.ActKeep}
	if !m.SecretSet {
		sec.Action, sec.Warn = rpc.ActNone, rpc.WarnNoSecret
	}
	out = append(out, sec)
	ts := rpc.TakeoverEffect{Item: rpc.EffTailscale, Action: rpc.ActUnchanged}
	if t := p.Discovery.Tailscale; t != nil && t.Service != nil {
		ts.Subject = t.Service.Label
	}
	return append(out, ts)
}
