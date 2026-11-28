package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/fixture"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// layoutSnapshot is a synthetic, healthy state: mihomo running with a
// group of flagged nodes, Tailscale up with a few devices, a health
// report. No real data.
func layoutSnapshot(now time.Time) status.Snapshot {
	members := []status.Member{
		{Name: "\U0001F1FA\U0001F1F8 NODE-US-01", Type: "Vless", Alive: true, Delay: 160, Tested: true},
		{Name: "\U0001F1ED\U0001F1F0 HK-01 · a node with a rather long name for the tiles", Type: "Trojan", Alive: true, Delay: 42, Tested: true},
		{Name: "\U0001F1EF\U0001F1F5 JP-02", Type: "Shadowsocks", Alive: true, Delay: 88, Tested: true},
		{Name: "DIRECT", Type: "Direct", Builtin: true},
	}
	g := status.Group{Name: "PROXY", Type: "Selector", Now: members[0].Name, Chain: []string{members[0].Name},
		Node: members[0].Name, Delay: 160, Tested: true, DelayAt: now, Members: members}
	auto := status.Group{Name: "\U0001F1FA\U0001F1F8 Auto", Type: "URLTest", Now: members[2].Name, Chain: []string{members[2].Name},
		Node: members[2].Name, Delay: 88, Tested: true, Members: members[:3]}
	s := status.Snapshot{Time: now, Level: status.LevelOK, Loaded: true, WritesAllowed: true}
	s.Mihomo = status.Mihomo{Reachable: true, Version: "v1.19.0", Groups: []status.Group{g, auto},
		Config:      mihomo.Config{Mode: "rule", MixedPort: 7890, Port: 7891, SocksPort: 7892, Tun: mihomo.Tun{Enable: true, Stack: "gvisor"}},
		Connections: 12, Memory: 48 << 20, Updated: now,
		Process: &status.Process{PID: 4242, RSS: 52 << 20, CPU: 1.5, Started: now.Add(-5 * time.Hour)}}
	s.Mihomo.Primary = &s.Mihomo.Groups[0]
	s.Mihomo.Traffic = status.TrafficSample{Traffic: mihomo.Traffic{Up: 2048, Down: 81920, UpTotal: 1 << 20, DownTotal: 9 << 20}, Time: now}
	peers := []tailscale.Peer{{Name: "laptop", Online: true}, {Name: "nas", Online: true}, {Name: "phone"}}
	s.Tailscale = status.Tailscale{Reachable: true, Updated: now, State: tailscale.State{BackendState: "Running",
		Version: "1.90.0", Tailnet: "example.ts.net", MagicDNSSuffix: "example.ts.net", User: "user@example.com",
		Self: tailscale.Peer{Name: "this-pc", Online: true}, Peers: peers}}
	var checks []health.CheckReport
	for i := range 6 {
		checks = append(checks, health.CheckReport{ID: fmt.Sprintf("check-%d", i), Title: fmt.Sprintf("检查 %d", i),
			Result: health.Result{Status: health.StatusOK, Summary: "一切正常，没有问题需要处理"}})
	}
	checks = append(checks, health.CheckReport{ID: health.LeakEgress{}.ID(), Title: "出口 IP 实测",
		Result: health.Result{Status: health.StatusOK, Summary: "敏感策略组的出口 IP 和本机直连出口不同：PROXY"}})
	s.Health.Report = &health.Report{Started: now.Add(-time.Minute), Checks: checks}
	return s
}

// layoutApp is an app that draws layoutSnapshot without a monitor
// loop, a helper or a controller.
func layoutApp(t *testing.T) (*app, status.Snapshot) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 11, 28, 7, 42, 0, 0, time.Local)
	snap := layoutSnapshot(now)
	a := &app{page: pageOverview, groupsOpen: map[string]bool{}, tileCols: map[string]int{},
		pageScroll: map[string]*ui.ScrollState{}, healthOpen: map[string]bool{}, shotMode: true}
	a.mh, _ = mihomo.New(mihomo.Options{Address: "127.0.0.1:1"})
	a.mon = status.NewMonitor(status.Options{})
	a.shotSnap = func(s *status.Snapshot) { *s = snap }
	a.onMain = func(f func()) { f() }
	a.probes = &shotProbes{machine: fixture.Synthetic()} // never this machine
	ns := notificationsSupported
	notificationsSupported = func() bool { return true } // no app main loop under a Tester
	t.Cleanup(func() { notificationsSupported = ns })
	return a, snap
}

