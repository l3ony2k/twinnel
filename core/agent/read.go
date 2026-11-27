package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/foreign"
	"sort"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// Read tools: always on while MCP is enabled. They change nothing.

type noArgs struct{}

func (s *Server) registerRead() {
	addTool(s, rpc.TierRead, "status", "Status",
		`One snapshot of Twinnel: the helper, mihomo (mode, TUN, version, the node each selector group uses),
the stored configuration (subscription, overrides, last apply, drift), Tailscale (state, exit node,
online peers), the last health result and which agent tiers are on. Start here.`,
		toolOpts{noHelper: true}, s.status)

	addTool(s, rpc.TierRead, "version", "Versions",
		`Versions of Twinnel, the helper (and its two binaries: the one shipped with Twinnel and the installed one,
with a warning when they differ), the mihomo kernel (running, installed, previous), Tailscale, the geo
databases and the web dashboard (geo / dashboard: installed or not, date, source, the last update check
and whether it found something newer). With check_updates it also asks GitHub for the latest mihomo
release, geo files and dashboard (network requests; nothing is installed).`,
		toolOpts{noHelper: true, openWorld: true}, s.version)

	addTool(s, rpc.TierRead, "health", "Health checks",
		`The helper's mihomo + Tailscale coexistence health checks: each check's status (ok, warn, fail, skip),
summary, evidence and fix hint. run=true runs them now (takes a few seconds; read-only), otherwise
the last scheduled result is returned.`,
		toolOpts{}, s.health)

	addTool(s, rpc.TierRead, "proxies", "Proxy groups",
		`mihomo's proxy groups: type, the node each uses now, its members and their last measured delay (ms,
0 = failed). With group set, only that group and its full chain. Use delay_test to measure again.`,
		toolOpts{}, s.proxies)

	addTool(s, rpc.TierRead, "connections", "Connections",
		`A summary of mihomo's open connections: count, totals, and the top destinations, rules, chains and
processes. top bounds each list (default 15).`,
		toolOpts{}, s.connections)

	addTool(s, rpc.TierRead, "traffic", "Traffic",
		`Current throughput through mihomo (bytes/s up and down, sampled over about 3 seconds) and the totals
since mihomo started.`,
		toolOpts{}, s.traffic)

	addTool(s, rpc.TierRead, "logs", "Logs",
		`Recent log lines. source "twinnel" (default): what the helper did (applies, subscription updates,
health changes, agent actions), newest last. source "mihomo": mihomo's live log, listened to for about
3 seconds (the stream has no backlog), at level (debug, info, warning, error; default info).
source "mihomo-output": what mihomo's service wrote to its output file, startup errors included
(a config mihomo refused at start shows up here, not in the live log); level filters it too.
source "helper": the helper's own log (every change it made and who asked, warnings, errors);
level filters it too.`,
		toolOpts{}, s.logs)

	addTool(s, rpc.TierRead, "subscriptions", "Subscriptions",
		`The stored subscriptions: id, name, whether active, update interval, last fetch and error. URLs are
never shown.`,
		toolOpts{}, s.subscriptions)

	addTool(s, rpc.TierRead, "layers", "Overrides",
		`The overrides (layers) Twinnel composes on top of the subscription, in order: id, name, stage, format,
enabled, managed. With id, that override's YAML content (credentials redacted).`,
		toolOpts{}, s.layers)

	addTool(s, rpc.TierRead, "history", "Config history",
		`The composed configs Twinnel applied or backed up, newest first. With diff set to a history id, a
semantic diff of that entry against the running config (or against "against", another id): settings,
groups, rules; node contents are never shown.`,
		toolOpts{}, s.history)

	addTool(s, rpc.TierRead, "preview", "Preview compose",
		`What apply would do now, without doing it: composes the subscription and overrides, validates the
result with mihomo -t, and returns the semantic diff against the running config, the validation
errors (and which override causes them) and the reload's impact (e.g. TUN rebuilt). The diff starts with
a one-line summary and the setting changes; shadowed rules that are the same on both sides are folded
into a count (verbose=true lists them all).`,
		toolOpts{}, s.preview)

	addTool(s, rpc.TierRead, "takeover_plans", "Takeover plans",
		`The takeover plans the helper made (adopting an existing mihomo setup): state, base, what it would
import and change. Read-only: making or adopting a plan is done in the Twinnel app.`,
		toolOpts{}, s.takeoverPlans)

	addTool(s, rpc.TierRead, "tailnet_peers", "Tailnet peers",
		`Tailscale's peers: name, MagicDNS name, IPs, OS, online, how traffic reaches it (direct, derp,
peer-relay, idle), whether it is or may be an exit node. online_only limits the list.`,
		toolOpts{noHelper: true}, s.tailnetPeers)

	if s.d.Doctor != nil {
		addTool(s, rpc.TierRead, "doctor", "Doctor",
			"Runs `twinnel doctor`: the helper's coexistence checks, the report the app shows. Its last report when it is under a minute old "+
				"(ageNs then says how old), else the helper runs them now; fresh=true always runs them now. "+
				"When the helper does not answer, this process runs them itself against the running mihomo and Tailscale; "+
				"the report's source then says local-no-helper, and checks that need the helper's knowledge skip (read-only).",
			toolOpts{noHelper: true}, s.doctor)
	}

	addTool(s, rpc.TierRead, "agent_activity", "Agent activity",
		`What local agents changed through Twinnel, newest first: time, client, tool, arguments (redacted),
result and how to undo it.`,
		toolOpts{}, s.agentActivity)
}

