package engine

// autostart.list / autostart.change: competing clients that will start
// again by themselves (core/autostart).

import (
	"context"
	"strings"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// ownLabels are the services Twinnel runs or adopted: never competitors.
func (e *Engine) ownLabels() []string {
	st, err := e.Store.Settings()
	if err != nil {
		return nil
	}
	return []string{st.Mihomo.ServiceLabel, st.Tailscale.ServiceLabel}
}

// AutostartFind is the competing clients' autostart entries for the
// health check (the read error keeps its kind: denied is not missing).
func (e *Engine) AutostartFind(ctx context.Context) ([]autostart.Entry, error) {
	return autostart.Find(ctx, e.Autostarts, e.ownLabels()...)
}

// HealthBinaries are the programs binary-integrity checks: the managed
// mihomo and the Tailscale binaries Twinnel installed or adopted (signed
// on Windows: the official client is Authenticode-signed).
func (e *Engine) HealthBinaries(goos string) []health.BinaryRef {
	var out []health.BinaryRef
	if e.Kernels != nil {
		if l, err := e.Kernels.List(); err == nil && l.Current != "" {
			out = append(out, health.BinaryRef{Name: "mihomo", Path: e.Kernels.Binary()})
		}
	}
	if st, err := e.Store.Settings(); err == nil {
		signed := goos == "windows"
		if st.Tailscale.Binary != "" {
			out = append(out, health.BinaryRef{Name: "tailscale", Path: st.Tailscale.Binary, Signed: signed})
		}
		if st.Tailscale.DaemonBinary != "" {
			out = append(out, health.BinaryRef{Name: "tailscaled", Path: st.Tailscale.DaemonBinary, Signed: signed})
		}
	}
	return out
}

// AutostartList lists the competing clients' autostart entries.
func (e *Engine) AutostartList(ctx context.Context) rpc.AutostartList {
	if e.Autostarts == nil {
		return rpc.AutostartList{Entries: []autostart.Entry{}}
	}
	es, err := autostart.Find(ctx, e.Autostarts, e.ownLabels()...)
	out := rpc.AutostartList{Supported: true, Entries: es}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

// AutostartChange disables (or enables back) entries through the helper.
// Every entry must be one the helper can change; nothing runs otherwise.
func (e *Engine) AutostartChange(ctx context.Context, p rpc.AutostartChangeParams) (rpc.AutostartChangeResult, error) {
	out := rpc.AutostartChangeResult{Steps: []string{}}
	op := autostart.Op(p.Op)
	if op != autostart.OpDisable && op != autostart.OpEnable {
		return out, rpc.ErrInvalid("op must be %q or %q", autostart.OpDisable, autostart.OpEnable)
	}
	if len(p.IDs) == 0 {
		return out, rpc.ErrInvalid("no entries")
	}
	if e.Autostarts == nil {
		return out, rpc.Errorf(rpc.CodeNotImplemented, "%v", autostart.ErrNotSupported)
	}
	all, _ := autostart.Find(ctx, e.Autostarts, e.ownLabels()...)
	byID := map[string]autostart.Entry{}
	for _, en := range all {
		byID[en.ID] = en
	}
	var cmds [][]string
	var picked []autostart.Entry
	for _, id := range p.IDs {
		en, ok := byID[id]
		if !ok {
			return out, rpc.ErrInvalid("no competing autostart %q (list them with autostart.list)", id)
		}
		c, err := autostart.Plan(en, op)
		if err != nil {
			return out, rpc.ErrInvalid("%v", err)
		}
		cmds = append(cmds, c...)
		picked = append(picked, en)
	}
	for _, c := range cmds {
		out.Steps = append(out.Steps, strings.Join(c, " "))
	}
	r := e.ServiceRunner
	if p.Preview || r == nil || r.DryRun || r.Exec == nil {
		out.DryRun = true
		return out, nil
	}
	for i, c := range cmds {
		if b, err := r.Exec.Run(ctx, c); err != nil {
			e.log().Warn("autostart change failed", "op", op, "step", i, "err", err)
			// Put back the steps that ran (best effort).
			e.autostartRevert(ctx, picked, op)
			msg := strings.TrimSpace(string(b))
			if msg == "" {
				msg = err.Error()
			}
			return out, rpc.Errorf(rpc.CodeFailed, "%s: %s", out.Steps[i], msg)
		}
	}
	back := autostart.OpEnable
	if op == autostart.OpEnable {
		back = autostart.OpDisable
	}
	out.Changed = picked
	out.Undo = &rpc.AutostartChangeParams{IDs: p.IDs, Op: string(back)}
	e.log().Info("autostart changed", "op", op, "entries", len(picked))
	e.triggerHealth("autostart " + string(op))
	return out, nil
}

func (e *Engine) autostartRevert(ctx context.Context, es []autostart.Entry, op autostart.Op) {
	back := autostart.OpEnable
	if op == autostart.OpEnable {
		back = autostart.OpDisable
	}
	for _, en := range es {
		cmds, err := autostart.Plan(en, back)
		if err != nil {
			continue
		}
		for _, c := range cmds {
			_, _ = e.ServiceRunner.Exec.Run(ctx, c)
		}
	}
}
