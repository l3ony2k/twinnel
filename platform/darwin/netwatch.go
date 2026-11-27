//go:build darwin

package darwin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
)

// NetFingerprint is a cheap summary of the network state. Two equal
// fingerprints mean nothing a health check cares about has changed.
type NetFingerprint struct {
	Route      string // default routes: interface and gateway (IPv4, IPv6)
	Interfaces string // interfaces with a routable address: name, carrier, addresses
	DNS        string // resolver configuration from `scutil --dns`
}

// Diff names the parts that differ ("route", "interfaces", "dns").
func (f NetFingerprint) Diff(o NetFingerprint) []string {
	var out []string
	if f.Route != o.Route {
		out = append(out, "route")
	}
	if f.Interfaces != o.Interfaces {
		out = append(out, "interfaces")
	}
	if f.DNS != o.DNS {
		out = append(out, "dns")
	}
	return out
}

// TakeFingerprint runs three read-only commands: `route -n get default`
// (IPv4 and IPv6), `ifconfig` and `scutil --dns`. A failing command
// contributes its error text, so "no default route" is a state too.
func TakeFingerprint(ctx context.Context, run CommandRunner) NetFingerprint {
	var fp NetFingerprint
	var routes []string
	for _, args := range [][]string{{"-n", "get", "default"}, {"-n", "get", "-inet6", "default"}} {
		out, err := run(ctx, "/sbin/route", args...)
		if err != nil {
			routes = append(routes, "none")
			continue
		}
		r := ParseRouteGet(string(out))
		if r.Interface == "" {
			routes = append(routes, "none")
			continue
		}
		routes = append(routes, r.Interface+" via "+r.Gateway)
	}
	fp.Route = strings.Join(routes, "; ")

	if out, err := run(ctx, "/sbin/ifconfig"); err != nil {
		fp.Interfaces = "error: " + err.Error()
	} else {
		fp.Interfaces = interfacesKey(ParseIfconfig(string(out)))
	}
	if out, err := run(ctx, "/usr/sbin/scutil", "--dns"); err != nil {
		fp.DNS = "error: " + err.Error()
	} else {
		fp.DNS = resolversKey(ParseScutilDNS(string(out)))
	}
	return fp
}

// interfacesKey keeps interfaces with at least one address that is not
// link-local: awdl0, llw0 and friends flap all the time but only ever have
// fe80:: addresses.
func interfacesKey(ifs []health.Interface) string {
	var parts []string
	for _, i := range ifs {
		var addrs []string
		for _, a := range i.Addrs {
			if a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
				continue
			}
			addrs = append(addrs, a.String())
		}
		if len(addrs) == 0 {
			continue
		}
		sort.Strings(addrs)
		parts = append(parts, fmt.Sprintf("%s up=%t active=%t %s", i.Name, i.Up, i.Active, strings.Join(addrs, ",")))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func resolversKey(rs []health.Resolver) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%s scoped=%t if=%s ns=%s search=%s",
			r.Domain, r.Scoped, r.IfIndex, strings.Join(r.Nameservers, ","), strings.Join(r.SearchDomains, ",")))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// NetWatcher polls the network fingerprint and sends a reason on C when it
// changed and then stayed the same for Settle. It needs no cgo and no
// privileges. Plug C into health.Scheduler.Triggers.
type NetWatcher struct {
	// Poll is the polling interval; default 3s.
	Poll time.Duration
	// Settle is how long a new state must hold before it is reported;
	// default one Poll. A state that flips back within Settle is not
	// reported.
	Settle time.Duration
	// Run executes commands; default ExecRunner(5s).
	Run CommandRunner
	// Take overrides TakeFingerprint (tests).
	Take  func(ctx context.Context) NetFingerprint
	Clock health.Clock // default health.RealClock

	c chan string
}

// NewNetWatcher returns a watcher with defaults; call Watch to start it.
func NewNetWatcher() *NetWatcher { return &NetWatcher{} }

// C returns the channel receiving change reasons such as
// "network: route, dns". Sends never block; a reader that is behind gets
// one pending reason.
func (w *NetWatcher) C() <-chan string {
	if w.c == nil {
		w.c = make(chan string, 1)
	}
	return w.c
}

// Watch polls until ctx is done. The first fingerprint is the baseline and
// is not reported.
func (w *NetWatcher) Watch(ctx context.Context) error {
	w.C()
	poll := w.Poll
	if poll <= 0 {
		poll = 3 * time.Second
	}
	settle := w.Settle
	if settle <= 0 {
		settle = poll
	}
	clock := w.Clock
	if clock == nil {
		clock = health.RealClock{}
	}
	take := w.Take
	if take == nil {
		run := w.Run
		if run == nil {
			run = ExecRunner(5 * time.Second)
		}
		take = func(ctx context.Context) NetFingerprint { return TakeFingerprint(ctx, run) }
	}

	reported := take(ctx)
	var pending *NetFingerprint
	var since time.Time
	t := clock.NewTimer(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C():
		}
		fp := take(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now := clock.Now()
		switch {
		case fp == reported:
			pending = nil // changed and changed back: nothing to report
		case pending == nil || fp != *pending:
			pending, since = &fp, now
		}
		if pending != nil && now.Sub(since) >= settle {
			reason := "network: " + strings.Join(pending.Diff(reported), ", ")
			reported, pending = *pending, nil
			select {
			case w.c <- reason:
			default:
			}
		}
		t.Reset(poll)
	}
}
