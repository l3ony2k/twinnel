package engine

// The DNS watchdog (core/dnswatch) in the helper: once dns.point made
// mihomo the system's resolver, a mihomo that stops, hangs or
// crash-loops would cut name resolution for the whole machine. The
// watchdog writes the backed-up DNS back meanwhile and points it at mihomo
// again once mihomo is healthy.

import (
	"context"
	"net"
	"slices"
	"sync"

	"github.com/l3ony2k/twinnel/core/dnswatch"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// dnsWatchState is the engine's watchdog, made on first use.
type dnsWatchState struct {
	once sync.Once
	w    *dnswatch.Watchdog
	// mu orders dns.point / dns.restore with the watchdog's changes.
	mu sync.Mutex
}

// dnsWatchdog returns the watchdog, nil where the platform cannot set the
// system DNS.
func (e *Engine) dnsWatchdog() *dnswatch.Watchdog {
	if e.SystemDNS == nil {
		return nil
	}
	e.dnsWatch.once.Do(func() {
		m := e.dnsManager()
		e.dnsWatch.w = &dnswatch.Watchdog{
			Config: e.DNSWatchConfig,
			DNS:    e.SystemDNS,
			Backup: m.ReadBackup,
			Target: e.dnsTarget,
			Probe: func(ctx context.Context) error {
				return e.probeDNS(ctx, net.JoinHostPort(e.dnsTarget()[0], "53"))
			},
			Service: e.dnsWatchService,
			Busy:    e.dnsWatchBusy,
			Lock:    &e.dnsWatch.mu,
			Path:    e.Store.Path("dns-watchdog.json"),
			Now:     e.now,
			Log:     e.log(),
			OnChange: func(st dnswatch.State) {
				e.publish(rpc.EventDNSWatch, toDNSWatch(st))
				e.triggerHealth("dns watchdog")
			},
		}
	})
	return e.dnsWatch.w
}

// dnsWatchService reads mihomo's service for the watchdog.
func (e *Engine) dnsWatchService(ctx context.Context) (dnswatch.Service, error) {
	if e.Services == nil {
		return dnswatch.Service{}, nil
	}
	s, err := e.ServiceStatus(ctx, "mihomo")
	if err != nil || s.Error != "" || !s.Installed {
		// Unknown (an adopted service the manager cannot read, or none):
		// the watchdog then goes by the DNS answers alone.
		return dnswatch.Service{}, nil
	}
	return dnswatch.Service{Known: true, Running: s.Running, PID: s.PID, Runs: s.Runs}, nil
}

// dnsWatchBusy: a service operation, a harden or a takeover runs, so
// mihomo not answering is expected.
func (e *Engine) dnsWatchBusy() bool {
	if e.adopting.Load() {
		return true
	}
	if !e.serviceMu.TryLock() {
		return true
	}
	e.serviceMu.Unlock()
	return false
}

func toDNSWatch(st dnswatch.State) *rpc.DNSWatch {
	out := &rpc.DNSWatch{Phase: st.Phase, Cause: st.Cause, Since: st.Since,
		LeftAlone: slices.Clone(st.LeftAlone), LastRepoint: st.LastRepoint}
	if out.Phase == "" {
		out.Phase = rpc.DNSWatchOff
	}
	for _, s := range st.Restored {
		out.Services = append(out.Services, s.Name)
	}
	return out
}

// DNSWatch is the watchdog's state for status.get and dns.status (nil
// where there is none). Phase off while no backup exists.
func (e *Engine) DNSWatch() *rpc.DNSWatch {
	w := e.dnsWatchdog()
	if w == nil {
		return nil
	}
	st := w.State()
	if st.Phase == dnswatch.PhaseWatching || st.Phase == dnswatch.PhaseOff {
		// The phase follows the backup, also before the first tick.
		st.Phase = dnswatch.PhaseOff
		if fileExists(e.Store.Path("dns-backup.json")) {
			st.Phase = dnswatch.PhaseWatching
		}
	}
	return toDNSWatch(st)
}

// dnsWatchLoop runs the watchdog until ctx ends.
func (e *Engine) dnsWatchLoop(ctx context.Context) {
	if w := e.dnsWatchdog(); w != nil && !e.DNSWatchOff {
		w.Run(ctx)
	}
}
