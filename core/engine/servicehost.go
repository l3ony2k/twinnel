package engine

import (
	"context"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// ServiceHostKeeper keeps the service host up to date with the helper
// (platform/windows/scm.HostKeeper).
type ServiceHostKeeper interface {
	// State is the host for status.get (nil: none installed).
	State(ctx context.Context) *rpc.ServiceHost
	// Refresh updates it when it is stale (nil: nothing needed).
	Refresh(ctx context.Context) (*rpc.ServiceHostRefresh, error)
}

// RefreshServiceHost brings the service host up to date, under the
// service lock (it may restart mihomo's service), and says so: in the log
// and as a service.changed event (op "host-refresh"). The helper runs it
// when it starts.
func (e *Engine) RefreshServiceHost(ctx context.Context) {
	if e.ServiceHost == nil {
		return
	}
	e.serviceMu.Lock()
	r, err := e.ServiceHost.Refresh(ctx)
	e.serviceMu.Unlock()
	switch {
	case r == nil && err != nil:
		e.log().Warn("service host: could not delete old copies", "err", err)
		return
	case r == nil:
		return
	case err != nil:
		e.log().Error("service host refresh failed", "from", r.From, "to", r.To, "restarted", r.Restarted, "err", err)
	case len(r.Restarted) > 0:
		e.log().Warn("service host updated; restarted the services that ran an older host", "from", r.From, "to", r.To, "restarted", r.Restarted)
	default:
		e.log().Info("service host updated; running services use it from their next restart", "from", r.From, "to", r.To)
	}
	data := map[string]any{"name": "mihomo", "op": "host-refresh", "host": r}
	e.publish(rpc.EventServiceChanged, data)
}