// ---- status ----

type groupNow struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Now     string `json:"now"`
	DelayMS *int   `json:"delay_ms,omitempty"` // of the chosen node's last test; 0 = failed
}

type statusOut struct {
	Twinnel string `json:"twinnel_version"`
	Helper  struct {
		Reachable bool   `json:"reachable"`
		Version   string `json:"version,omitempty"`
		Error     string `json:"error,omitempty"`
	} `json:"helper"`
	Agent  *rpc.AgentSettings `json:"agent_tiers,omitempty"`
	Mihomo struct {
		Reachable bool       `json:"reachable"`
		Version   string     `json:"version,omitempty"`
		Mode      string     `json:"mode,omitempty"`
		TUN       *bool      `json:"tun,omitempty"`
		Groups    []groupNow `json:"selector_groups,omitempty"`
		Error     string     `json:"error,omitempty"`
		// Owner: who listens on the controller address. Foreign: another
		// program's mihomo; what the controller reports is that program's.
		Owner *foreign.Owner `json:"controller_owner,omitempty"`
	} `json:"mihomo"`
	Config *struct {
		ActiveSubscription string     `json:"active_subscription,omitempty"`
		Layers             int        `json:"layers"`
		LastApply          *applyLine `json:"last_apply,omitempty"`
		Drift              *rpc.Drift `json:"drift,omitempty"`
		AutoApply          bool       `json:"auto_apply"`
		TailscaleCoexist   bool       `json:"tailscale_coexist"`
		LeakGuardGroups    []string   `json:"leak_guard_groups,omitempty"`
	} `json:"config,omitempty"`
	Tailscale *tsSummary `json:"tailscale,omitempty"`
	Health    *healthSum `json:"health,omitempty"`
}

