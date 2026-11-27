// Package healthfix runs the fixes of the health checks' failure reasons
// (core/health's fix catalog): it previews a fix (Plan), applies it
// through the helper or the app's own mihomo/Tailscale client (Apply),
// runs the checks again and returns the check's new result, and puts the
// change back where it can (Undo).
//
// The app, `twinnel doctor --fix` and the MCP server share it, so a fix
// does the same everywhere. Fixes that write the system or the config go
// through the helper, which validates, backs up and rolls back as for any
// apply.
package healthfix

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/rpc/healthsrc"
)

// Caller calls the helper.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Mihomo switches mihomo's mode (the controller).
type Mihomo interface {
	Mode(ctx context.Context) (string, error)
	SetMode(ctx context.Context, mode string) error
}

// Tailscale reads and sets the exit node (LocalAPI). ExitNode returns the
// id SetExitNode takes ("" for none).
type Tailscale interface {
	ExitNode(ctx context.Context) (string, error)
	SetExitNode(ctx context.Context, id string) error
}

// Fixer runs fixes. Any dependency may be nil: a fix that needs a missing
// one is Blocked in its plan.
type Fixer struct {
	Helper    Caller
	Mihomo    Mihomo
	Tailscale Tailscale
	Lang      string // core/i18n tag of the texts
	Platform  string // runtime.GOOS of the machine
	// Rerun runs the checks without the helper (the app in-process); nil
	// uses health.run.
	Rerun func(ctx context.Context) (*health.Report, error)
}

// Plan is a fix as previewed: what is wrong, how it is fixed, and, for a
// one-click fix, its steps and whether it can be undone.
type Plan struct {
	Check   string         `json:"check"`
	Reason  string         `json:"reason"`
	Kind    health.FixKind `json:"kind"`
	Action  string         `json:"action,omitempty"` // one-click (auto) or the extra button of a guided/manual fix
	Target  string         `json:"target,omitempty"` // guided
	Open    string         `json:"open,omitempty"`   // manual: a system setting
	Param   string         `json:"param,omitempty"`
	Text    health.FixText `json:"text"`
	Steps   []string       `json:"steps,omitempty"`
	Undo    bool           `json:"undo,omitempty"`    // Apply returns an undo
	Blocked string         `json:"blocked,omitempty"` // why the one-click fix cannot run here
	Command string         `json:"command,omitempty"` // the check's command, for manual fixes
}

// Undo puts a fix back. A fix that writes the config records, for
// every stored layer and setting it writes, the value before the fix
// (Layers, Settings); the undo writes those back and applies, so the
// layer store and the running config return to the same pre-fix state.
type Undo struct {
	Action string `json:"action"`
	Check  string `json:"check,omitempty"`  // the check the fix was for
	Reason string `json:"reason,omitempty"` // the failure reason it fixed
	// Layers are the stored layers the fix wrote, as they were before it.
	Layers   []LayerBefore       `json:"layers,omitempty"`
	Settings *rpc.SettingsUpdate `json:"settings,omitempty"` // settings as before
	Mode     string              `json:"mode,omitempty"`
	ExitNode string              `json:"exit_node,omitempty"`
	DNS      bool                `json:"dns,omitempty"` // dns.restore
	// Autostart enables again the entries a disable-autostart turned off.
	Autostart *rpc.AutostartChangeParams `json:"autostart,omitempty"`

	// LayerID and HistoryID are the undo of older versions, still
	// accepted: a layer the fix added (deleted on undo), and the history
	// entry before the fix (its layers are put back from that entry's
	// manifest, then applied; never a bare rollback).
	LayerID   string `json:"layer_id,omitempty"`
	HistoryID string `json:"history_id,omitempty"`
}

// LayerBefore is a stored layer as it was before a fix: Absent when there
// was none (the undo deletes it), else its metadata and content.
type LayerBefore struct {
	ID      string `json:"id"`
	Absent  bool   `json:"absent,omitempty"`
	Name    string `json:"name,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Format  string `json:"format,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Content string `json:"content,omitempty"`
}

// Outcome is what Apply did.
type Outcome struct {
	Undo   *Undo               `json:"undo,omitempty"`
	Check  *health.CheckReport `json:"check,omitempty"` // the check's new result
	Report *health.Report      `json:"-"`
	Notes  []string            `json:"notes,omitempty"`
}

// ErrNoFix: the check has no problem to fix.
var ErrNoFix = errors.New("nothing to fix: the check has no failure reason")

