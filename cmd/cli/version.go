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
	"sync"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
)

// version is the duet CLI version, set at build time with
// -ldflags "-X main.version=..." (scripts/build-release.sh).
var version = "0.1.0-dev"

func init() {
	register("version", "print the versions of duet, the helper, mihomo and Tailscale (read-only)", runVersion)
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

// VersionReport is the output of `duet version --json`.
type VersionReport struct {
	CLI       VersionEntry `json:"cli"`
	Helper    VersionEntry `json:"helper"`
	Mihomo    VersionEntry `json:"mihomo"`
	Tailscale VersionEntry `json:"tailscale"`
}

// VersionEntry is one component. Version is empty when it could not be read.
type VersionEntry struct {
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"` // where it was read, build info
	Error   string `json:"error,omitempty"`
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the versions as JSON")
	socket := fs.String("socket", rpc.SocketFromEnv(), "helper socket `PATH` (env DUET_SOCKET)")
	controller := fs.String("controller", "", "mihomo controller `HOST:PORT` (default: the app's setting, else 127.0.0.1:9090)")
	timeout := fs.Duration("timeout", 3*time.Second, "how long to wait for each component")
	short := fs.Bool("short", false, "print only the duet CLI version")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *short {
		fmt.Fprintln(stdout, version)
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
	rep := VersionReport{CLI: VersionEntry{
		Version: version,
		Detail:  fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		wg      sync.WaitGroup
		hst     rpc.Status
		herr    error
		mv      mihomo.Version
		merr    error
		tsv     string
		tserr   error
		hasHelp bool
	)
	wg.Add(3)
	go func() { defer wg.Done(); hst, herr = helperStatus(ctx, socket); hasHelp = herr == nil }()
	go func() { defer wg.Done(); mv, merr = mihomoVersion(ctx, controller, secret) }()
	go func() { defer wg.Done(); tsv, tserr = tailscaleVersion(ctx) }()
	wg.Wait()

	if hasHelp {
		rep.Helper = VersionEntry{Version: hst.HelperVersion, Detail: socket}
	} else {
		rep.Helper = VersionEntry{Detail: socket, Error: herr.Error()}
	}

	switch {
	case merr == nil && mv.Version != "":
		d := "controller " + controller
		if mv.Meta {
			d = "Meta, " + d
		}
		rep.Mihomo = VersionEntry{Version: mv.Version, Detail: d}
	case hasHelp && hst.Mihomo.Version != "":
		// The helper may reach a controller this user cannot (secret).
		rep.Mihomo = VersionEntry{Version: hst.Mihomo.Version, Detail: "via the helper, controller " + hst.Mihomo.Controller}
	case merr != nil:
		rep.Mihomo = VersionEntry{Detail: "controller " + controller, Error: merr.Error()}
	default:
		rep.Mihomo = VersionEntry{Detail: "controller " + controller, Error: "empty /version answer"}
	}
	if hasHelp && hst.Kernel.Active != "" {
		k := "binary " + hst.Kernel.Active
		if hst.Kernel.Current != "" && !hst.Kernel.External {
			k = "managed kernel " + hst.Kernel.Current
		}
		rep.Mihomo.Detail = joinDetail(rep.Mihomo.Detail, k)
	}

	if tserr == nil && tsv != "" {
		rep.Tailscale = VersionEntry{Version: tsv, Detail: "tailscaled"}
	} else if tserr != nil {
		rep.Tailscale = VersionEntry{Error: tserr.Error()}
	} else {
		rep.Tailscale = VersionEntry{Error: "tailscaled did not report a version"}
	}
	return rep
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func writeVersions(w io.Writer, rep VersionReport) {
	for _, r := range []struct {
		name string
		e    VersionEntry
	}{{"duet", rep.CLI}, {"duet-helper", rep.Helper}, {"mihomo", rep.Mihomo}, {"tailscale", rep.Tailscale}} {
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
		line := fmt.Sprintf("%-12s %s", r.name, v)
		if len(extra) > 0 {
			line += "  (" + strings.Join(extra, "; ") + ")"
		}
		fmt.Fprintln(w, line)
	}
}