type applyLine struct {
	Time      time.Time `json:"time"`
	OK        bool      `json:"ok"`
	Changed   bool      `json:"changed"`
	HistoryID string    `json:"history_id,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type tsSummary struct {
	State       string   `json:"state"`
	Running     bool     `json:"running"`
	Self        string   `json:"self,omitempty"`
	IPs         []string `json:"ips,omitempty"`
	ExitNode    string   `json:"exit_node,omitempty"`
	OnlinePeers int      `json:"online_peers"`
	Peers       int      `json:"peers"`
	MagicDNS    bool     `json:"magic_dns"`
	Health      []string `json:"health,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type healthSum struct {
	Overall string         `json:"overall"`
	Counts  map[string]int `json:"counts,omitempty"`
	At      time.Time      `json:"at,omitzero"`
	Failing []string       `json:"not_ok,omitempty"` // "id: summary" of warn/fail checks
}

func (s *Server) status(c *call, _ noArgs) (any, error) {
	var out statusOut
	out.Twinnel = s.d.Version
	if h, err := c.helper(); err != nil {
		out.Helper.Error = err.Error()
	} else {
		out.Helper.Reachable = true
		a := c.agent
		out.Agent = &a
		var st rpc.Status
		if err := h.Call(c.ctx, rpc.MethodStatus, nil, &st); err == nil {
			out.Helper.Version = st.HelperVersion
			out.Mihomo.Owner = st.Mihomo.Owner
			out.Config = &struct {
				ActiveSubscription string     `json:"active_subscription,omitempty"`
				Layers             int        `json:"layers"`
				LastApply          *applyLine `json:"last_apply,omitempty"`
				Drift              *rpc.Drift `json:"drift,omitempty"`
				AutoApply          bool       `json:"auto_apply"`
				TailscaleCoexist   bool       `json:"tailscale_coexist"`
				LeakGuardGroups    []string   `json:"leak_guard_groups,omitempty"`
			}{ActiveSubscription: st.ActiveSubscription, Layers: st.Layers}
			if a := st.LastApply; a != nil {
				out.Config.LastApply = &applyLine{Time: a.Time, OK: a.Error == "", Changed: a.Changed, HistoryID: a.HistoryID, Reason: a.Reason, Error: a.Error}
			}
			var set rpc.Settings
			if h.Call(c.ctx, rpc.MethodSettingsGet, nil, &set) == nil {
				out.Config.Drift = set.Drift
				out.Config.AutoApply = set.AutoApply
				out.Config.TailscaleCoexist = set.TailscaleCoexist
				out.Config.LeakGuardGroups = set.LeakGuardGroups
			}
		} else {
			out.Helper.Error = err.Error()
		}
		var hr rpc.HealthReport
		if h.Call(c.ctx, rpc.MethodHealthLast, rpc.HealthLastParams{}, &hr) == nil {
			out.Health = summarizeHealth(hr)
		}
	}
	if o := out.Mihomo.Owner; o.IsForeign() {
		// What the controller says is the other program's mihomo, not
		// Twinnel's: its mode, TUN and groups are left out.
		out.Mihomo.Error = foreignOwnerError(*o).Error()
	} else if mc, err := c.mihomo(false); err != nil {
		out.Mihomo.Error = err.Error()
	} else if v, err := mc.Version(c.ctx); err != nil {
		out.Mihomo.Error = err.Error()
	} else {
		out.Mihomo.Reachable, out.Mihomo.Version = true, v.Version
		if cfg, err := mc.Configs(c.ctx); err == nil {
			out.Mihomo.Mode = string(cfg.Mode)
			tun := cfg.Tun.Enable
			out.Mihomo.TUN = &tun
		}
		if all, err := mc.Proxies(c.ctx); err == nil {
			for _, g := range mihomo.Groups(all) {
				if g.Type != "Selector" || g.Name == "GLOBAL" {
					continue
				}
				gn := groupNow{Name: g.Name, Type: g.Type, Now: g.Now}
				if p, ok := all[g.Now]; ok {
					if d, ok := p.LastDelay(); ok {
						gn.DelayMS = &d
					}
				}
				out.Mihomo.Groups = append(out.Mihomo.Groups, gn)
			}
		}
	}
	if ts, err := c.tailscale(false); err == nil {
		out.Tailscale = tsState(c.ctx, ts)
	}
	return out, nil
}

