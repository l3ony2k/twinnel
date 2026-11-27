//go:build darwin

// Package darwin implements platform facts for macOS using read-only system
// commands, the Tailscale CLI and GET requests to the mihomo controller.
package darwin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/dnswire"
)

// CommandRunner runs a command and returns its stdout. Tests replace it.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs commands for real with a per-command timeout.
func ExecRunner(timeout time.Duration) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, msg)
			}
			return out, fmt.Errorf("%s: %w", filepath.Base(name), err)
		}
		return out, nil
	}
}

// Options configure an Env. Zero values are discovered.
type Options struct {
	// Controller is the mihomo controller "host:port". Default: the
	// config's external-controller, else 127.0.0.1:9090.
	Controller string
	// Secret overrides the controller secret read from the config.
	Secret string
	// ConfigPath is the mihomo config file. Default: discovered from the
	// mihomo LaunchDaemon, else ~/.config/mihomo/config.yaml.
	ConfigPath string
	// DNSServer is mihomo's DNS listener. Default: from the config's
	// dns.listen, else 127.0.0.1:53.
	DNSServer string
	// TailscaleBin is the tailscale CLI. Default: discovered.
	TailscaleBin string
	// TailscaleStatus, if set, replaces `tailscale status --json` (a
	// development helper started with --tailnet-status-file).
	TailscaleStatus func(ctx context.Context) ([]byte, error)
	// Settings are check preferences. Default: health.DefaultSettings().
	Settings *health.Settings

	Run  CommandRunner
	HTTP *http.Client
}

// Env implements health.Env on macOS.
type Env struct {
	opt Options

	cfgOnce   sync.Once
	cfg       *health.MihomoConfig
	cfgSecret string
	cfgErr    error
}

var _ health.Env = (*Env)(nil)

// New returns an Env with defaults filled in.
func New(opt Options) *Env {
	if opt.Run == nil {
		opt.Run = ExecRunner(8 * time.Second)
	}
	if opt.HTTP == nil {
		// Never use a proxy for the local controller.
		opt.HTTP = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	}
	return &Env{opt: opt}
}

func (e *Env) Platform() string { return health.Darwin }

func (e *Env) Settings() health.Settings {
	if e.opt.Settings != nil {
		return *e.opt.Settings
	}
	return health.DefaultSettings()
}

func (e *Env) run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := e.opt.Run(ctx, name, args...)
	return string(out), err
}

func (e *Env) RouteGet(ctx context.Context, dest string) (health.RouteLookup, error) {
	args := []string{"-n", "get"}
	if strings.Contains(dest, ":") {
		args = append(args, "-inet6")
	}
	out, err := e.run(ctx, "/sbin/route", append(args, dest)...)
	if err != nil {
		return health.RouteLookup{}, err
	}
	r := ParseRouteGet(out)
	if r.Interface == "" {
		return r, fmt.Errorf("route get %s: no interface in output", dest)
	}
	return r, nil
}

func (e *Env) Routes(ctx context.Context) ([]health.Route, error) {
	out, err := e.run(ctx, "/usr/sbin/netstat", "-rn")
	if err != nil {
		return nil, err
	}
	return ParseNetstat(out), nil
}

func (e *Env) Interfaces(ctx context.Context) ([]health.Interface, error) {
	out, err := e.run(ctx, "/sbin/ifconfig")
	if err != nil {
		return nil, err
	}
	return ParseIfconfig(out), nil
}

func (e *Env) Resolvers(ctx context.Context) ([]health.Resolver, error) {
	out, err := e.run(ctx, "/usr/sbin/scutil", "--dns")
	if err != nil {
		return nil, err
	}
	return ParseScutilDNS(out), nil
}

func (e *Env) NetworkServices(ctx context.Context) ([]health.NetworkService, error) {
	out, err := e.run(ctx, "/usr/sbin/networksetup", "-listnetworkserviceorder")
	if err != nil {
		return nil, err
	}
	svcs := ParseServiceOrder(out)
	for i := range svcs {
		if svcs[i].Device == "" {
			continue
		}
		dns, err := e.run(ctx, "/usr/sbin/networksetup", "-getdnsservers", svcs[i].Name)
		if err != nil {
			return nil, err
		}
		svcs[i].DNS = ParseGetDNSServers(dns)
	}
	return svcs, nil
}

func (e *Env) VPNs(ctx context.Context) ([]health.VPN, error) {
	out, err := e.run(ctx, "/usr/sbin/scutil", "--nc", "list")
	if err != nil {
		return nil, err
	}
	return ParseNCList(out), nil
}

