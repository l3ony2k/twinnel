package agent

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/tailscale"
)

// Operate tools: reversible switches of the running mihomo and Tailscale.
// Each result says what it was before, so the agent can put it back.

func (s *Server) registerOperate() {
	addTool(s, rpc.TierOperate, "set_mode", "Set proxy mode",
		`Switches mihomo's mode: rule (follow the rules; the normal mode), global (everything through the
GLOBAL group's node) or direct (nothing through a proxy). Takes effect at once for new connections.
Undo: set_mode with the previous mode in the result.`,
		toolOpts{idempotent: true}, s.setMode)

	addTool(s, rpc.TierOperate, "select_node", "Select node",
		`Chooses the node (or nested group) a Selector group uses. New connections through the group use it at
once; open ones keep their node. Undo: select_node with the previous node in the result.`,
		toolOpts{idempotent: true}, s.selectNode)

	addTool(s, rpc.TierOperate, "delay_test", "Delay test",
		`Measures delay (ms) through nodes: one node, or every member of a group (group). Sends one small
HTTP request per node to the test URL; changes no setting, but url-test and fallback groups may switch
to a faster node as a result.`,
		toolOpts{openWorld: true}, s.delayTest)

	addTool(s, rpc.TierOperate, "health_egress_run", "Test egress",
		`Runs the egress check (leak-guard-egress) once in the helper: two HTTPS GETs to a neutral IP-echo
address, one directly and one through mihomo, and compares the public IPs (the sensitive groups must not
leave from this network's IP). Changes no setting; the result stays in the health report for 24 hours.
Nothing to undo.`,
		toolOpts{openWorld: true}, s.healthEgressRun)

	addTool(s, rpc.TierOperate, "set_tun", "Turn TUN on or off",
		`Turns mihomo's TUN (system-wide capture of traffic) on or off. Off: only apps that use the proxy port
go through mihomo. Rebuilding the TUN briefly interrupts every connection. Not kept across a config
apply (the config's tun setting wins). Undo: set_tun with the previous value in the result.`,
		toolOpts{destructive: true, idempotent: true}, s.setTun)

	addTool(s, rpc.TierOperate, "tailscale_up", "Connect Tailscale",
		`Connects this device to the tailnet (as tailscale up with the current settings). Undo: tailscale_down.`,
		toolOpts{idempotent: true, noHelper: false}, s.tailscaleUp)

	addTool(s, rpc.TierOperate, "tailscale_down", "Disconnect Tailscale",
		`Disconnects this device from the tailnet (as tailscale down): tailnet devices and names stop
answering, and an exit node stops being used. If you reach this computer over Tailscale, you lose that
connection. Undo: tailscale_up.`,
		toolOpts{destructive: true, idempotent: true}, s.tailscaleDown)

	addTool(s, rpc.TierOperate, "set_exit_node", "Set exit node",
		`Sends this device's internet traffic through a tailnet peer (peer: name, MagicDNS name, IP or id; it
must offer itself as exit node). With mihomo's TUN on, both want the default route: check status and
health after. Undo: set_exit_node with the previous peer in the result, or clear_exit_node.`,
		toolOpts{destructive: true, idempotent: true}, s.setExitNode)

	addTool(s, rpc.TierOperate, "clear_exit_node", "Stop using exit node",
		`Stops using a Tailscale exit node: internet traffic leaves directly again (or through mihomo).
Undo: set_exit_node with the previous peer in the result.`,
		toolOpts{idempotent: true}, s.clearExitNode)

	addTool(s, rpc.TierOperate, "refresh_subscription", "Refresh subscription",
		`Downloads a subscription again (id; default the active one). When it changed and auto-apply is on, the
helper composes and applies the new config (a hot reload of mihomo; the result has the apply and its
history id). Undo an unwanted apply: history_rollback (configure tier) or in the app.`,
		toolOpts{openWorld: true}, s.refreshSubscription)

	addTool(s, rpc.TierOperate, "flush_fakeip", "Flush fake-ip cache",
		`Clears mihomo's fake-ip cache, so names resolve to new fake addresses. Use after rule or DNS changes
when apps still reach the old destination. Apps holding old fake IPs reconnect. Nothing to undo.`,
		toolOpts{destructive: true}, s.flushFakeIP)
}

type modeIn struct {
	Mode string `json:"mode" jsonschema:"rule, global or direct"`
}

func (s *Server) setMode(c *call, in modeIn) (any, error) {
	m := mihomo.Mode(strings.ToLower(in.Mode))
	switch m {
	case mihomo.ModeRule, mihomo.ModeGlobal, mihomo.ModeDirect:
	default:
		return nil, fmt.Errorf("mode must be rule, global or direct")
	}
	mc, err := c.mihomo(true)
	if err != nil {
		return nil, err
	}
	cfg, err := mc.Configs(c.ctx)
	if err != nil {
		return nil, err
	}
	prev := cfg.Mode
	if err := mc.SetMode(c.ctx, m); err != nil {
		return nil, err
	}
	c.undo = "set_mode mode=" + string(prev)
	return map[string]any{"mode": m, "previous": prev, "undo": c.undo}, nil
}

