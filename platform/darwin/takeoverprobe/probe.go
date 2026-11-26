//go:build darwin

// Package takeoverprobe is the read-only takeover.Probe for macOS. Both the
// CLI (`duet takeover plan`) and the helper (`takeover.plan`) use it.
package takeoverprobe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/core/takeover"
	"github.com/l3ony2k/duet/platform/darwin"
)

// Probe reads launchd plists, asks the mihomo controller for its version,
// runs `tailscale status --json` and `route -n get 100.100.100.100`.
// Nothing it does changes the system.
type Probe struct {
	Run darwin.CommandRunner
	// Home is the user whose ~/Library/LaunchAgents is searched and whose
	// ~/.config/mihomo is the fallback config dir.
	Home string
	// ServiceDirs replaces the launchd directories that are searched
	// (development: a scratch dir holding a copied plist). Each entry is
	// "DIR" (daemons) or "agent:DIR".
	ServiceDirs []string
	// TailscaleBin is the tailscale CLI; empty searches the usual places.
	TailscaleBin string
	// TailnetStatus, if set, replaces `tailscale status --json`
	// (development: a saved status file).
	TailnetStatus func(ctx context.Context) ([]byte, error)
	// TailnetIface, if set, replaces the route lookup.
	TailnetIface func(ctx context.Context) (string, error)
}

// New returns a probe for the user's home dir.
func New(home string) *Probe {
	return &Probe{Run: darwin.ExecRunner(10 * time.Second), Home: home}
}

var _ takeover.Probe = (*Probe)(nil)

// UserHome implements takeover.Probe.
func (p *Probe) UserHome() string { return p.Home }

type serviceDir struct{ dir, kind string }

func (p *Probe) dirs() []serviceDir {
	if len(p.ServiceDirs) > 0 {
		var out []serviceDir
		for _, d := range p.ServiceDirs {
			if rest, ok := strings.CutPrefix(d, "agent:"); ok {
				out = append(out, serviceDir{rest, "launchd-agent"})
			} else {
				out = append(out, serviceDir{d, "launchd-daemon"})
			}
		}
		return out
	}
	out := []serviceDir{
		{"/Library/LaunchDaemons", "launchd-daemon"},
		{"/Library/LaunchAgents", "launchd-agent"},
	}
	if p.Home != "" {
		out = append(out, serviceDir{filepath.Join(p.Home, "Library", "LaunchAgents"), "launchd-agent"})
	}
	return out
}

// Services reads launchd plists (daemons and agents) that mention mihomo,
// clash or tailscale.
func (p *Probe) Services(ctx context.Context) ([]takeover.ServiceDef, error) {
	run := p.Run
	if run == nil {
		run = darwin.ExecRunner(10 * time.Second)
	}
	var out []takeover.ServiceDef
	for _, d := range p.dirs() {
		plists, _ := filepath.Glob(filepath.Join(d.dir, "*.plist"))
		for _, pl := range plists {
			b, err := os.ReadFile(pl)
			if err != nil {
				continue
			}
			s := strings.ToLower(string(b))
			if !strings.Contains(s, "mihomo") && !strings.Contains(s, "clash") && !strings.Contains(s, "tailscale") {
				continue
			}
			js, err := run(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", pl)
			if err != nil {
				continue
			}
			var v struct {
				Label            string
				Program          string
				ProgramArguments []string
				UserName         string
			}
			if json.Unmarshal(js, &v) != nil {
				continue
			}
			prog := v.ProgramArguments
			if len(prog) == 0 && v.Program != "" {
				prog = []string{v.Program}
			}
			out = append(out, takeover.ServiceDef{Label: v.Label, Path: pl, Kind: d.kind, Program: prog, User: v.UserName})
		}
	}
	return out, nil
}

// MihomoVersion implements takeover.Probe (GET /version).
func (p *Probe) MihomoVersion(ctx context.Context, controller, secret string) (string, error) {
	c, err := mihomo.New(mihomo.Options{Address: controller, Secret: secret, Timeout: 3 * time.Second})
	if err != nil {
		return "", err
	}
	v, err := c.Version(ctx)
	if err != nil {
		return "", err
	}
	return v.Version, nil
}

// TailscaleStatus implements takeover.Probe.
func (p *Probe) TailscaleStatus(ctx context.Context) ([]byte, error) {
	if p.TailnetStatus != nil {
		return p.TailnetStatus(ctx)
	}
	bin := p.TailscaleBin
	if bin == "" {
		bin = FindTailscale()
	}
	if bin == "" {
		return nil, errors.New("tailscale CLI not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return nil, errors.New("tailscale status --json: " + err.Error())
	}
	return b, nil
}

// TailscaleInterface implements takeover.Probe.
func (p *Probe) TailscaleInterface(ctx context.Context) (string, error) {
	if p.TailnetIface != nil {
		return p.TailnetIface(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := darwin.New(darwin.Options{}).RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}

// TailscaleInstall implements takeover.Probe.
func (p *Probe) TailscaleInstall() tailscale.Install {
	return tailscale.DetectInstall(runtime.GOOS, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

// FindTailscale returns the tailscale CLI on PATH or in the usual places.
func FindTailscale() string {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	for _, p := range []string{
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
