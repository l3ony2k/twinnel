package health

import (
	"context"
	"github.com/l3ony2k/twinnel/core/foreign"
	"net/netip"
	"sync"
)

// Memo wraps an Env so each fact is fetched at most once, even when checks
// run concurrently. Create one per report run. Fetches use ctx (the run's
// context), not the context of the check that asked first, so one check
// timing out does not poison the shared fact for the others.
func Memo(ctx context.Context, env Env) Env {
	return &memoEnv{ctx: ctx, inner: env, cells: map[string]*cell{}}
}

type cell struct {
	once sync.Once
	val  any
	err  error
}

type memoEnv struct {
	ctx   context.Context
	inner Env
	mu    sync.Mutex
	cells map[string]*cell
}

func memo[T any](m *memoEnv, key string, f func() (T, error)) (T, error) {
	m.mu.Lock()
	c, ok := m.cells[key]
	if !ok {
		c = &cell{}
		m.cells[key] = c
	}
	m.mu.Unlock()
	c.once.Do(func() { c.val, c.err = f() })
	v, _ := c.val.(T)
	return v, c.err
}

func (m *memoEnv) Platform() string   { return m.inner.Platform() }
func (m *memoEnv) Settings() Settings { return m.inner.Settings() }

func (m *memoEnv) RouteGet(_ context.Context, dest string) (RouteLookup, error) {
	return memo(m, "route:"+dest, func() (RouteLookup, error) { return m.inner.RouteGet(m.ctx, dest) })
}
func (m *memoEnv) Routes(_ context.Context) ([]Route, error) {
	return memo(m, "routes", func() ([]Route, error) { return m.inner.Routes(m.ctx) })
}
func (m *memoEnv) Interfaces(_ context.Context) ([]Interface, error) {
	return memo(m, "ifaces", func() ([]Interface, error) { return m.inner.Interfaces(m.ctx) })
}
func (m *memoEnv) Resolvers(_ context.Context) ([]Resolver, error) {
	return memo(m, "resolvers", func() ([]Resolver, error) { return m.inner.Resolvers(m.ctx) })
}
func (m *memoEnv) NetworkServices(_ context.Context) ([]NetworkService, error) {
	return memo(m, "services", func() ([]NetworkService, error) { return m.inner.NetworkServices(m.ctx) })
}
func (m *memoEnv) VPNs(_ context.Context) ([]VPN, error) {
	return memo(m, "vpns", func() ([]VPN, error) { return m.inner.VPNs(m.ctx) })
}
func (m *memoEnv) Tailscale(_ context.Context) (*TailscaleStatus, error) {
	return memo(m, "tailscale", func() (*TailscaleStatus, error) { return m.inner.Tailscale(m.ctx) })
}
func (m *memoEnv) MihomoRuntime(_ context.Context) (*MihomoRuntime, error) {
	return memo(m, "mihomo-rt", func() (*MihomoRuntime, error) { return m.inner.MihomoRuntime(m.ctx) })
}
func (m *memoEnv) MihomoConfig(_ context.Context) (*MihomoConfig, error) {
	return memo(m, "mihomo-cfg", func() (*MihomoConfig, error) { return m.inner.MihomoConfig(m.ctx) })
}
func (m *memoEnv) LookupMihomo(_ context.Context, name string) (DNSAnswer, error) {
	return memo(m, "dns-mihomo:"+name, func() (DNSAnswer, error) { return m.inner.LookupMihomo(m.ctx, name) })
}
func (m *memoEnv) LookupSystem(_ context.Context, name string) (DNSAnswer, error) {
	return memo(m, "dns-system:"+name, func() (DNSAnswer, error) { return m.inner.LookupSystem(m.ctx, name) })
}

func (m *memoEnv) DirectEgress(_ context.Context, url string) (netip.Addr, error) {
	e, ok := m.inner.(EgressEnv)
	if !ok {
		return netip.Addr{}, ErrNotSupported
	}
	return memo(m, "egress-direct:"+url, func() (netip.Addr, error) { return e.DirectEgress(m.ctx, url) })
}

type proxiedEgress struct {
	addr  netip.Addr
	chain []string
}

func (m *memoEnv) ProxiedEgress(_ context.Context, url string) (netip.Addr, []string, error) {
	e, ok := m.inner.(EgressEnv)
	if !ok {
		return netip.Addr{}, nil, ErrNotSupported
	}
	v, err := memo(m, "egress-proxied:"+url, func() (proxiedEgress, error) {
		a, c, err := e.ProxiedEgress(m.ctx, url)
		return proxiedEgress{a, c}, err
	})
	return v.addr, v.chain, err
}

func (m *memoEnv) ControllerOwner(_ context.Context) (foreign.Owner, error) {
	oe, ok := m.inner.(OwnerEnv)
	if !ok {
		return foreign.Owner{}, ErrNotSupported
	}
	return memo(m, "controller-owner", func() (foreign.Owner, error) { return oe.ControllerOwner(m.ctx) })
}
