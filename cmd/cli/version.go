package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
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
		var st rpc.Status
		err := helperRPC(ctx, socket, rpc.MethodStatus, nil, &st)
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
	// helperBinaryPaths are the helper beside this CLI and the installed
	// one ("" when not there). Their versions come from binaryVersion.
	helperBinaryPaths = func() (bundled, installed string) {
		if b := defaultHelperBin(); filepath.IsAbs(b) {
			bundled = b
		}
		if in, _ := helperPaths(store.DefaultDir, store.DefaultHelperLabel); in != "" && fileExistsFn(in) {
			installed = in
		}
		return bundled, installed
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
	controller := fs.String("controller", "", controllerFlagUsage)
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
	cfg := controllerFor(context.Background(), *socket, *controller)
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
	rep := coreversion.Collect(ctx, timeout, self, coreversion.Sources{
		Socket: socket, Controller: controller,
		Helper:    func(ctx context.Context) (rpc.Status, error) { return helperStatus(ctx, socket) },
		Mihomo:    func(ctx context.Context) (mihomo.Version, error) { return mihomoVersion(ctx, controller, secret) },
		Tailscale: tailscaleVersion,
	})
	rep.HelperBinaries = collectHelperBinaries(ctx, timeout, rep.Helper.Version)
	return rep
}

// collectHelperBinaries reads the version of both helper binaries
// (`--version`) and says when they, or the running helper, differ.
func collectHelperBinaries(ctx context.Context, timeout time.Duration, running string) *coreversion.HelperBinaries {
	bundled, installed := helperBinaryPaths()
	if bundled == "" && installed == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	read := func(p string) coreversion.Entry {
		e := coreversion.Entry{Detail: p}
		if p == "" {
			e.Error = "not found"
			return e
		}
		v, err := binaryVersion(ctx, p)
		if err != nil {
			e.Error = err.Error()
		}
		e.Version = v
		return e
	}
	hb := &coreversion.HelperBinaries{Bundled: read(bundled), Installed: read(installed)}
	hb.Warning = coreversion.HelperDrift(hb.Bundled.Version, hb.Installed.Version, running)
	return hb
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
		if r.name == "twinnel-helper" && rep.HelperBinaries != nil {
			hb := rep.HelperBinaries
			for _, b := range []struct {
				name string
				e    VersionEntry
			}{{"  installed", hb.Installed}, {"  bundled", hb.Bundled}} {
				v := b.e.Version
				if v == "" {
					v = "-"
				}
				line := fmt.Sprintf("%-15s %s", b.name, v)
				if b.e.Detail != "" || b.e.Error != "" {
					line += "  (" + strings.Join(nonEmpty(b.e.Detail, b.e.Error), "; ") + ")"
				}
				fmt.Fprintln(w, line)
			}
		}
	}
	if hb := rep.HelperBinaries; hb != nil && hb.Warning != "" {
		fmt.Fprintln(w, "warning: "+hb.Warning)
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
