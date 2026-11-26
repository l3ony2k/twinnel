package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

// ServiceRunner carries out service plans. cmd/helper sets Exec and Files
// to the real launchctl and filesystem only when it runs as root;
// otherwise DryRun is set and plans are only reported.
type ServiceRunner struct {
	Exec   service.Executor
	Files  service.Files
	DryRun bool
}

// serviceOps is the fixed allowlist of service operations per managed
// service. The helper's own service is not in it: stopping or restarting
// it over its own socket would cut the call short (use `duet helper
// install/uninstall`).
var serviceOps = map[string][]service.Op{
	"mihomo":     {service.OpStart, service.OpStop, service.OpRestart, service.OpInstall, service.OpUninstall},
	"tailscaled": {service.OpStart, service.OpStop, service.OpRestart},
}

func allowedOp(name string, op service.Op) bool {
	for _, o := range serviceOps[name] {
		if o == op {
			return true
		}
	}
	return false
}

// mihomoSpec is the mihomo service generated from settings: the managed
// kernel through its "current" link (so a kernel switch needs only a
// restart), or the adopted binary.
func (e *Engine) mihomoSpec(st *store.Settings) (service.Spec, error) {
	bin := st.Mihomo.Binary
	if bin == "" {
		if e.Kernels == nil {
			return service.Spec{}, errors.New("no mihomo binary")
		}
		bin = e.Kernels.Binary()
		if _, err := os.Stat(bin); err != nil {
			return service.Spec{}, errors.New("no mihomo kernel installed: run kernel.install first")
		}
	}
	logDir := st.Mihomo.LogDir
	if logDir == "" {
		logDir = e.Store.Path("logs")
	}
	return service.MihomoSpec(st.Mihomo.ServiceLabel, bin, st.Mihomo.ConfigDir, st.Mihomo.ConfigFile, logDir), nil
}

// ServiceOp runs one allowlisted operation on a managed service.
func (e *Engine) ServiceOp(ctx context.Context, name string, op service.Op) (rpc.ServiceOpResult, error) {
	out := rpc.ServiceOpResult{Name: name, Op: string(op), Steps: []string{}}
	if _, known := serviceOps[name]; !known {
		if name == "helper" {
			return out, rpc.ErrInvalid("the helper's own service is managed with `duet helper install/uninstall`, not over RPC")
		}
		return out, rpc.ErrInvalid("unknown service %q (mihomo, tailscaled)", name)
	}
	if !allowedOp(name, op) {
		return out, rpc.ErrInvalid("service.%s is not allowed for %s", op, name)
	}
	if e.Services == nil {
		return out, rpc.Errorf(rpc.CodeFailed, "no service manager on this platform")
	}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	var spec service.Spec
	switch name {
	case "mihomo":
		spec.Label = st.Mihomo.ServiceLabel
		if op == service.OpInstall {
			if spec, err = e.mihomoSpec(st); err != nil {
				return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
			}
		}
	case "tailscaled":
		spec.Label = st.Tailscale.ServiceLabel
	}
	out.Label = spec.Label
	steps, err := e.Services.Plan(op, spec)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	if op == service.OpInstall {
		// The log dir must exist before launchd opens the log files.
		if spec.StdoutPath != "" {
			steps = append([]service.Step{{Kind: service.StepMkdir, Path: filepath.Dir(spec.StdoutPath), Mode: 0o755}}, steps...)
		}
		if _, err := os.Stat(st.Mihomo.ConfigFile); err != nil {
			e.log().Warn("installing mihomo service without a config file yet", "config", st.Mihomo.ConfigFile)
		}
	}
	for _, s := range steps {
		out.Steps = append(out.Steps, s.String())
	}

	r := e.ServiceRunner
	if r == nil || r.DryRun || r.Exec == nil || r.Files == nil {
		out.DryRun = true
		e.log().Info("service op (dry run)", "service", name, "op", op, "steps", len(steps))
	} else {
		e.serviceMu.Lock()
		err = service.Apply(ctx, steps, r.Exec, r.Files)
		e.serviceMu.Unlock()
		e.log().Info("service op", "service", name, "op", op, "err", err)
		if err != nil {
			out.Status, _ = e.ServiceStatus(ctx, name)
			return out, rpc.Errorf(rpc.CodeFailed, "%s %s: %v", op, name, err)
		}
	}
	out.Status, _ = e.ServiceStatus(ctx, name)
	e.publish(rpc.EventServiceChanged, map[string]any{"name": name, "op": op, "dry_run": out.DryRun})
	e.triggerHealth("service " + string(op) + " " + name)
	return out, nil
}
