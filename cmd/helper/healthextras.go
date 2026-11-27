package main

import (
	"context"
	"runtime"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/health"
)

// healthExtras gives the checks the helper's own facts: the binaries it
// installed (binary-integrity), the competing clients' autostarts
// (competitor-autostart), and for the egress test the configured echo
// endpoints and the DNS servers from before Twinnel (leak-guard-egress).
func healthExtras(e *engine.Engine, s *health.Settings) {
	s.Binaries = e.HealthBinaries(runtime.GOOS)
	s.DirectDNS = e.EgressDirectDNS()
	if st, err := e.Store.Settings(); err == nil {
		s.EgressURLs = st.HealthEgressURLs
	}
	if e.Autostarts != nil {
		s.Autostarts = func(ctx context.Context) ([]autostart.Entry, error) { return e.AutostartFind(ctx) }
	}
}