// titleBarFor is the window controls' room each platform's window
// reports (plat.titleBar): the traffic lights on macOS, the caption
// buttons over a hidden title bar on Windows and Linux.
func titleBarFor(goos string) ui.TitleBar {
	switch goos {
	case "windows":
		return ui.TitleBar{Height: float32(plat.titleBarHeight), Right: 3 * 46}
	case "linux":
		return ui.TitleBar{Height: float32(plat.titleBarHeight), Right: 90}
	}
	return ui.TitleBar{Height: 52, Left: 78, Bottom: 33}
}

// Every page, on every platform's shell, at window widths from small to
// a 4K screen's and the display scales of Windows (100–200%): the page
// column sits inside the window, centered in the pane right of the
// sidebar; no text runs past the window's right edge; and the layout in
// DIPs is the same at every scale (the column's math never sees pixels).
func TestPageLayoutAllWidthsAndScales(t *testing.T) {
	old := plat
	defer func() { plat = old; resetFlags() }()
	widths := []int{900, 1280, 1600, 2575}
	scales := []float32{1, 1.25, 1.5, 2}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		plat = platformFor(goos)
		resetFlags()
		a, _ := layoutApp(t)
		a.hc = nil
		tr := ui.NewTester(func(c *ui.Context) { a.mainView(c) }, widths[0], 900)
		tr.SetTitleBar(titleBarFor(goos))
		ref := map[string]ui.Rect{} // page@width → the column at scale 1
		for _, scale := range scales {
			tr.SetScale(scale)
			for _, w := range widths {
				tr.SetSize(w, 900)
				for _, p := range allPages {
					if p == pageWizard {
						continue // the whole window, no sidebar: wizard_test.go
					}
					old := a.page
					a.page = p
					a.pageChanged(old)
					tr.Frame()
					tr.Frame() // centered on the width the first frame measured
					where := fmt.Sprintf("%s %s w=%d scale=%v", goos, p, w, scale)
					pane, col := a.paneBox, a.colBox
					W := float32(w)
					if pane.W <= 0 || pane.X+pane.W > W+0.5 {
						t.Errorf("%s: pane %+v outside the window", where, pane)
					}
					for _, txt := range tr.Texts() {
						if r, ok := tr.Find(txt); ok && r.W > 0 && r.X+r.W > W+0.5 {
							t.Errorf("%s: %q ends at %.1f, past the window (%d)", where, txt, r.X+r.W, w)
						}
					}
					if p == pageLogs {
						continue // the logs fill the pane: no column
					}
					if col.W <= 0 {
						t.Errorf("%s: no page column", where)
						continue
					}
					if col.X < pane.X-0.5 || col.X+col.W > pane.X+pane.W+0.5 {
						t.Errorf("%s: column %+v outside the pane %+v", where, col, pane)
					}
					if col.W > pageMaxWidth(p)+0.5 {
						t.Errorf("%s: column %.1f wider than its max %v", where, col.W, pageMaxWidth(p))
					}
					left, right := col.X-pane.X, pane.X+pane.W-(col.X+col.W)
					if math.Abs(float64(left-right)) > 1 {
						t.Errorf("%s: column not centered in the pane: left %.1f, right %.1f", where, left, right)
					}
					key := fmt.Sprintf("%s@%d", p, w)
					if r0, ok := ref[key]; !ok {
						ref[key] = col
					} else if math.Abs(float64(r0.X-col.X)) > 1 || math.Abs(float64(r0.W-col.W)) > 1 {
						t.Errorf("%s: column %+v differs from scale 1's %+v", where, col, r0)
					}
				}
			}
		}
	}
}
