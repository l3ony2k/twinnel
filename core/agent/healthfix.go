package agent

import (
	"fmt"
	"runtime"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/healthfix"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
)

// Health fixes (core/healthfix): the same fixes as the app's 修复 and
// `twinnel doctor --fix`. Listing them reads; a fix that only switches
// something reversible (mode, exit node, a restart, the egress test, a
// rerun) is in the operate tier; one that changes the config or the
// system DNS is in the configure tier.

// configureActions are the fix actions of the configure tier.
var configureActions = map[string]bool{
	health.FixCoexist: true, health.FixLeakGuard: true, health.FixControllerLoopback: true,
	health.FixController: true, health.FixSystemDNS: true, health.FixControllerPort: true,
}

func (s *Server) registerHealthFix() {
	addTool(s, rpc.TierRead, "health_fixes", "Health fixes",
		`For a health check (check: its id) or every failing one: why it fails (reason codes), and for each
reason how it is fixed: kind "auto" (one call: health_fix or health_fix_config, steps listed), "guided"
(a place in the Twinnel app where the person changes it: target) or "manual" (Twinnel cannot fix it;
the text says why and what to do outside). Reads only.`,
		toolOpts{}, s.healthFixes)

	addTool(s, rpc.TierOperate, "health_fix", "Fix a health check",
		`Runs the one-click fix of a failure reason (check, reason from health_fixes) whose action only
switches something reversible: rerun, egress-probe, mode-rule, exit-node-off, restart-mihomo,
restart-tailscaled (a restart drops the network or tailnet for a few seconds). Then the checks run
again; the result has the check's new status and, when it can be put back, undo: pass it as undo to
this tool to revert. Config and system DNS fixes are health_fix_config.`,
		toolOpts{destructive: true}, s.healthFixOperate)

	addTool(s, rpc.TierConfigure, "health_fix_config", "Fix a health check (config)",
		`Runs the one-click fix of a failure reason (check, reason from health_fixes) that changes the config
or the system: tailscale-coexist-override, leak-guard-override, controller-loopback (overrides,
then an apply the helper validates and rolls back on failure), controller-secret (new controller
secret: other controller clients need it), system-dns (point the system DNS at mihomo, backed up).
Then the checks run again. Undo: pass the result's undo as undo (controller-secret has none).`,
		toolOpts{destructive: true}, s.healthFixConfigure)
}

type healthFixesIn struct {
	Check string `json:"check,omitempty" jsonschema:"a check id; empty: every failing or warning check"`
}

type healthFixIn struct {
	Check  string          `json:"check,omitempty" jsonschema:"the check id"`
	Reason string          `json:"reason,omitempty" jsonschema:"a reason code of the check (health_fixes); empty: its first one-click fix"`
	Undo   *healthfix.Undo `json:"undo,omitempty" jsonschema:"the undo of an earlier fix, to revert it instead"`
}

func (s *Server) fixer(c *call) *healthfix.Fixer {
	f := &healthfix.Fixer{Lang: "en", Platform: runtime.GOOS}
	if c.h != nil {
		f.Helper = c.h
	}
	if mc, err := c.mihomo(true); err == nil {
		f.Mihomo = healthfix.MihomoClient(mc)
	}
	if ts, err := c.tailscale(true); err == nil {
		f.Tailscale = healthfix.TailscaleClient(ts)
	}
	return f
}

func (s *Server) lastReport(c *call) (*health.Report, error) {
	var hr rpc.HealthReport
	if err := c.rpc(rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "agent", Lang: "en"}, &hr); err != nil {
		return nil, err
	}
	rep, err := healthsrc.Decode(hr)
	if err == nil && rep == nil {
		err = fmt.Errorf("no health report")
	}
	return rep, err
}

func (s *Server) healthFixes(c *call, in healthFixesIn) (any, error) {
	rep, err := s.lastReport(c)
	if err != nil {
		return nil, err
	}
	f := s.fixer(c)
	var out []map[string]any
	for _, cr := range rep.Checks {
		if in.Check != "" && cr.ID != in.Check || in.Check == "" && len(cr.Reasons) == 0 {
			continue
		}
		out = append(out, map[string]any{"check": cr.ID, "status": cr.Status, "summary": cr.Summary,
			"reasons": cr.Reasons, "fixes": f.Plans(c.ctx, cr)})
	}
	if in.Check != "" && len(out) == 0 {
		return nil, fmt.Errorf("no check %q", in.Check)
	}
	return map[string]any{"checks": out}, nil
}

func (s *Server) healthFixOperate(c *call, in healthFixIn) (any, error) {
	return s.healthFix(c, in, false)
}

func (s *Server) healthFixConfigure(c *call, in healthFixIn) (any, error) {
	return s.healthFix(c, in, true)
}

func (s *Server) healthFix(c *call, in healthFixIn, configure bool) (any, error) {
	tierOK := func(action string) error {
		if configureActions[action] != configure {
			if configure {
				return fmt.Errorf("%s is an operate-tier fix: use health_fix", action)
			}
			return fmt.Errorf("%s changes the config or the system: use health_fix_config", action)
		}
		return nil
	}
	f := s.fixer(c)
	if in.Undo != nil {
		if err := tierOK(in.Undo.Action); err != nil {
			return nil, err
		}
		rep, err := f.Undo(c.ctx, in.Undo)
		if err != nil {
			return nil, err
		}
		c.undo = "health_fix again"
		return map[string]any{"undone": in.Undo.Action, "overall": overall(rep)}, nil
	}
	rep, err := s.lastReport(c)
	if err != nil {
		return nil, err
	}
	var cr *health.CheckReport
	for i := range rep.Checks {
		if rep.Checks[i].ID == in.Check {
			cr = &rep.Checks[i]
		}
	}
	if cr == nil {
		return nil, fmt.Errorf("no check %q", in.Check)
	}
	var plan *healthfix.Plan
	for _, p := range f.Plans(c.ctx, *cr) {
		if p.Action != "" && (in.Reason == "" || p.Reason == in.Reason) && (in.Reason != "" || p.Kind == health.FixAuto) {
			plan = &p
			break
		}
	}
	if plan == nil {
		return nil, fmt.Errorf("%s has no one-click fix for %q now (status %s; see health_fixes)", cr.ID, in.Reason, cr.Status)
	}
	if err := tierOK(plan.Action); err != nil {
		return nil, err
	}
	out, err := f.Apply(c.ctx, *plan)
	if err != nil {
		return nil, err
	}
	res := map[string]any{"action": plan.Action, "steps": plan.Steps, "check": out.Check}
	if out.Undo != nil {
		res["undo"] = out.Undo
		c.undo = fmt.Sprintf("%s with undo", map[bool]string{true: "health_fix_config", false: "health_fix"}[configure])
	}
	if len(out.Notes) > 0 {
		res["notes"] = out.Notes
	}
	return res, nil
}

func overall(r *health.Report) health.Status {
	if r == nil {
		return ""
	}
	return r.Overall
}