type selectIn struct {
	Group string `json:"group" jsonschema:"a Selector group's name, as proxies lists it"`
	Node  string `json:"node" jsonschema:"one of the group's members"`
}

func (s *Server) selectNode(c *call, in selectIn) (any, error) {
	mc, err := c.mihomo(true)
	if err != nil {
		return nil, err
	}
	g, err := mc.Proxy(c.ctx, in.Group)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", in.Group, err)
	}
	if g.Type != "Selector" {
		return nil, fmt.Errorf("%q is a %s group; only Selector groups take a choice", in.Group, g.Type)
	}
	found := false
	for _, m := range g.All {
		if m == in.Node {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%q is not a member of %q; members: %s", in.Node, in.Group, strings.Join(g.All, ", "))
	}
	if err := mc.SelectProxy(c.ctx, in.Group, in.Node); err != nil {
		return nil, err
	}
	c.undo = fmt.Sprintf("select_node group=%s node=%s", in.Group, g.Now)
	return map[string]any{"group": in.Group, "node": in.Node, "previous": g.Now, "undo": c.undo}, nil
}

type delayIn struct {
	Node      string `json:"node,omitempty" jsonschema:"one node (or group) to test"`
	Group     string `json:"group,omitempty" jsonschema:"test every member of this group"`
	URL       string `json:"url,omitempty" jsonschema:"test URL (default https://www.gstatic.com/generate_204)"`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"per node (default 5000, max 30000)"`
}

func (s *Server) delayTest(c *call, in delayIn) (any, error) {
	if (in.Node == "") == (in.Group == "") {
		return nil, fmt.Errorf("give node or group (one of them)")
	}
	if in.URL != "" && !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		return nil, fmt.Errorf("url must be http(s)")
	}
	to := time.Duration(in.TimeoutMS) * time.Millisecond
	if to <= 0 {
		to = 5 * time.Second
	}
	to = min(to, 30*time.Second)
	mc, err := c.mihomo(false) // delay tests are GETs
	if err != nil {
		return nil, err
	}
	if in.Node != "" {
		d, err := mc.Delay(c.ctx, in.Node, in.URL, to)
		if err != nil {
			return map[string]any{"node": in.Node, "delay_ms": 0, "error": err.Error()}, nil
		}
		return map[string]any{"node": in.Node, "delay_ms": d}, nil
	}
	res, err := mc.GroupDelay(c.ctx, in.Group, in.URL, to)
	if err != nil {
		return nil, err
	}
	return map[string]any{"group": in.Group, "delays_ms": res, "note": "members missing from delays_ms failed or timed out"}, nil
}

type tunIn struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) setTun(c *call, in tunIn) (any, error) {
	mc, err := c.mihomo(true)
	if err != nil {
		return nil, err
	}
	cfg, err := mc.Configs(c.ctx)
	if err != nil {
		return nil, err
	}
	prev := cfg.Tun.Enable
	if err := mc.SetTun(c.ctx, in.Enabled); err != nil {
		return nil, err
	}
	c.undo = fmt.Sprintf("set_tun enabled=%v", prev)
	return map[string]any{"tun": in.Enabled, "previous": prev, "undo": c.undo}, nil
}

func (s *Server) tailscaleUp(c *call, _ noArgs) (any, error) {
	ts, err := c.tailscale(true)
	if err != nil {
		return nil, err
	}
	if err := ts.Up(c.ctx); err != nil {
		return nil, err
	}
	c.undo = "tailscale_down"
	return map[string]any{"ok": true, "undo": c.undo}, nil
}

func (s *Server) tailscaleDown(c *call, _ noArgs) (any, error) {
	ts, err := c.tailscale(true)
	if err != nil {
		return nil, err
	}
	if err := ts.Down(c.ctx); err != nil {
		return nil, err
	}
	c.undo = "tailscale_up"
	return map[string]any{"ok": true, "undo": c.undo}, nil
}

type exitIn struct {
	Peer string `json:"peer" jsonschema:"the peer's name, MagicDNS name, Tailscale IP or stable id"`
}

// findPeer resolves what an agent calls a peer.
func findPeer(st tailscale.State, q string) (*tailscale.Peer, error) {
	q = strings.TrimSuffix(strings.TrimSpace(q), ".")
	ip, ipErr := netip.ParseAddr(q)
	var hits []*tailscale.Peer
	for i := range st.Peers {
		p := &st.Peers[i]
		match := p.ID == q || strings.EqualFold(p.Name, q) || strings.EqualFold(p.DNSName, q) || strings.EqualFold(p.HostName, q)
		if ipErr == nil {
			for _, a := range p.IPs {
				if a == ip {
					match = true
				}
			}
		}
		if match {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no tailnet peer %q; tailnet_peers lists them", q)
	case 1:
		return hits[0], nil
	}
	return nil, fmt.Errorf("%q matches %d peers; use its id or MagicDNS name", q, len(hits))
}

func (s *Server) setExitNode(c *call, in exitIn) (any, error) {
	ts, err := c.tailscale(true)
	if err != nil {
		return nil, err
	}
	st, err := ts.State(c.ctx)
	if err != nil {
		return nil, err
	}
	p, err := findPeer(st, in.Peer)
	if err != nil {
		return nil, err
	}
	if !p.ExitNodeOption {
		return nil, fmt.Errorf("%s does not offer itself as an exit node", p.Name)
	}
	prev := ""
	if st.ExitNode != nil {
		prev = st.ExitNode.Name
	}
	if err := ts.SetExitNode(c.ctx, p.ID); err != nil {
		return nil, err
	}
	c.undo = "clear_exit_node"
	if prev != "" {
		c.undo = "set_exit_node peer=" + prev
	}
	return map[string]any{"exit_node": p.Name, "previous": prev, "undo": c.undo}, nil
}

func (s *Server) clearExitNode(c *call, _ noArgs) (any, error) {
	ts, err := c.tailscale(true)
	if err != nil {
		return nil, err
	}
	st, err := ts.State(c.ctx)
	if err != nil {
		return nil, err
	}
	prev := ""
	if st.ExitNode != nil {
		prev = st.ExitNode.Name
	}
	if err := ts.SetExitNode(c.ctx, ""); err != nil {
		return nil, err
	}
	if prev != "" {
		c.undo = "set_exit_node peer=" + prev
	}
	return map[string]any{"exit_node": "", "previous": prev, "undo": c.undo}, nil
}

type refreshIn struct {
	ID string `json:"id,omitempty" jsonschema:"subscription id (default: the active one)"`
}

func (s *Server) refreshSubscription(c *call, in refreshIn) (any, error) {
	id := in.ID
	if id == "" {
		var subs []rpc.Subscription
		if err := c.rpc(rpc.MethodSubList, nil, &subs); err != nil {
			return nil, err
		}
		for _, x := range subs {
			if x.Active {
				id = x.ID
			}
		}
		if id == "" {
			return nil, fmt.Errorf("no active subscription")
		}
	}
	prev := c.latestHistory()
	var r rpc.SubRefreshResult
	if err := c.rpc(rpc.MethodSubRefresh, rpc.IDParams{ID: id}, &r); err != nil {
		return nil, err
	}
	out := map[string]any{"id": id, "changed": r.Changed, "via": r.Via}
	if a := r.Apply; a != nil {
		out["apply"] = applySummary(a)
		if prev != "" && a.Written {
			c.undo = "history_rollback id=" + prev
			out["undo"] = c.undo
		}
	}
	return out, nil
}

func (s *Server) flushFakeIP(c *call, _ noArgs) (any, error) {
	mc, err := c.mihomo(true)
	if err != nil {
		return nil, err
	}
	if err := mc.FlushFakeIP(c.ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// latestHistory is the id of the newest history entry ("" when none):
// the config running before a change, to roll back to.
func (c *call) latestHistory() string {
	var hs []rpc.HistoryEntry
	if c.rpc(rpc.MethodHistoryList, nil, &hs) != nil || len(hs) == 0 {
		return ""
	}
	return hs[0].ID
}

// applySummary is what an agent needs of an apply result.
func applySummary(a *rpc.ApplyResult) map[string]any {
	m := map[string]any{
		"written": a.Written, "reloaded": a.Reloaded, "changed": a.Changed, "valid": a.Valid,
		"history_id": a.HistoryID, "rolled_back": a.RolledBack,
	}
	if a.Error != "" {
		m["error"] = a.Error
	}
	if len(a.ValidateErrors) > 0 {
		m["validate_errors"] = a.ValidateErrors
	}
	if a.Impact != nil {
		m["impact"] = a.Impact
	}
	if len(a.Warnings) > 0 {
		m["warnings"] = a.Warnings
	}
	return m
}

func (s *Server) healthEgressRun(c *call, _ noArgs) (any, error) {
	var hr rpc.HealthReport
	if err := c.rpc(rpc.MethodHealthEgress, rpc.HealthEgressParams{}, &hr); err != nil {
		return nil, err
	}
	var rep struct {
		Checks []json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal(hr.Report, &rep); err == nil {
		for _, raw := range rep.Checks {
			var id struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &id) == nil && id.ID == (health.LeakEgress{}).ID() {
				return map[string]any{"check": raw}, nil
			}
		}
	}
	return map[string]any{"report": hr.Report}, nil
}
