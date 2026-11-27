package scm

import (
	"context"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

// HostKeeper keeps the service host up to date with the helper that runs
// it (the helper's side of Host: it refreshes at start, and reports the
// host in status.get).
type HostKeeper struct {
	Src      string          // the running helper's executable
	Labels   func() []string // the services that may run under the host
	Reader   HostReader
	Revision int // svchost.Revision
	// Exec and Files carry the refresh out; Files nil: read only (a
	// development helper never replaces the installed host).
	Exec  service.Executor
	Files service.Files
	// TTL is how long State reuses a read (it runs the host twice).
	// Default 5 min.
	TTL time.Duration
	Now func() time.Time

	mu   sync.Mutex
	last *Host
	at   time.Time
	err  string
}

func (k *HostKeeper) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func (k *HostKeeper) read(ctx context.Context) Host {
	var labels []string
	if k.Labels != nil {
		labels = k.Labels()
	}
	return k.Reader.Read(ctx, "", k.Src, labels)
}

// Refresh brings the host up to date (Host.RefreshPlan) and reads it
// again. A nil result: nothing was needed (copies left aside may have
// been deleted).
func (k *HostKeeper) Refresh(ctx context.Context) (*rpc.ServiceHostRefresh, error) {
	h := k.read(ctx)
	stale := h.Stale(k.Revision)
	steps := h.RefreshPlan(k.Src, k.Revision, k.now())
	if len(steps) == 0 || k.Files == nil || (!stale && len(h.Aside) == 0) {
		k.store(h, "")
		return nil, nil
	}
	err := service.Apply(ctx, steps, k.Exec, k.Files)
	after := k.read(ctx)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	k.store(after, msg)
	if !stale {
		return nil, err // only old copies were deleted
	}
	return &rpc.ServiceHostRefresh{From: h.Version, To: after.Version, Replaced: !h.Same, Restarted: h.Restarts(k.Revision), Error: msg}, err
}

func (k *HostKeeper) store(h Host, err string) {
	k.mu.Lock()
	k.last, k.at, k.err = &h, k.now(), err
	k.mu.Unlock()
}

// State is the host for status.get (nil when it is not installed), read
// again at most every TTL.
func (k *HostKeeper) State(ctx context.Context) *rpc.ServiceHost {
	ttl := k.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	k.mu.Lock()
	h, at, msg := k.last, k.at, k.err
	k.mu.Unlock()
	if h == nil || k.now().Sub(at) > ttl {
		r := k.read(ctx)
		h = &r
		k.mu.Lock()
		k.last, k.at = h, k.now()
		k.mu.Unlock()
	}
	if !h.Present {
		return nil
	}
	return &rpc.ServiceHost{Path: h.Path, Version: h.Version, Stale: h.Stale(k.Revision), Error: msg}
}
