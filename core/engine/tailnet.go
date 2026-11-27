package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
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

// sameTailnet compares the whole value through its JSON form (the form it
// is cached in), so fields added to TailnetInfo later are covered too.
func sameTailnet(a, b config.TailnetInfo) bool {
	a.Resolver, b.Resolver = "", ""
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ja, jb)
}

// tailnetIface looks up the Tailscale interface. Only utun* names count:
// while tailscaled is down the route to 100.100.100.100 follows the default
// route (en0), which must not be used. On failure prev is kept, so a
// transient lookup error doesn't re-apply the config.
func (e *Engine) tailnetIface(ctx context.Context, prev string) string {
	if e.TailnetInterface == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	name, err := e.TailnetInterface(ctx)
	name = strings.TrimSpace(name)
	if err != nil || !isTunnelIface(name) {
		if err != nil {
			e.log().Debug("tailnet interface lookup", "err", err)
		}
		return prev
	}
	return name
}

func isTunnelIface(name string) bool {
	// utunN on macOS, tailscale0 on Linux, "Tailscale" on Windows.
	return strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tailscale") || name == "Tailscale"
}

// RefreshTailnet reads `tailscale status --json`. When the peers or the
// MagicDNS suffix changed, the cache is updated and, with coexist and
// auto-apply on, the config is composed and applied again. Auto-apply only
// re-applies: before Twinnel has applied anything (no history) a change is
// recorded but nothing is applied, so the first poll of a new helper, which
// always reports a change (there is no cache yet), never applies a config
// on its own, possibly half-way through the user's setup.
func (e *Engine) RefreshTailnet(ctx context.Context) (rpc.TailnetRefreshResult, error) {
	var out rpc.TailnetRefreshResult
	if e.Tailnet == nil {
		return out, rpc.Errorf(rpc.CodeFailed, "no tailscale status source")
	}
	raw, err := e.Tailnet.Status(ctx)
	now := e.now().UTC()
	var info config.TailnetInfo
	if err == nil {
		info, err = config.TailnetInfoFromStatus(raw)
	}
	if err != nil {
		e.mu.Lock()
		e.tailnet.LastPoll, e.tailnet.Error = now, err.Error()
		e.mu.Unlock()
		return out, err
	}
	e.tailnetMu.Lock()
	old, had := e.cachedTailnet()
	info.Interface = e.tailnetIface(ctx, old.Interface)
	out.Changed = !had || !sameTailnet(old, info)
	if out.Changed {
		e.saveTailnet(info)
	}
	e.tailnetMu.Unlock()
	// Decide about the auto-apply before the poll shows in status.get, so
	// a client that saw this poll knows it can no longer start an apply.
	st, serr := e.Store.Settings()
	if serr != nil {
		return out, serr
	}
	_, active := st.ActiveSubscription()
	autoApply := out.Changed && active && st.Tailscale.Coexist && *st.AutoApply && st.Drift == nil && e.appliedBefore()
	out.Tailnet = rpc.TailnetStatus{Peers: info.PeerShortNames, Suffix: info.MagicDNSSuffix, Interface: info.Interface, LastPoll: now}
	e.mu.Lock()
	e.tailnet = out.Tailnet
	e.mu.Unlock()
	out.Tailnet.Coexist = st.Tailscale.Coexist
	if out.Changed {
		e.publish(rpc.EventTailnetChanged, out.Tailnet)
		ap := rpc.ApplyParams{Reason: "tailnet changed", ReasonCode: store.ReasonTailnet}
		if autoApply && !e.adopting.Load() && !e.holdAutoApply(ctx, ap) {
			r, _ := e.Apply(ctx, ap)
			out.Apply = r
		}
	}
	return out, nil
}

// appliedBefore: Twinnel has written a config before (any history entry).
func (e *Engine) appliedBefore() bool {
	_, ok, err := e.Store.LatestHistory()
	return ok && err == nil
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
