package main

// STUB — delete this file when feat/ui-controls (pending.go, states.go) is
// merged. It stands in for ui-controls' API so that feat/ui-proxies builds
// on its own: the same names and arguments, with today's behaviour (no
// optimistic state, the raw error as before).

import (
	"context"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
)

const (
	pendMode = "mode"
	pendTun  = "tun"
	pendTS   = "tailscale"
	pendExit = "exit"
)

type netService int

const (
	svcMihomo netService = iota
	svcTailscale
)

func nodeKey(group string) string { return "node:" + group }

func pendingValue[T comparable](_ *app, _ string, def T) T { return def }

func (a *app) selectNode(group, name string) {
	a.do(func(ctx context.Context) error { return a.mh.SelectProxy(ctx, group, name) })
}

func (a *app) controlError(*ui.Context, string) {}

func (a *app) unreachableState(c *ui.Context, _ netService, err string) {
	groupBox(c, c.Theme()).Children(func() { emptyState(c, iconError, i18n.T.Unreachable, err) })
}

func unreachableLine(c *ui.Context, _ netService, err string) {
	if err != "" {
		errorNote(c, err)
	}
}