// tailscaleBin finds the tailscale CLI.
func (e *Env) tailscaleBin() (string, error) {
	if e.opt.TailscaleBin != "" {
		return e.opt.TailscaleBin, nil
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	for _, p := range []string{
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("tailscale CLI not found")
}

func (e *Env) Tailscale(ctx context.Context) (*health.TailscaleStatus, error) {
	if e.opt.TailscaleStatus != nil {
		out, err := e.opt.TailscaleStatus(ctx)
		if err != nil {
			return nil, err
		}
		return ParseTailscaleStatus(out)
	}
	bin, err := e.tailscaleBin()
	if err != nil {
		return nil, err
	}
	out, err := e.opt.Run(ctx, bin, "status", "--json")
	if err != nil && len(out) == 0 {
		return nil, err
	}
	// `tailscale status --json` exits non-zero when stopped but still
	// prints the status.
	return ParseTailscaleStatus(out)
}

// ---- mihomo ---------------------------------------------------------------

// DiscoverConfigPath finds the config file of the mihomo LaunchDaemon by
// reading its plist (read-only), falling back to ~/.config/mihomo.
func DiscoverConfigPath(ctx context.Context, run CommandRunner) string {
	plists, _ := filepath.Glob("/Library/LaunchDaemons/*.plist")
	for _, p := range plists {
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), "mihomo") {
			continue
		}
		out, err := run(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", p)
		if err != nil {
			continue
		}
		var pl struct {
			ProgramArguments []string
		}
		if json.Unmarshal(out, &pl) != nil || len(pl.ProgramArguments) == 0 {
			continue
		}
		if path := ConfigFromArgs(pl.ProgramArguments); path != "" {
			return path
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mihomo", "config.yaml")
}

// ConfigFromArgs returns the config file named by mihomo's -f / -d flags.
func ConfigFromArgs(args []string) string {
	if !strings.Contains(filepath.Base(args[0]), "mihomo") {
		return ""
	}
	dir, file := "", ""
	for i := 1; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		val := ""
		if k, v, ok := strings.Cut(a, "="); ok {
			a, val = k, v
		} else if i+1 < len(args) {
			val = args[i+1]
		}
		switch a {
		case "f":
			file = val
		case "d":
			dir = val
		}
	}
	switch {
	case file != "" && !filepath.IsAbs(file) && dir != "":
		return filepath.Join(dir, file)
	case file != "":
		return file
	case dir != "":
		return filepath.Join(dir, "config.yaml")
	}
	return ""
}

func (e *Env) loadConfig(ctx context.Context) {
	e.cfgOnce.Do(func() {
		path := e.opt.ConfigPath
		if path == "" {
			path = DiscoverConfigPath(ctx, e.opt.Run)
		}
		// The config dir belongs to a user and this may run as root: do not
		// follow a symlink out of it.
		b, err := config.ReadConfined(path)
		if err != nil {
			e.cfgErr = err
			return
		}
		e.cfg, e.cfgSecret, e.cfgErr = ParseMihomoConfigFile(path, b)
	})
}

func (e *Env) MihomoConfig(ctx context.Context) (*health.MihomoConfig, error) {
	e.loadConfig(ctx)
	return e.cfg, e.cfgErr
}

func (e *Env) controller(ctx context.Context) (addr, secret string) {
	e.loadConfig(ctx)
	addr, secret = e.opt.Controller, e.opt.Secret
	if addr == "" && e.cfg != nil && e.cfg.ExternalController != "" {
		addr = e.cfg.ExternalController
		if h, p, err := net.SplitHostPort(addr); err == nil && (h == "" || h == "0.0.0.0" || h == "::") {
			addr = net.JoinHostPort("127.0.0.1", p)
		}
	}
	if addr == "" {
		addr = "127.0.0.1:9090"
	}
	if secret == "" {
		secret = e.cfgSecret
	}
	return addr, secret
}

func (e *Env) get(ctx context.Context, addr, secret, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return nil, err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := e.opt.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return body, nil
}

func (e *Env) MihomoRuntime(ctx context.Context) (*health.MihomoRuntime, error) {
	addr, secret := e.controller(ctx)
	rt := &health.MihomoRuntime{AuthUsed: secret != ""}
	b, err := e.get(ctx, addr, secret, "/version")
	if err != nil {
		return nil, fmt.Errorf("mihomo controller %s: %w", addr, err)
	}
	if rt.Version, rt.Meta, err = ParseMihomoVersion(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/configs"); err != nil {
		return nil, err
	}
	if rt.Configs, err = ParseMihomoConfigs(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/proxies"); err != nil {
		return nil, err
	}
	if rt.Proxies, err = ParseMihomoProxies(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/rules"); err != nil {
		return nil, err
	}
	if rt.Rules, err = ParseMihomoRules(b); err != nil {
		return nil, err
	}
	return rt, nil
}

func (e *Env) dnsServer(ctx context.Context) string {
	if e.opt.DNSServer != "" {
		return e.opt.DNSServer
	}
	e.loadConfig(ctx)
	if e.cfg != nil && e.cfg.DNS.Listen != "" {
		if h, p, err := net.SplitHostPort(e.cfg.DNS.Listen); err == nil {
			if h == "" || h == "0.0.0.0" || h == "::" {
				h = "127.0.0.1"
			}
			return net.JoinHostPort(h, p)
		}
	}
	return "127.0.0.1:53"
}

// mihomoDNSTimeout bounds one query to mihomo's DNS. A healthy local answer
// takes milliseconds; mihomo answers SERVFAIL only after several seconds
// when an upstream loops back to itself.
const mihomoDNSTimeout = 2 * time.Second

func (e *Env) LookupMihomo(ctx context.Context, name string) (health.DNSAnswer, error) {
	server := e.dnsServer(ctx)
	ctx, cancel := context.WithTimeout(ctx, mihomoDNSTimeout)
	defer cancel()
	a, err := dnswire.Query(ctx, server, name, dnswire.TypeA)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return health.DNSAnswer{Name: name}, fmt.Errorf("%w (%s, %s)", health.ErrDNSTimeout, server, mihomoDNSTimeout)
	}
	if err != nil {
		return health.DNSAnswer{Name: name}, err
	}
	return health.DNSAnswer{Name: name, Rcode: a.Rcode, Addrs: a.Addrs}, nil
}

func (e *Env) LookupSystem(ctx context.Context, name string) (health.DNSAnswer, error) {
	out, err := e.run(ctx, "/usr/bin/dscacheutil", "-q", "host", "-a", "name", name)
	if err != nil {
		return health.DNSAnswer{Name: name}, err
	}
	return ParseDscacheutil(name, out), nil
}
