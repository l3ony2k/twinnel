package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// readmeShots (TWINNEL_SHOTS_FLOW=readme) captures the README's pictures
// against the hermetic stack scripts/readme-shots.sh seeds: 总览 with the
// egress result, 策略组, Tailscale 设备, 健康检查, 订阅与覆写 with a
// preview of a pending layer change, 历史 with a diff, 设置 → 本地 Agent,
// the flyout and the menu bar icon states. Nothing is written: the
// egress test is the fixture's probe and the agent switches are set in
// memory. Run once per TWINNEL_LANG and TWINNEL_THEME.
func (a *app) readmeShots(save func(*mygo.Window, string)) {
	const w, h = 1100, 720
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	settle := func(d time.Duration) {
		deadline := time.Now().Add(20 * time.Second)
		time.Sleep(d)
		for time.Now().Before(deadline) {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" || a.hd.loading })
			if !busy {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		time.Sleep(400 * time.Millisecond)
	}
	page := func(p string, height int) {
		on(func() {
			a.win.SetSize(w, height)
			old := a.page
			a.page = p
			a.pageChanged(old)
			if st := a.pageScroll[p]; st != nil {
				st.Y = 0
			}
		})
		settle(1200 * time.Millisecond)
	}
	// A headless capture lays out with the size the frame before saw:
	// save twice, the second one counts.
	shot := func(name string) {
		save(a.win, name)
		time.Sleep(300 * time.Millisecond)
		save(a.win, name)
	}

	// 总览: the recent events, then the egress test so the 出口 tile shows
	// an address.
	for i := 0; i < 20; i++ {
		n := 0
		mygo.RunOnMain(func() { a.backfillEvents(); n = len(a.events.evs) })
		if n > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	page(pageOverview, 672) // ends under the traffic chart
	probe := a.egressProbe
	a.egressProbe = func(ctx context.Context) *health.CheckReport {
		cr := probe(ctx)
		if cr != nil && cr.Egress != nil {
			if p := a.mon.Snapshot().Mihomo.Primary; p != nil {
				cr.Egress.Node = p.Node // the fixture's chain, said to be the shown exit
			}
		}
		return cr
	}
	on(func() {
		if p := a.mon.Snapshot().Mihomo.Primary; p != nil && !p.Tested {
			a.testNode(p.Node)
		}
		a.testEgress()
	})
	time.Sleep(2500 * time.Millisecond)
	shot("overview")

	page(pageProxies, h)
	shot("proxies")
	page(pageTailscale, h)
	shot("tailscale")
	page(pageHealth, h)
	shot("health")
	if os.Getenv("TWINNEL_README_PART") == "health" { // the warning variant: health only
		return
	}

	// 订阅与覆写: the pending layer change, previewed.
	page(pageConfig, 780)
	on(func() { a.hd.cfg.sel, a.hd.cfg.isNew, a.hd.cfg.ed = "", false, nil })
	on(func() { a.runPreview(a.mon.Snapshot()) })
	settle(2 * time.Second)
	shot("config-preview")

	// 历史: the entry before the newest, against the live config.
	page(pageHistory, h)
	var prev string
	mygo.RunOnMain(func() {
		if len(a.hd.history) > 1 {
			prev = a.hd.history[1].ID // newest first
		}
	})
	if prev != "" {
		on(func() { a.hd.hist.sel = prev; a.historyDiff(a.mon.Snapshot(), prev) })
		settle(time.Second)
	}
	shot("history")

	// 设置 → 本地 Agent, on, with recent actions.
	now := time.Now()
	acts := []rpc.AgentAction{
		{Time: now.Add(-2 * time.Minute), Client: "claude-code", Tool: "health_run", Tier: rpc.TierRead, OK: true},
		{Time: now.Add(-6 * time.Minute), Client: "claude-code", Tool: "select_node", Args: "group=OpenAI node=" + a.readmeNode(), Tier: rpc.TierOperate, OK: true},
		{Time: now.Add(-50 * time.Minute), Client: "codex", Tool: "layer_put", Args: "content=<58 bytes> id=agent-direct", Tier: rpc.TierConfigure, OK: false, Error: "configure tools are off"},
	}
	page(pageSettings, 560)
	agentOn := func() {
		if a.hd.settings == nil {
			a.hd.settings = &rpc.Settings{}
		}
		a.hd.settings.Agent = rpc.AgentSettings{MCP: true, Operate: true}
		v := &a.agent
		v.activity, v.actLoaded, v.actErr, v.actSeq = acts, true, "", a.hd.lastEvent.Seq
		v.shotLinks = []string{shotCLILink(a.shotGOOS())}
		a.shotSection = "agent"
	}
	on(agentOn)
	time.Sleep(700 * time.Millisecond)
	on(agentOn) // a helper reload may have replaced the settings meanwhile
	time.Sleep(400 * time.Millisecond)
	shot("settings-agent")
	on(func() { a.shotSection = "" })

	// The flyout, from the overview (its delay kept fresh there).
	page(pageOverview, 900)
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		save(panel, "flyout")
		time.Sleep(300 * time.Millisecond)
		save(panel, "flyout")
	}

	// The menu bar icon's states, one PNG each; the script makes a strip.
	dir := os.Getenv("TWINNEL_SHOTS")
	for _, v := range []status.TrayIcon{status.TrayOK, status.TrayWarn, status.TrayError, status.TrayUnknown,
		status.TrayTSDown, status.TrayMode, status.TrayNoHelper} {
		if err := os.WriteFile(filepath.Join(dir, "tray-"+string(v)+".png"), trayIcon(v), 0o644); err != nil {
			log.Print(err)
		}
	}
}

// readmeNode is a node of the OpenAI group, for the agent's sample action.
func (a *app) readmeNode() string {
	var n string
	mygo.RunOnMain(func() {
		for _, g := range a.mon.Snapshot().Mihomo.Groups {
			if g.Name == "OpenAI" {
				n = g.Now
			}
		}
	})
	if n == "" {
		n = "US West 01"
	}
	return n
}