// Plans returns the plans of every reason of cr, in its order, one per
// distinct fix.
func (f *Fixer) Plans(ctx context.Context, cr health.CheckReport) []Plan {
	var out []Plan
	seen := map[string]bool{}
	for _, r := range cr.Reasons {
		p, err := f.Plan(ctx, cr, r)
		if err != nil {
			continue
		}
		key := string(p.Kind) + "|" + p.Action + "|" + p.Target + "|" + p.Open
		if p.Kind == health.FixManual {
			key += "|" + r
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// Plan previews the fix of one reason of cr. It reads only.
func (f *Fixer) Plan(ctx context.Context, cr health.CheckReport, reason string) (Plan, error) {
	spec, ok := health.FixFor(reason, f.Platform)
	if !ok {
		return Plan{}, fmt.Errorf("no fix for %q", reason)
	}
	L := tx(f.Lang)
	p := Plan{Check: cr.ID, Reason: reason, Kind: spec.Kind, Action: spec.Action, Target: spec.Target, Open: spec.Open}
	p.Text, _ = health.FixTextFor(f.Lang, f.Platform, reason)
	if cr.Fix != nil {
		p.Param, p.Command = cr.Fix.Param, cr.Fix.Command
	}
	if p.Action == "" {
		return p, nil
	}
	need := func(ok bool, why string) {
		if !ok && p.Blocked == "" {
			p.Blocked = why
		}
	}
	switch p.Action {
	case health.FixRerun:
		p.Steps = []string{L.stepRerun}
	case health.FixEgressProbe:
		need(f.Helper != nil || f.Rerun != nil, L.blockedHelper)
		p.Steps = []string{L.stepEgress}
	case health.FixCoexist:
		need(f.Helper != nil, L.blockedHelper)
		p.Undo = true // turns the override on: off again on undo
		if f.Helper != nil {
			var st rpc.Settings
			if err := f.Helper.Call(ctx, rpc.MethodSettingsGet, rpc.Empty{}, &st); err == nil {
				if p.Undo = !st.TailscaleCoexist; p.Undo {
					p.Steps = append(p.Steps, L.stepCoexistOn)
				}
			}
		}
		p.Steps = append(p.Steps, L.stepCoexistRegen, L.stepApply)
	case health.FixLeakGuard:
		need(f.Helper != nil, L.blockedHelper)
		groups := splitParam(p.Param)
		p.Steps = []string{fmt.Sprintf(L.stepGuard, strings.Join(groups, ", ")), L.stepApply}
		p.Undo = true
	case health.FixControllerLoopback:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{fmt.Sprintf(L.stepLoopback, loopbackAddr(p.Param)), L.stepApply}
		p.Undo = true
	case health.FixControllerPort:
		need(f.Helper != nil, L.blockedHelper)
		from, to := "127.0.0.1:9090", "127.0.0.1:9097"
		if f.Helper != nil {
			var r rpc.ControllerMoveResult
			if err := f.Helper.Call(ctx, rpc.MethodControllerMove, rpc.ControllerMoveParams{DryRun: true}, &r); err == nil && r.To != "" {
				from, to = r.From, r.To
			}
		}
		p.Steps = []string{fmt.Sprintf(L.stepPortMove, from, to), fmt.Sprintf(L.stepPortWrite, from), L.stepPortRestart, L.stepPortNote}
	case health.FixController:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{L.stepSecret, L.stepApply, L.stepSecretNote}
	case health.FixTUNStack:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{L.stepTUNStack, L.stepApply, L.stepTUNRebuild}
		p.Undo = true
	case health.FixSystemDNS:
		need(f.Helper != nil, L.blockedHelper)
		if f.Helper != nil {
			var ds rpc.DNSStatus
			if err := f.Helper.Call(ctx, rpc.MethodDNSStatus, rpc.Empty{}, &ds); err == nil {
				need(ds.Supported, L.blockedDNS)
				for _, s := range ds.Services {
					if !slices.Equal(s.Servers, ds.Target) {
						p.Steps = append(p.Steps, fmt.Sprintf(L.stepDNSService, s.Name, orNone(L, s.Servers), strings.Join(ds.Target, ", ")))
					}
				}
			}
		}
		if len(p.Steps) == 0 {
			p.Steps = []string{L.stepDNS}
		}
		p.Undo = true
	case health.FixModeRule:
		need(f.Mihomo != nil, L.blockedMihomo)
		p.Steps = []string{L.stepModeRule}
		p.Undo = true
	case health.FixExitNodeOff:
		need(f.Tailscale != nil, L.blockedTailscale)
		p.Steps = []string{L.stepExitOff}
		p.Undo = true
	case health.FixRestartMihomo:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{L.stepRestartMihomo}
	case health.FixRestartTailscaled:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{L.stepRestartTailscaled}
	default:
		if !f.planExtra(ctx, &p) {
			return p, fmt.Errorf("unknown fix action %q", p.Action)
		}
	}
	return p, nil
}

// Apply runs the one-click fix of p, then the checks, and returns the
// check's new result. A failed step leaves what earlier steps changed
// put back where it can.
func (f *Fixer) Apply(ctx context.Context, p Plan) (*Outcome, error) {
	if p.Action == "" {
		return nil, fmt.Errorf("%s has no one-click fix", p.Reason)
	}
	if p.Blocked != "" {
		return nil, errors.New(p.Blocked)
	}
	L := tx(f.Lang)
	out := &Outcome{}
	var rep *health.Report
	var err error
	switch p.Action {
	case health.FixRerun:
	case health.FixEgressProbe:
		if f.Helper != nil {
			var hr rpc.HealthReport
			if err := f.Helper.Call(ctx, rpc.MethodHealthEgress, rpc.HealthEgressParams{Lang: f.Lang}, &hr); err != nil {
				return nil, err
			}
			if rep, err = healthsrc.Decode(hr); err != nil {
				return nil, err
			}
		}
	case health.FixCoexist:
		out.Undo, err = f.applyCoexist(ctx, p)
	case health.FixLeakGuard:
		out.Undo, err = f.applyLeakGuard(ctx, p)
	case health.FixControllerLoopback:
		out.Undo, err = f.applyLoopback(ctx, p)
	case health.FixControllerPort:
		var r rpc.ControllerMoveResult
		err = f.Helper.Call(ctx, rpc.MethodControllerMove, rpc.ControllerMoveParams{}, &r)
		if err == nil {
			out.Notes = append(out.Notes, L.stepPortNote)
		}
	case health.FixTUNStack:
		out.Undo, err = f.applyTUNStack(ctx, p)
	case health.FixController:
		var r rpc.ControllerSecureResult
		err = f.Helper.Call(ctx, rpc.MethodControllerSecure, rpc.Empty{}, &r)
		if err == nil {
			out.Notes = append(out.Notes, L.stepSecretNote)
		}
	case health.FixSystemDNS:
		var ds rpc.DNSStatus
		if err = f.Helper.Call(ctx, rpc.MethodDNSPoint, rpc.Empty{}, &ds); err == nil {
			out.Undo = &Undo{Action: p.Action, DNS: true}
		}
	case health.FixModeRule:
		var prev string
		if prev, err = f.Mihomo.Mode(ctx); err == nil {
			if err = f.Mihomo.SetMode(ctx, "rule"); err == nil && prev != "" && prev != "rule" {
				out.Undo = &Undo{Action: p.Action, Mode: prev}
			}
		}
	case health.FixExitNodeOff:
		var prev string
		if prev, err = f.Tailscale.ExitNode(ctx); err == nil {
			if err = f.Tailscale.SetExitNode(ctx, ""); err == nil && prev != "" {
				out.Undo = &Undo{Action: p.Action, ExitNode: prev}
			}
		}
	case health.FixRestartMihomo, health.FixRestartTailscaled:
		name := "mihomo"
		if p.Action == health.FixRestartTailscaled {
			name = "tailscaled"
		}
		var r rpc.ServiceOpResult
		err = f.Helper.Call(ctx, rpc.MethodServiceRestart, rpc.ServiceParams{Name: name}, &r)
	default:
		var ok bool
		if out.Undo, ok, err = f.applyExtra(ctx, p); !ok {
			return nil, fmt.Errorf("unknown fix action %q", p.Action)
		}
	}
	if err != nil {
		return out, err
	}
	if out.Undo != nil {
		// What the undo goes back to: the preview says it fails again.
		out.Undo.Check, out.Undo.Reason = p.Check, p.Reason
	}
	if rep == nil {
		if rep, err = f.rerun(ctx); err != nil {
			return out, fmt.Errorf("%s: %w", L.errRerun, err)
		}
	}
	out.Report = rep
	if rep != nil {
		for i := range rep.Checks {
			if rep.Checks[i].ID == p.Check {
				out.Check = &rep.Checks[i]
			}
		}
	}
	return out, nil
}

// helper is the helper, or one that refuses every call.
func (f *Fixer) helper() Caller {
	if f.Helper == nil {
		return refuse{errors.New(tx(f.Lang).blockedHelper)}
	}
	return f.Helper
}

type refuse struct{ err error }

func (r refuse) Call(context.Context, string, any, any) error { return r.err }

func (f *Fixer) rerun(ctx context.Context) (*health.Report, error) {
	if f.Helper != nil {
		var hr rpc.HealthReport
		if err := f.Helper.Call(ctx, rpc.MethodHealthRun, rpc.HealthRunParams{Reason: "fix", Lang: f.Lang}, &hr); err != nil {
			return nil, err
		}
		return healthsrc.Decode(hr)
	}
	if f.Rerun != nil {
		return f.Rerun(ctx)
	}
	return nil, nil
}

// apply composes and applies the config; the helper validates it, keeps
// a backup and rolls back a config mihomo refuses.
func (f *Fixer) apply(ctx context.Context, what string) error {
	var r rpc.ApplyResult
	err := f.Helper.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{Reason: "health fix: " + what}, &r)
	if err == nil && r.Error != "" {
		err = errors.New(r.Error)
	}
	return err
}

// applyCoexist turns the coexistence override on if it is off, makes the
// helper read the tailnet now (the override is generated from it) and
// applies. With the override on already nothing stored changes: the fix
// only regenerates it from the current tailnet, and has no undo.
func (f *Fixer) applyCoexist(ctx context.Context, p Plan) (*Undo, error) {
	var st rpc.Settings
	if err := f.Helper.Call(ctx, rpc.MethodSettingsGet, rpc.Empty{}, &st); err != nil {
		return nil, err
	}
	var ch configChange
	if !st.TailscaleCoexist {
		on := true
		ch.settings = &rpc.SettingsUpdate{TailscaleCoexist: &on}
	}
	ch.then = func(ctx context.Context) error {
		return f.Helper.Call(ctx, rpc.MethodTailnetRefresh, rpc.Empty{}, &rpc.TailnetRefreshResult{})
	}
	return f.changeConfig(ctx, p, ch)
}

func (f *Fixer) applyLeakGuard(ctx context.Context, p Plan) (*Undo, error) {
	var st rpc.Settings
	if err := f.Helper.Call(ctx, rpc.MethodSettingsGet, rpc.Empty{}, &st); err != nil {
		return nil, err
	}
	groups := slices.Clone(st.LeakGuardGroups)
	for _, g := range splitParam(p.Param) {
		if !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	return f.changeConfig(ctx, p, configChange{settings: &rpc.SettingsUpdate{LeakGuardGroups: &groups}})
}

// LoopbackLayerID is the override the controller-loopback fix adds.
const LoopbackLayerID = "controller-loopback"

func (f *Fixer) applyLoopback(ctx context.Context, p Plan) (*Undo, error) {
	content := "# Twinnel health fix: the controller listens on this computer only.\nexternal-controller: " + loopbackAddr(p.Param) + "\n"
	on := true
	return f.changeConfig(ctx, p, configChange{layers: []rpc.LayerPutParams{
		{ID: LoopbackLayerID, Name: tx(f.Lang).layerLoopback, Enabled: &on, Content: content}}})
}

// applyTUNStack writes the override that switches mihomo's TUN to the
// gvisor stack (health.TUNStackLayer), enabled, then applies. A new
// override goes last among the user overrides, so it wins over a stack
// set in another one; an override of that id written by hand keeps its
// place and name, and the undo puts its content back.
func (f *Fixer) applyTUNStack(ctx context.Context, p Plan) (*Undo, error) {
	on := true
	return f.changeConfig(ctx, p, configChange{layers: []rpc.LayerPutParams{
		{ID: health.TUNStackLayerID, Name: tx(f.Lang).layerTUNStack, Enabled: &on, Content: health.TUNStackLayer}}})
}

func loopbackAddr(port string) string {
	if port == "" {
		port = "9090"
	}
	return "127.0.0.1:" + port
}

func splitParam(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func orNone(L *texts, xs []string) string {
	if len(xs) == 0 {
		return L.auto
	}
	return strings.Join(xs, ", ")
}
