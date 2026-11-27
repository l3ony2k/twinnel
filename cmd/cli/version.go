package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
	coreversion "github.com/l3ony2k/twinnel/core/version"
)

func init() {
	register("version", "print the versions of Twinnel, the helper, mihomo and Tailscale (read-only)", runVersion)
}

// Version sources, replaced by tests. All of them only read.
var (
	// helperStatus asks the helper for status.get.
	helperStatus = func(ctx context.Context, socket string) (rpc.Status, error) {
		c, err := rpc.Dial(ctx, nil, socket)
		if err != nil {
			return rpc.Status{}, err
		}
		defer c.Close()
		var st rpc.Status
		err = c.Call(ctx, rpc.MethodStatus, nil, &st)
		return st, err
	}
	// mihomoVersion sends GET /version to the controller.
	mihomoVersion = func(ctx context.Context, controller, secret string) (mihomo.Version, error) {
		c, err := mihomo.New(mihomo.Options{Address: controller, Secret: secret})
		if err != nil {
			return mihomo.Version{}, err
		}
		return c.Version(ctx)
	}
	// tailscaleVersion reads the daemon's version from the LocalAPI status.
	tailscaleVersion = func(ctx context.Context) (string, error) {
		st, err := tailscale.LocalAPI(nil).Status(ctx)
		if err != nil {
			return "", err
		}
		return st.Version, nil
	}
)

// VersionReport is the output of `twinnel version --json`.
type (
	VersionReport = coreversion.Report
	VersionEntry  = coreversion.Entry
)

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the versions as JSON")
	socket := fs.String("socket", rpc.SocketFromEnv(), "helper socket `PATH` (env TWINNEL_SOCKET)")
	controller := fs.String("controller", "", "mihomo controller `HOST:PORT` (default: the app's setting, else 127.0.0.1:9090)")
	timeout := fs.Duration("timeout", 3*time.Second, "how long to wait for each component")
	short := fs.Bool("short", false, "print only the Twinnel CLI version")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *short {
		fmt.Fprintln(stdout, coreversion.String())
		return nil
	}
	cfg, err := status.LoadConfig(appConfigPath(), os.Getenv)
	if err != nil {
		cfg, _ = status.LoadConfig("", os.Getenv)
	}
	if *controller != "" {
		cfg.Controller = *controller
	}
	rep := collectVersions(context.Background(), *timeout, *socket, cfg.Controller, cfg.Secret)
	if *asJSON {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(b))
		return nil
	}
	writeVersions(stdout, rep)
	return nil
}

// collectVersions asks every component at once; none of them is required.
func collectVersions(ctx context.Context, timeout time.Duration, socket, controller, secret string) VersionReport {
	self := VersionEntry{
		Version: coreversion.String(),
		Detail:  fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}
	return coreversion.Collect(ctx, timeout, self, coreversion.Sources{
		Socket: socket, Controller: controller,
		Helper:    func(ctx context.Context) (rpc.Status, error) { return helperStatus(ctx, socket) },
		Mihomo:    func(ctx context.Context) (mihomo.Version, error) { return mihomoVersion(ctx, controller, secret) },
		Tailscale: tailscaleVersion,
	})
}

func writeVersions(w io.Writer, rep VersionReport) {
	for _, r := range []struct {
		name string
		e    VersionEntry
	}{{"twinnel", rep.CLI}, {"twinnel-helper", rep.Helper}, {"mihomo", rep.Mihomo}, {"tailscale", rep.Tailscale}} {
		v := r.e.Version
		if v == "" {
			v = "-"
		}
		var extra []string
		if r.e.Detail != "" {
			extra = append(extra, r.e.Detail)
		}
		if r.e.Error != "" {
			extra = append(extra, "not reachable: "+r.e.Error)
		}
		line := fmt.Sprintf("%-15s %s", r.name, v)
		if len(extra) > 0 {
			line += "  (" + strings.Join(extra, "; ") + ")"
		}
		fmt.Fprintln(w, line)
	}
}
