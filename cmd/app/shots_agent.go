package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// agentShots (TWINNEL_SHOTS_FLOW=agent) captures 设置 → 本地 Agent: off (no
// activity), on with operate and recent actions and the command line tool
// installed, and the tool's failure row. The switches and the activity
// are set in memory over the scratch helper's settings; nothing is
// written. Run with TWINNEL_LANG=zh|en and TWINNEL_THEME=light|dark.
func (a *app) agentShots(save func(*mygo.Window, string)) {
	now := time.Now()
	acts := []rpc.AgentAction{
		{Time: now.Add(-2 * time.Minute), Client: "claude-code 2.1", Tool: "select_node", Args: "group=Proxy node=JP 1", Tier: rpc.TierOperate, OK: true},
		{Time: now.Add(-9 * time.Minute), Client: "claude-code 2.1", Tool: "set_mode", Args: "mode=rule", Tier: rpc.TierOperate, OK: true},
		{Time: now.Add(-40 * time.Minute), Client: "codex 0.9", Tool: "layer_put", Args: "content=<58 bytes> id=agent-direct", Tier: rpc.TierConfigure, OK: false, Error: "configure tools are off"},
		{Time: now.Add(-3 * time.Hour), Client: "cursor 1.4", Tool: "delay_test", Args: "group=Proxy", Tier: rpc.TierOperate, OK: true},
	}
	mygo.RunOnMain(func() {
		a.win.SetSize(a.winWidth(), 1900)
		old := a.page
		a.page = pageSettings
		a.pageChanged(old)
	})
	time.Sleep(1500 * time.Millisecond) // the helper's settings load
	link, linkErr := shotCLILink(a.shotGOOS()), "symlink /usr/local/bin/twinnel: permission denied"
	if a.shotGOOS() == "windows" {
		linkErr = "open " + link + ": Access is denied."
	}
	for _, st := range []struct {
		name  string
		agent rpc.AgentSettings
		acts  []rpc.AgentAction
		links []string
		err   string
		cli   int
	}{
		{"settings-agent-off", rpc.AgentSettings{Operate: true}, []rpc.AgentAction{}, nil, "", 0},
		{"settings-agent-on", rpc.AgentSettings{MCP: true, Operate: true}, acts, []string{link}, "", 0},
		{"settings-agent-json", rpc.AgentSettings{MCP: true, Operate: true, Configure: true}, acts[:2], nil,
			linkErr, 3},
	} {
		apply := func() {
			if a.hd.settings == nil {
				a.hd.settings = &rpc.Settings{}
			}
			a.hd.settings.Agent = st.agent
			v := &a.agent
			v.activity, v.actLoaded, v.actErr, v.actSeq = st.acts, true, "", a.hd.lastEvent.Seq
			v.shotLinks, v.cliErr, v.client = st.links, st.err, st.cli
			a.win.Invalidate()
		}
		mygo.RunOnMain(apply)
		time.Sleep(700 * time.Millisecond)
		mygo.RunOnMain(apply) // a helper reload may have replaced the settings meanwhile
		time.Sleep(400 * time.Millisecond)
		save(a.win, st.name)
	}
}
