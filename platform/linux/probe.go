package linux

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
	"github.com/l3ony2k/duet/core/takeover"
	"github.com/l3ony2k/duet/platform/linux/systemd"
)

// UnitDirs are the unit directories in systemd's precedence order (an
// earlier directory hides a unit of the same name in a later one): system
// units, then the user's own units under home.
func UnitDirs(home string) []string {
	dirs := []string{"/etc/systemd/system", "/run/systemd/system", "/usr/local/lib/systemd/system",
		"/usr/lib/systemd/system", "/lib/systemd/system"}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".config", "systemd", "user"))
	}
	return dirs
}

func kindOf(dir string) string {
	if strings.Contains(dir, filepath.Join("systemd", "user")) {
		return "systemd-user"
	}
	return "systemd"
}

// ScanUnits reads *.service files in dirs that mention mihomo, clash or
// tailscale and returns them as takeover.ServiceDefs. Read-only; drop-ins
// and templates (name@.service) are not resolved. A unit hidden by one of
// the same name in an earlier directory is skipped, as systemd does.
func ScanUnits(dirs []string, readFile func(string) ([]byte, error)) []takeover.ServiceDef {
	if readFile == nil {
		readFile = os.ReadFile
	}
	seen := map[string]bool{}
	var out []takeover.ServiceDef
	for _, d := range dirs {
		paths, _ := filepath.Glob(filepath.Join(d, "*.service"))
		sort.Strings(paths)
		for _, p := range paths {
			name := filepath.Base(p)
			if seen[name] || strings.Contains(name, "@") {
				continue
			}
			seen[name] = true
			b, err := readFile(p)
			if err != nil {
				continue
			}
			s := strings.ToLower(string(b))
			if !strings.Contains(s, "mihomo") && !strings.Contains(s, "clash") && !strings.Contains(s, "tailscale") {
				continue
			}
			u := systemd.ParseUnitFile(b)
			argv := systemd.SplitExec(u.Get("Service", "ExecStart"))
			if len(argv) == 0 {
				continue
			}
			out = append(out, takeover.ServiceDef{
				Label: strings.TrimSuffix(name, ".service"), Path: p, Kind: kindOf(d),
				Program: argv, User: u.Get("Service", "User"),
			})
		}
	}
	return out
}

// DiscoverConfigPath finds the config file of a systemd unit running mihomo
// (from its -d / -f flags), else /etc/mihomo/config.yaml when it exists,
// else ~/.config/mihomo/config.yaml.
func DiscoverConfigPath(dirs []string, readFile func(string) ([]byte, error)) string {
	if readFile == nil {
		readFile = os.ReadFile
	}
	for _, s := range ScanUnits(dirs, readFile) {
		base := strings.ToLower(filepath.Base(s.Program[0]))
		if !strings.Contains(base, "mihomo") && !strings.Contains(base, "clash") {
			continue
		}
		if _, file := takeover.ConfigFromArgs(s.Program); file != "" {
			return file
		}
	}
	if _, err := readFile("/etc/mihomo/config.yaml"); err == nil {
		return "/etc/mihomo/config.yaml"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mihomo", "config.yaml")
}

// Probe is the read-only takeover.Probe for Linux (systemd).
type Probe struct {
	Home     string
	UnitDirs []string
	Run      CommandRunner
	ReadFile func(string) ([]byte, error)
	Stat     func(string) (os.FileInfo, error)
	// TailscaleBin is the tailscale CLI. Default: discovered.
	TailscaleBin string
}

var _ takeover.Probe = (*Probe)(nil)

// NewProbe returns a Probe for the current user.
func NewProbe() *Probe {
	home, _ := os.UserHomeDir()
	return &Probe{Home: home, UnitDirs: UnitDirs(home), Run: ExecRunner(10 * time.Second),
		ReadFile: os.ReadFile, Stat: os.Stat}
}

func (p *Probe) UserHome() string { return p.Home }

// Services implements takeover.Probe.
func (p *Probe) Services(ctx context.Context) ([]takeover.ServiceDef, error) {
	return ScanUnits(p.UnitDirs, p.ReadFile), nil
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

func (p *Probe) env() *Env {
	return New(Options{Run: p.Run, TailscaleBin: p.TailscaleBin, ReadFile: p.ReadFile, UnitDirs: p.UnitDirs})
}

// TailscaleStatus implements takeover.Probe (`tailscale status --json`).
func (p *Probe) TailscaleStatus(ctx context.Context) ([]byte, error) {
	e := p.env()
	bin, err := e.tailscaleBin()
	if err != nil {
		return nil, err
	}
	out, err := p.Run(ctx, bin, "status", "--json")
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return out, nil
}

// TailscaleInterface implements takeover.Probe: the device `ip route get
// 100.100.100.100` picks (usually tailscale0).
func (p *Probe) TailscaleInterface(ctx context.Context) (string, error) {
	r, err := p.env().RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}

// TailscaleInstall implements takeover.Probe.
func (p *Probe) TailscaleInstall() tailscale.Install {
	stat := p.Stat
	if stat == nil {
		stat = os.Stat
	}
	return tailscale.DetectInstall("linux", func(path string) bool {
		_, err := stat(path)
		return err == nil
	})
}
