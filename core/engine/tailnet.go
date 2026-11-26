package engine

import (
	"context"
	"slices"
	"time"

	"github.com/l3ony2k/duet/core/config"
	"github.com/l3ony2k/duet/core/rpc"
)

const tailnetCache = "tailnet.json"

func (e *Engine) cachedTailnet() (config.TailnetInfo, bool) {
	var info config.TailnetInfo
	if err := e.Store.ReadJSON(tailnetCache, &info); err != nil {
		return config.TailnetInfo{}, false
	}
	return info, true
}

func (e *Engine) saveTailnet(info config.TailnetInfo) {
	info.Resolver = "" // comes from settings at compose time
	if err := e.Store.WriteJSON(tailnetCache, info); err != nil {
		e.log().Warn("save tailnet cache", "err", err)
	}
}

func sameTailnet(a, b config.TailnetInfo) bool {
	return a.MagicDNSSuffix == b.MagicDNSSuffix && a.IPv4CIDR == b.IPv4CIDR && a.IPv6CIDR == b.IPv6CIDR &&
		slices.Equal(a.PeerShortNames, b.PeerShortNames) && slices.Equal(a.ProcessNames, b.ProcessNames)
}

// RefreshTailnet reads `tailscale status --json`. When the peers or the
// MagicDNS suffix changed, the cache is updated and, with coexist and
// auto-apply on, the config is composed and applied again.
func (e *Engine) RefreshTailnet(ctx context.Context) (rpc.TailnetRefreshResult, error) {
	var out rpc.TailnetRefreshResult
	if e.Tailnet == nil {
		return out, rpc.Errorf(rpc.CodeFailed, "no tailscale status source")
	}
	raw, err := e.Tailnet.Status(ctx)
	now := e.now().UTC()
	if err == nil {
		var info config.TailnetInfo
		info, err = config.TailnetInfoFromStatus(raw)
		if err == nil {
			old, had := e.cachedTailnet()
			out.Changed = !had || !sameTailnet(old, info)
			if out.Changed {
				e.saveTailnet(info)
			}
			e.mu.Lock()
			e.tailnet = rpc.TailnetStatus{Peers: info.PeerShortNames, Suffix: info.MagicDNSSuffix, LastPoll: now}
			e.mu.Unlock()
		}
	}
	if err != nil {
		e.mu.Lock()
		e.tailnet.LastPoll, e.tailnet.Error = now, err.Error()
		e.mu.Unlock()
		return out, err
	}
	st, serr := e.Store.Settings()
	if serr != nil {
		return out, serr
	}
	e.mu.Lock()
	out.Tailnet = e.tailnet
	e.mu.Unlock()
	out.Tailnet.Coexist = st.Tailscale.Coexist
	if out.Changed {
		e.publish(rpc.EventTailnetChanged, out.Tailnet)
		if st.Tailscale.Coexist && *st.AutoApply {
			if _, ok := st.ActiveSubscription(); ok {
				r, _ := e.Apply(ctx, rpc.ApplyParams{Reason: "tailnet changed"})
				out.Apply = r
			}
		}
	}
	return out, nil
}

func (e *Engine) tailnetLoop(ctx context.Context) {
	if e.Tailnet == nil {
		return
	}
	for {
		st, err := e.Store.Settings()
		interval := time.Minute
		if err == nil {
			interval = time.Duration(st.Tailscale.PollSeconds) * time.Second
			if st.Tailscale.Coexist {
				if _, err := e.RefreshTailnet(ctx); err != nil && ctx.Err() == nil {
					e.log().Debug("tailnet poll", "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
