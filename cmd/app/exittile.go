package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// 总览's first tile, 出口: where traffic leaves and how well. The delay of
// the node the primary group resolves to (big, with its age), the node,
// and the public IP mihomo's traffic leaves from, as the egress test last
// saw it. The delay is kept fresh while the overview or the flyout shows
// (status.Monitor.TestExit); the IP comes from the egress test, which the
// tile offers when there is no result.

// egressKind is what the tile's IP line shows.
type egressKind int

const (
	egressNone    egressKind = iota // no result: offer 检测出口 IP
	egressRunning                   // the test runs
	egressKnown                     // the IP through mihomo, with its time
	egressFailed                    // the last run failed or measured nothing
	egressNoTest                    // the test can't run here: nothing
)

// exitView is the 出口 tile's content, worked out from the snapshot so it
// can be tested without drawing.
type exitView struct {
	figure   string // the delay, "未测", or "—"
	known    bool   // figure is a result (colour it)
	ms       int
	age      string // "5 分钟前"; "" when unknown
	testing  bool   // the node's delay test runs
	offer    bool   // untested: offer 测速 inline
	node     string // the node carrying the traffic; Unreachable when down
	route    string // group → … → node, for the tooltip
	egress   egressKind
	ip       string // the IP through mihomo (egressKnown)
	ipAt     string // when it was measured: "21:32"
	ipDirect string // the direct IP, for the tooltip, when known
	why      string // egressFailed: the reason in words
}

// exitTileView works out the tile. eg is the newest egress result (nil:
// none), at its time; running/failed are the app's own run of the test.
func exitTileView(s status.Snapshot, testing bool, eg *health.CheckReport, at time.Time, canTest, running bool, failed string) exitView {
	T := i18n.T
	v := exitView{figure: status.NoDelay}
	m := s.Mihomo
	if !m.Reachable {
		v.node = T.Unreachable
		v.egress = egressNoTest
		return v
	}
	p := m.Primary
	if p == nil {
		v.egress = egressNoTest
		return v
	}
	v.node = p.Node
	v.route = routeText(p)
	v.testing = testing
	switch {
	case p.Builtin:
	case p.Tested:
		v.figure, v.known, v.ms = p.DelayText(), true, p.Delay
		if !p.DelayAt.IsZero() {
			v.age = status.Ago(s.Time, p.DelayAt)
		}
	default:
		v.figure = T.DelayUnknown
		v.offer = !testing
	}
	v.egress = egressNone
	if !canTest {
		v.egress = egressNoTest
	}
	var ips *health.EgressIPs
	if eg != nil {
		ips = eg.Egress
	}
	switch {
	case running:
		v.egress = egressRunning
	case failed != "":
		v.egress, v.why = egressFailed, failed
	case ips != nil && ips.Proxied.IsValid() && (ips.Node == "" || ips.Node == p.Node):
		v.egress, v.ip = egressKnown, ips.Proxied.String()
		if !at.IsZero() {
			v.ipAt = eventTime(s.Time, at)
		}
		if ips.Direct.IsValid() {
			v.ipDirect = ips.Direct.String()
		}
	case ips != nil && ips.Node != "" && ips.Node != p.Node:
		// Measured through another node: that IP is not this exit's.
	case eg != nil && ips == nil && eg.Status != health.StatusOK && canTest:
		// A result without addresses: the test ran and failed (its
		// summary says why), or a helper too old to hand them over.
		if eg.Status == health.StatusWarn || eg.Status == health.StatusSkip {
			v.egress, v.why = egressFailed, eg.Summary
		}
	}
	return v
}

// routeText is a group's route to its node: "PROXY → Auto → node".
func routeText(p *status.Group) string {
	out := p.Name
	for _, n := range p.Chain {
		out += " → " + n
	}
	return out
}

// egressNow is the newest egress result the app has, with its time: the
// in-process run's (no helper), else the egress check of the health
// report unless that only says the test is off.
func (a *app) egressNow(s status.Snapshot) (*health.CheckReport, time.Time) {
	if r := a.egress.result; r != nil && !r.Unsupported {
		return r, r.At
	}
	rep := s.Health.Report
	if rep == nil {
		return nil, time.Time{}
	}
	for i := range rep.Checks {
		cr := &rep.Checks[i]
		if cr.ID != (health.LeakEgress{}).ID() || egressOff(*cr) || cr.Unsupported {
			continue
		}
		at := cr.At
		if at.IsZero() {
			at = rep.Started
		}
		return cr, at
	}
	return nil, time.Time{}
}