func tsState(ctx context.Context, ts *tailscale.Client) *tsSummary {
	st, err := ts.State(ctx)
	if err != nil {
		return &tsSummary{Error: err.Error()}
	}
	o := &tsSummary{State: st.BackendState, Running: st.Running(), Self: st.Self.DNSName, OnlinePeers: st.OnlinePeers(),
		Peers: len(st.Peers), MagicDNS: st.Prefs.MagicDNS, Health: st.Health}
	for _, ip := range st.Self.IPs {
		o.IPs = append(o.IPs, ip.String())
	}
	if st.ExitNode != nil {
		o.ExitNode = st.ExitNode.Name
	}
	return o
}

func summarizeHealth(hr rpc.HealthReport) *healthSum {
	if len(hr.Report) == 0 {
		return nil
	}
	var r struct {
		Overall string `json:"overall"`
		Checks  []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"checks"`
	}
	if json.Unmarshal(hr.Report, &r) != nil {
		return nil
	}
	h := &healthSum{Overall: r.Overall, Counts: map[string]int{}, At: hr.At}
	for _, ch := range r.Checks {
		h.Counts[ch.Status]++
		if ch.Status == "warn" || ch.Status == "fail" {
			h.Failing = append(h.Failing, ch.ID+": "+ch.Summary)
		}
	}
	return h
}

// ---- version ----

type versionIn struct {
	CheckUpdates bool `json:"check_updates,omitempty" jsonschema:"also ask GitHub for the latest mihomo release and geo files"`
}

func (s *Server) version(c *call, in versionIn) (any, error) {
	out := map[string]any{"twinnel": s.d.Version}
	h, err := c.helper()
	if err != nil {
		out["helper_error"] = err.Error()
	} else {
		var st rpc.Status
		if h.Call(c.ctx, rpc.MethodStatus, nil, &st) == nil {
			out["helper"] = st.HelperVersion
			out["mihomo_running"] = st.Mihomo.Version
			if o := st.Mihomo.Owner; o.IsForeign() {
				// The version that answered is the other program's mihomo.
				out["mihomo_running_owner"] = o
			}
			if st.ServiceHost != nil {
				// Windows: the helper's copy that runs mihomo.
				out["service_host"] = st.ServiceHost
			}
		}
		var kl rpc.KernelList
		if h.Call(c.ctx, rpc.MethodKernelList, nil, &kl) == nil {
			out["kernel"] = kl
		}
		var ti rpc.TailscaleInfo
		if h.Call(c.ctx, rpc.MethodTailscaleInfo, nil, &ti) == nil {
			out["tailscale"] = ti
		}
		if in.CheckUpdates {
			var rel rpc.KernelRelease
			if err := h.Call(c.ctx, rpc.MethodKernelCheck, rpc.KernelCheckParams{}, &rel); err == nil {
				out["kernel_latest"] = rel
			} else {
				out["kernel_latest_error"] = err.Error()
			}
		}
		for _, x := range []struct{ key, status, check string }{
			{"geo", rpc.MethodGeoStatus, rpc.MethodGeoCheck},
			{"dashboard", rpc.MethodUIStatus, rpc.MethodUICheck},
		} {
			method, params := x.status, any(nil)
			if in.CheckUpdates {
				method, params = x.check, rpc.AccessoryCheckParams{}
			}
			var r rpc.AccessoryCheck
			if err := h.Call(c.ctx, method, params, &r); err == nil {
				out[x.key] = r
			} else {
				out[x.key+"_error"] = err.Error()
			}
		}
	}
	if _, ok := out["mihomo_running"]; !ok {
		if mc, err := c.mihomo(false); err == nil {
			if v, err := mc.Version(c.ctx); err == nil {
				out["mihomo_running"] = v.Version
			}
		}
	}
	if s.d.HelperBinaries != nil {
		running, _ := out["helper"].(string)
		if hb := s.d.HelperBinaries(c.ctx, running); hb != nil {
			out["helper_binaries"] = hb
		}
	}
	return out, nil
}

// ---- health ----

type healthIn struct {
	Run bool `json:"run,omitempty" jsonschema:"run the checks now instead of returning the last result"`
}

func (s *Server) health(c *call, in healthIn) (any, error) {
	var hr rpc.HealthReport
	var err error
	if in.Run {
		err = c.rpc(rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "agent"}, &hr)
	} else {
		err = c.rpc(rpc.MethodHealthLast, rpc.HealthLastParams{}, &hr)
	}
	if err != nil {
		return nil, err
	}
	if len(hr.Report) == 0 {
		return map[string]any{"report": nil, "note": "no health run yet; call health with run=true"}, nil
	}
	return map[string]any{"at": hr.At, "reason": hr.Reason, "report": hr.Report}, nil
}

// ---- proxies ----

type proxiesIn struct {
	Group string `json:"group,omitempty" jsonschema:"only this group (and the chain it resolves to)"`
}

type proxyMember struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	DelayMS *int   `json:"delay_ms,omitempty"`
}

type groupOut struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Now     string        `json:"now,omitempty"`
	Chain   []string      `json:"chain,omitempty"`
	Members []proxyMember `json:"members"`
}

func (s *Server) proxies(c *call, in proxiesIn) (any, error) {
	mc, err := c.mihomo(false)
	if err != nil {
		return nil, err
	}
	all, err := mc.Proxies(c.ctx)
	if err != nil {
		return nil, err
	}
	var out []groupOut
	for _, g := range mihomo.Groups(all) {
		if in.Group != "" && g.Name != in.Group {
			continue
		}
		o := groupOut{Name: g.Name, Type: g.Type, Now: g.Now}
		if in.Group != "" {
			o.Chain = mihomo.Resolve(all, g.Name)
		}
		for _, m := range g.All {
			pm := proxyMember{Name: m, Type: all[m].Type}
			if d, ok := all[m].LastDelay(); ok {
				pm.DelayMS = &d
			}
			o.Members = append(o.Members, pm)
		}
		out = append(out, o)
	}
	if in.Group != "" && len(out) == 0 {
		return nil, fmt.Errorf("no group %q; call proxies without a group to list them", in.Group)
	}
	return map[string]any{"groups": out}, nil
}

// ---- connections, traffic ----

type connectionsIn struct {
	Top int `json:"top,omitempty" jsonschema:"entries per list (default 15, max 100)"`
}

type countBytes struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

func topN(m map[string]*countBytes, n int) []countBytes {
	out := make([]countBytes, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (s *Server) connections(c *call, in connectionsIn) (any, error) {
	n := in.Top
	if n <= 0 {
		n = 15
	}
	n = min(n, 100)
	mc, err := c.mihomo(false)
	if err != nil {
		return nil, err
	}
	cs, err := mc.Connections(c.ctx)
	if err != nil {
		return nil, err
	}
	hosts, rules, chains, procs := map[string]*countBytes{}, map[string]*countBytes{}, map[string]*countBytes{}, map[string]*countBytes{}
	add := func(m map[string]*countBytes, k string, b int64) {
		if k == "" {
			k = "(none)"
		}
		e := m[k]
		if e == nil {
			e = &countBytes{Key: k}
			m[k] = e
		}
		e.Count++
		e.Bytes += b
	}
	for _, x := range cs.Connections {
		b := x.Upload + x.Download
		host := x.Metadata.Host
		if host == "" {
			host = x.Metadata.SniffHost
		}
		if host == "" {
			host = x.Metadata.DestinationIP
		}
		add(hosts, host, b)
		rule := x.Rule
		if x.RulePayload != "" {
			rule += "," + x.RulePayload
		}
		add(rules, rule, b)
		add(chains, strings.Join(x.Chains, " ← "), b)
		add(procs, x.Metadata.Process, b)
	}
	return map[string]any{
		"open": len(cs.Connections), "upload_total": cs.UploadTotal, "download_total": cs.DownloadTotal,
		"top_destinations": topN(hosts, n), "top_rules": topN(rules, n), "top_chains": topN(chains, n), "top_processes": topN(procs, n),
	}, nil
}

func (s *Server) traffic(c *call, _ noArgs) (any, error) {
	mc, err := c.mihomo(false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(c.ctx, s.d.LogWindow)
	defer cancel()
	var samples []mihomo.Traffic
	err = mc.StreamTraffic(ctx, func(t mihomo.Traffic) { samples = append(samples, t) })
	if len(samples) == 0 {
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("mihomo sent no traffic sample")
	}
	var up, down int64
	for _, t := range samples {
		up += t.Up
		down += t.Down
	}
	last := samples[len(samples)-1]
	return map[string]any{
		"samples": len(samples), "up_bytes_per_s": up / int64(len(samples)), "down_bytes_per_s": down / int64(len(samples)),
		"up_total": last.UpTotal, "down_total": last.DownTotal,
	}, nil
}

// ---- logs ----

type logsIn struct {
	Source string `json:"source,omitempty" jsonschema:"twinnel (default), mihomo, mihomo-output or helper"`
	Level  string `json:"level,omitempty" jsonschema:"mihomo, mihomo-output and helper: debug, info (default), warning or error"`
	Lines  int    `json:"lines,omitempty" jsonschema:"at most this many lines, newest (default 50, max 500)"`
}

type logLine struct {
	Time  time.Time       `json:"time"`
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Level string          `json:"level,omitempty"`
}

func (s *Server) logs(c *call, in logsIn) (any, error) {
	n := in.Lines
	if n <= 0 {
		n = 50
	}
	n = min(n, 500)
	var out []logLine
	switch in.Source {
	case "", "twinnel":
		var r rpc.EventsWaitResult
		if err := c.rpc(rpc.MethodEventsWait, rpc.EventsWaitParams{After: 0, TimeoutMS: 1}, &r); err != nil {
			return nil, err
		}
		for _, e := range r.Events {
			if e.Type == rpc.EventEventsTruncated {
				continue
			}
			out = append(out, logLine{Time: e.Time, Type: e.Type, Data: e.Data})
		}
	case "mihomo":
		lvl := in.Level
		if lvl == "" {
			lvl = "info"
		}
		switch lvl {
		case "debug", "info", "warning", "error":
		default:
			return nil, fmt.Errorf("level must be debug, info, warning or error")
		}
		mc, err := c.mihomo(false)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(c.ctx, s.d.LogWindow)
		defer cancel()
		err = mc.StreamLogs(ctx, lvl, func(e mihomo.LogEntry) {
			out = append(out, logLine{Time: e.Time, Type: "mihomo", Level: e.Type, Text: e.Payload})
		})
		if err != nil && ctx.Err() == nil {
			return nil, err
		}
	case "mihomo-output", "helper":
		// A log file the helper keeps (service.log): the same
		// key=value lines, parsed alike.
		name := rpc.ServiceLogMihomo
		if in.Source == "helper" {
			name = rpc.ServiceLogHelper
		}
		lvl := orDefault(in.Level, "info")
		var r rpc.ServiceLog
		if err := c.rpc(rpc.MethodServiceLog, rpc.ServiceLogParams{Name: name, Lines: n}, &r); err != nil {
			return nil, err
		}
		for _, f := range r.Files {
			for _, e := range mihomo.ParseLogLines(f.Lines) {
				if mihomo.LevelAtLeast(e.Type, lvl) {
					out = append(out, logLine{Time: e.Time, Type: f.File, Level: e.Type, Text: s.redactString(e.Payload)})
				}
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	default:
		return nil, fmt.Errorf("source must be twinnel, mihomo, mihomo-output or helper")
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return map[string]any{"source": orDefault(in.Source, "twinnel"), "lines": out}, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---- stored configuration ----

func (s *Server) subscriptions(c *call, _ noArgs) (any, error) {
	var subs []rpc.Subscription
	if err := c.rpc(rpc.MethodSubList, nil, &subs); err != nil {
		return nil, err
	}
	return map[string]any{"subscriptions": subs}, nil
}

type layersIn struct {
	ID string `json:"id,omitempty" jsonschema:"return this override with its content"`
}

func (s *Server) layers(c *call, in layersIn) (any, error) {
	if in.ID != "" {
		var l rpc.LayerContent
		if err := c.rpc(rpc.MethodLayerGet, rpc.IDParams{ID: in.ID}, &l); err != nil {
			return nil, err
		}
		l.Content = RedactYAML(l.Content)
		return l, nil
	}
	var ls []rpc.Layer
	if err := c.rpc(rpc.MethodLayerList, nil, &ls); err != nil {
		return nil, err
	}
	return map[string]any{"layers": ls}, nil
}

type historyIn struct {
	Diff    string `json:"diff,omitempty" jsonschema:"a history id: return its semantic diff against the running config"`
	Against string `json:"against,omitempty" jsonschema:"with diff: compare against this history id instead of the running config"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many entries (default 20)"`
	Verbose bool   `json:"verbose,omitempty" jsonschema:"with diff: list every shadowed or redundant rule, also those the same on both sides"`
}

func (s *Server) history(c *call, in historyIn) (any, error) {
	if in.Diff != "" {
		from := rpc.ConfigGetParams{Source: rpc.ConfigLive}
		if in.Against != "" {
			from = rpc.ConfigGetParams{Source: rpc.ConfigHistory, ID: in.Against}
		}
		text, err := c.semDiff(from, rpc.ConfigGetParams{Source: rpc.ConfigHistory, ID: in.Diff}, in.Verbose)
		if err != nil {
			return nil, err
		}
		return map[string]any{"from": orDefault(in.Against, "live"), "to": in.Diff, "diff": text}, nil
	}
	var hs []rpc.HistoryEntry
	if err := c.rpc(rpc.MethodHistoryList, nil, &hs); err != nil {
		return nil, err
	}
	n := in.Limit
	if n <= 0 {
		n = 20
	}
	if len(hs) > n {
		hs = hs[:n]
	}
	return map[string]any{"entries": hs}, nil
}

// semDiff is the semantic diff of two config texts the helper returns.
// SemanticDiff never renders node contents and redacts settings.
// verbose lists every dead rule; otherwise those both sides share are
// folded into a count and a few examples.
func (c *call) semDiff(a, b rpc.ConfigGetParams, verbose bool) (string, error) {
	var ta, tb rpc.ConfigText
	if err := c.rpc(rpc.MethodConfigGet, a, &ta); err != nil {
		return "", fmt.Errorf("%s config: %w", a.Source, err)
	}
	if err := c.rpc(rpc.MethodConfigGet, b, &tb); err != nil {
		return "", fmt.Errorf("%s config: %w", b.Source, err)
	}
	d, err := config.SemanticDiff([]byte(ta.Content), []byte(tb.Content))
	if err != nil {
		return "", err
	}
	if d.Equivalent() {
		return "(no difference in behavior)", nil
	}
	return d.Render(config.TextOptions{Limit: 200, Verbose: verbose, More: "verbose=true lists them all"}), nil
}

// previewOut is compose.preview with the diff against the running config.
type previewOut struct {
	Valid          bool          `json:"valid"`
	ValidateErrors []string      `json:"validate_errors,omitempty"`
	Culprits       []rpc.Culprit `json:"culprits,omitempty"`
	Changed        bool          `json:"changed"`
	Warnings       []string      `json:"warnings,omitempty"`
	Applied        []string      `json:"layers_applied"`
	Hash           string        `json:"preview_hash"`
	Diff           string        `json:"diff"`
}

func (c *call) previewNow(verbose bool) (*previewOut, error) {
	var r rpc.ComposeResult
	if err := c.rpc(rpc.MethodComposePreview, nil, &r); err != nil {
		return nil, err
	}
	out := &previewOut{Valid: r.Valid, ValidateErrors: r.ValidateErrors, Culprits: r.Culprits, Changed: r.Changed,
		Warnings: r.Warnings, Applied: r.Applied, Hash: r.Hash}
	diff, err := c.semDiff(rpc.ConfigGetParams{Source: rpc.ConfigLive}, rpc.ConfigGetParams{Source: rpc.ConfigPreview}, verbose)
	if err != nil {
		out.Diff = "(diff unavailable: " + err.Error() + ")"
	} else {
		out.Diff = diff
	}
	return out, nil
}

type previewIn struct {
	Verbose bool `json:"verbose,omitempty" jsonschema:"list every shadowed or redundant rule, also those the same on both sides"`
}

func (s *Server) preview(c *call, in previewIn) (any, error) { return c.previewNow(in.Verbose) }

func (s *Server) takeoverPlans(c *call, _ noArgs) (any, error) {
	var ps []rpc.TakeoverPlan
	if err := c.rpc(rpc.MethodTakeoverPlans, rpc.TakeoverPlansParams{}, &ps); err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].ConfirmHash = "" // adopting is done in the app
	}
	return map[string]any{"plans": ps}, nil
}

