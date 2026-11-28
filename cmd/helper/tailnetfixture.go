package main

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"

	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// registerFixtureTailscale makes a helper started with
// --tailnet-status-file (development, shots) answer tailscale.info from
// that fixture, as it already reads the tailnet from it: never the
// machine's tailscale CLI, nor pkgs.tailscale.com for the latest version.
// It runs after Engine.Register, whose handler it replaces.
func registerFixtureTailscale(srv *rpc.Server, o *options) {
	if o == nil || o.tailnetStatus == "" {
		return
	}
	f := fileTailnet(o.tailnetStatus)
	rpc.Typed(srv, rpc.MethodTailscaleInfo, func(ctx context.Context, _ rpc.Empty) (rpc.TailscaleInfo, error) {
		return f.Info(ctx, runtime.GOOS), nil
	})
}

// Info is the tailscale.info of the fixture's node: installed, at the
// version its status names, of the kind Twinnel installs on goos; no
// path (no binary was looked for) and no latest version (nothing was
// fetched), so no update is offered.
func (f fileTailnet) Info(ctx context.Context, goos string) rpc.TailscaleInfo {
	kind := kernel.TSBrew
	if goos == "windows" {
		kind = kernel.TSWindows
	}
	out := rpc.TailscaleInfo{InstallKind: kind}
	b, err := f.Status(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	var st struct{ Version string }
	if err := json.Unmarshal(b, &st); err != nil {
		out.Error = err.Error()
		return out
	}
	out.Installed = true
	out.Version, _, _ = strings.Cut(st.Version, "-") // 1.90.6-t1234abcd-g5678 → 1.90.6
	return out
}