// exitTile draws the 出口 tile; a click opens 策略组.
func (a *app) exitTile(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	p := pal(t)
	T := i18n.T
	testing := false
	if pr := s.Mihomo.Primary; pr != nil && s.Mihomo.Reachable && a.mon != nil {
		testing = a.mon.Testing(pr.Node)
	}
	eg, at := a.egressNow(s)
	v := exitTileView(s, testing, eg, at, a.canTestEgress(s), a.egress.running, a.egress.why)
	b := ui.Column(c.Key("tile-exit")).Gap(4).Padding(12, groupPadX).Radius(plat.groupRadius).
		Background(p.group).Border(1, p.groupLine).MinWidth(0).
		Role(ui.RoleButton).Focusable().Label(T.OvTileExit + " " + v.figure)
	if b.Hovered() {
		b.Border(1, t.Text.Alpha(0.18))
	}
	if b.Clicked() {
		a.openPage(pageProxies)
	}
	b.Children(func() {
		small(c, T.OvTileExit).SingleLine()
		ui.Row(c).Gap(6).AlignItems(ui.Baseline).MinWidth(0).Children(func() {
			fig := num(ui.Text(c, v.figure)).FontSize(plat.figureSize).FontWeight(500).SingleLine().Shrink(0)
			switch {
			case v.known:
				fig.TextColor(delayColor(t, v.ms, true))
			case v.figure != status.NoDelay:
				fig.TextColor(t.TextMuted)
			}
			if v.age != "" {
				num(small(c, v.age)).SingleLine().Shrink(1).MinWidth(0)
			}
			ui.Spacer(c)
			switch {
			case v.testing:
				spinnerIcon(c).AlignSelf(ui.Center)
			case v.offer:
				if busyButton(c.Key("exit-test"), T.TestNodeDelay, false, false, false).Height(22).AlignSelf(ui.Center).Clicked() {
					a.testNode(s.Mihomo.Primary.Node)
				}
			}
		})
		node := v.node
		if node == "" {
			node = " " // keep the tile's height
		}
		sub := small(c, node).SingleLine().TruncateMiddle()
		if v.route != "" {
			sub.Tooltip(v.route)
		}
		a.exitIPLine(c, v)
	})
}

// exitIPLine is the tile's last line: the IP mihomo's traffic leaves from
// and when it was measured, or the test to learn it.
func (a *app) exitIPLine(c *ui.Context, v exitView) {
	T := i18n.T
	t := c.Theme()
	ui.Row(c.Key("exit-ip")).Gap(6).AlignItems(ui.Center).MinHeight(22).MinWidth(0).Children(func() {
		switch v.egress {
		case egressKnown:
			ip := monoText(c, v.ip).SingleLine().Shrink(1).MinWidth(0).Selectable()
			if v.ipDirect != "" {
				ip.Tooltip(T.OvEgressDirect + " " + v.ipDirect)
			}
			if v.ipAt != "" {
				num(small(c, v.ipAt)).SingleLine().Shrink(0)
			}
		case egressRunning:
			busyButton(c.Key("egress-check"), T.OvEgressCheck, false, true, true).Height(22)
		case egressFailed:
			ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(t.Warning).Shrink(0).AccessHidden(true)
			small(c, T.EgressFailed).SingleLine().Shrink(1).MinWidth(0).Tooltip(v.why)
			ui.Spacer(c)
			if iconButton(c.Key("egress-retry"), iconRefresh, T.Retry, false).Clicked() {
				a.testEgress()
			}
		case egressNone:
			if busyButton(c.Key("egress-check"), T.OvEgressCheck, false, false, false).Height(22).Clicked() {
				a.testEgress()
			}
		}
	})
}

// keepExitFresh tests the exit's delay when its last result is stale
// (status.Monitor.TestExit throttles it to once per ExitFresh). The
// overview and the flyout call it as they draw, which they do only while
// they show; a monitor change redraws them every few seconds.
func (a *app) keepExitFresh(s status.Snapshot) {
	if a.mon == nil || !s.Mihomo.Reachable || a.shotMode {
		return // shots test only when their flow says (shots_exit.go)
	}
	a.mon.TestExit()
}