// ---- Tailscale ----

type peersIn struct {
	OnlineOnly bool `json:"online_only,omitempty"`
}

type peerOut struct {
	Name           string   `json:"name"`
	DNSName        string   `json:"dns_name,omitempty"`
	IPs            []string `json:"ips,omitempty"`
	OS             string   `json:"os,omitempty"`
	Online         bool     `json:"online"`
	Path           string   `json:"path"`
	Relay          string   `json:"relay,omitempty"`
	ExitNode       bool     `json:"exit_node,omitempty"`
	ExitNodeOption bool     `json:"exit_node_option,omitempty"`
	ID             string   `json:"id"`
}

func (s *Server) tailnetPeers(c *call, in peersIn) (any, error) {
	ts, err := c.tailscale(false)
	if err != nil {
		return nil, err
	}
	st, err := ts.State(c.ctx)
	if err != nil {
		return nil, err
	}
	var out []peerOut
	for _, p := range st.Peers {
		if in.OnlineOnly && !p.Online {
			continue
		}
		po := peerOut{Name: p.Name, DNSName: p.DNSName, OS: p.OS, Online: p.Online, Path: p.Path(), Relay: p.Relay,
			ExitNode: p.ExitNode, ExitNodeOption: p.ExitNodeOption, ID: p.ID}
		for _, ip := range p.IPs {
			po.IPs = append(po.IPs, ip.String())
		}
		out = append(out, po)
	}
	return map[string]any{"self": st.Self.DNSName, "state": st.BackendState, "peers": out}, nil
}

type doctorIn struct {
	Fresh bool `json:"fresh,omitempty" jsonschema:"run the checks now even when the helper's last report is under a minute old"`
}

func (s *Server) doctor(c *call, in doctorIn) (any, error) {
	raw, err := s.d.Doctor(c.ctx, in.Fresh)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

type activityIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"at most this many (default 20)"`
}

func (s *Server) agentActivity(c *call, in activityIn) (any, error) {
	n := in.Limit
	if n <= 0 {
		n = 20
	}
	var list []rpc.AgentAction
	if err := c.rpc(rpc.MethodAgentActivity, rpc.AgentActivityParams{Limit: n}, &list); err != nil {
		return nil, err
	}
	return map[string]any{"actions": list}, nil
}
