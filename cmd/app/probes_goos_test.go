package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health/fixture"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// A Windows shot run shows a Windows machine: no brew, utun, en0 or Unix
// paths anywhere in what the probes answer (ui-review 8.6), and the
// overview still finds mihomo and tailscaled in its process list.
func TestShotProbesPerGOOS(t *testing.T) {
	p := &shotProbes{goos: "windows", machine: shotMachine("windows")}
	in := p.tailscaleInstall()
	if in.Kind != tailscale.InstallWindows || in.Daemon != tailscale.WindowsDaemon {
		t.Errorf("windows install = %+v", in)
	}
	procs, _ := p.processes(context.Background())
	if status.FindProcess(procs, time.Now(), status.IsMihomoCommand) == nil || status.FindProcess(procs, time.Now(), status.IsTailscaleDaemon) == nil {
		t.Errorf("windows processes not recognised: %+v", procs)
	}
	m, _ := json.Marshal(p.machine)
	all := string(m) + shotCLILink("windows")
	for _, pr := range procs {
		all += " " + pr.Command
	}
	all += " " + in.Daemon + " " + in.Source
	for _, mac := range []string{"utun", `"en0"`, "brew", "/usr/", "/opt/", "macOS"} {
		if strings.Contains(all, mac) {
			t.Errorf("windows fixtures contain %q", mac)
		}
	}
	if p.machine.TailnetIface != "Tailscale" || p.machine.Runtime.Configs.TUN.Device != "Meta" {
		t.Errorf("adapters: tailnet %q, tun %q", p.machine.TailnetIface, p.machine.Runtime.Configs.TUN.Device)
	}
	// The rename works on a copy: the shared synthetic fixture is untouched.
	if fixture.Synthetic().TailnetIface != "utun9" || fixture.Synthetic().Config.TUN.Device != "utun5" {
		t.Error("shotMachine changed fixture.Synthetic")
	}

	// macOS (and a probe set without goos, as older tests build it) keeps
	// exactly the fixtures its shots always had.
	for _, goos := range []string{"darwin", ""} {
		d := &shotProbes{goos: goos, machine: shotMachine("darwin")}
		if in := d.tailscaleInstall(); in.Source != "brew" || in.Daemon != "/opt/homebrew/opt/tailscale/bin/tailscaled" {
			t.Errorf("%q install = %+v", goos, in)
		}
		if procs, _ := d.processes(context.Background()); procs[0].Command != "/usr/local/bin/mihomo" {
			t.Errorf("%q processes = %+v", goos, procs)
		}
	}
	if shotCLILink("darwin") != "/usr/local/bin/twinnel" {
		t.Error("macOS CLI link changed")
	}
	if l := (&shotProbes{goos: "linux"}).tailscaleInstall(); l.Kind != tailscale.InstallTailscaled || l.Daemon != "/usr/sbin/tailscaled" {
		t.Errorf("linux install = %+v", l)
	}
}
