// Package windows implements platform facts for Windows: the network
// adapters, the route table and the NRPT (through Sys: iphlpapi and the
// registry on Windows, fakes in tests), the Tailscale CLI, GET requests to
// the mihomo controller and plain file reads.
//
// The Env has no build tag: the OS calls sit behind Sys, so the Env is
// tested with fixtures on every OS.
package windows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/dnswire"
	"github.com/l3ony2k/twinnel/platform/common"
	"github.com/l3ony2k/twinnel/platform/egress"
)

// CommandRunner runs a command and returns its stdout. Tests replace it.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs commands for real with a per-command timeout, without a
// console window (the app is a GUI program; a console flashing up would
// steal focus).
func ExecRunner(timeout time.Duration) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		hideWindow(cmd)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if s := strings.TrimSpace(stderr.String()); s != "" {
				return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, s)
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
	// ConfigPath is the mihomo config file. Default: DefaultConfigPaths,
	// the first that can be read.
	ConfigPath string
	// DNSServer is mihomo's DNS listener. Default: from dns.listen, else
	// 127.0.0.1:53.
	DNSServer string
	// TailscaleBin is the tailscale CLI. Default: DefaultTailscaleBin, else
	// tailscale.exe on PATH.
	TailscaleBin string
	// TailscaleStatus, if set, replaces `tailscale status --json` (a
	// development helper started with --tailnet-status-file).
	TailscaleStatus func(ctx context.Context) ([]byte, error)
	// Settings are check preferences. Default: health.DefaultSettings().
	Settings *health.Settings

	// Sys reads adapters, routes and the NRPT. Default: NewSys().
	Sys      Sys
	HTTP     *http.Client
	ReadFile func(string) ([]byte, error)
	// Run runs the tailscale CLI. Default: ExecRunner.
	Run CommandRunner
	// LookupHost resolves with the OS resolver. Default:
	// net.DefaultResolver (GetAddrInfoW on Windows: NRPT, hosts, DNS client).
	LookupHost func(ctx context.Context, name string) ([]netip.Addr, error)
	// LookPath is exec.LookPath (replaceable for tests).
	LookPath func(string) (string, error)
	// Direct makes the egress test's direct request on a socket pinned to
	// ifc. Default: egress.Direct. Tests replace it.
	Direct func(ctx context.Context, url string, ifc egress.Iface) (netip.Addr, error)
}

// Env implements health.Env on Windows.
type Env struct {
	opt        Options
	readConfig func(string) ([]byte, error)

	cfgOnce   sync.Once
	cfg       *health.MihomoConfig
	cfgSecret string
	cfgErr    error
}

var _ health.Env = (*Env)(nil)

// DefaultConfigPaths are where the mihomo config is looked for: the one
// Twinnel's service uses, then the per-user default.
func DefaultConfigPaths() []string {
	var out []string
	if pd := os.Getenv("ProgramData"); pd != "" {
		out = append(out, filepath.Join(pd, "Twinnel", "mihomo", "config.yaml"))
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		out = append(out, filepath.Join(home, ".config", "mihomo", "config.yaml"))
	}
	return out
}

// DefaultTailscaleBin is the official client's CLI.
func DefaultTailscaleBin() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "Tailscale", "tailscale.exe")
}

// New returns an Env with defaults filled in.
func New(opt Options) *Env {
	if opt.Sys == nil {
		opt.Sys = NewSys()
	}
	if opt.Run == nil {
		opt.Run = ExecRunner(8 * time.Second)
	}
	if opt.HTTP == nil {
		// Never use a proxy for the local controller.
		opt.HTTP = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	}
	readConfig := opt.ReadFile
	if opt.ReadFile == nil {
		opt.ReadFile = os.ReadFile
		readConfig = config.ReadConfined
	}
	if opt.LookPath == nil {
		opt.LookPath = exec.LookPath
	}
	if opt.LookupHost == nil {
		opt.LookupHost = func(ctx context.Context, name string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", name)
		}
	}
	return &Env{opt: opt, readConfig: readConfig}
}

func (e *Env) Platform() string { return health.Windows }

func (e *Env) Settings() health.Settings {
	if e.opt.Settings != nil {
		return *e.opt.Settings
	}
	return health.DefaultSettings()
}

// ---- network --------------------------------------------------------------

func (e *Env) RouteGet(ctx context.Context, dest string) (health.RouteLookup, error) {
	d, err := netip.ParseAddr(dest)
	if err != nil {
		return health.RouteLookup{}, fmt.Errorf("route get %s: %w", dest, err)
	}
	routes, err := e.opt.Sys.Routes(ctx)
	if err != nil {
		return health.RouteLookup{}, err
	}
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return health.RouteLookup{}, err
	}
	r, a, ok := BestRoute(routes, ads, d)
	if !ok {
		return health.RouteLookup{}, fmt.Errorf("route get %s: no route", dest)
	}
	return routeLookup(dest, r, a), nil
}

// TailscaleInterface is the adapter holding the route to 100.100.100.100
// (normally "Tailscale").
func (e *Env) TailscaleInterface(ctx context.Context) (string, error) {
	r, err := e.RouteGet(ctx, "100.100.100.100")
	return r.Interface, err
}

func (e *Env) Routes(ctx context.Context) ([]health.Route, error) {
	routes, err := e.opt.Sys.Routes(ctx)
	if err != nil {
		return nil, err
	}
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	names := map[uint32]string{}
	for _, a := range ads {
		names[a.Index] = a.Name
	}
	out := make([]health.Route, 0, len(routes))
	for _, r := range routes {
		if !r.Prefix.IsValid() {
			continue
		}
		fam := 6
		if r.Prefix.Addr().Is4() {
			fam = 4
		}
		hr := health.Route{Family: fam, Destination: r.Prefix.String(), Interface: names[r.IfIndex]}
		if r.NextHop.IsValid() && !r.NextHop.IsUnspecified() {
			hr.Gateway = r.NextHop.String()
		}
		out = append(out, hr)
	}
	return out, nil
}

func (e *Env) Interfaces(ctx context.Context) ([]health.Interface, error) {
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]health.Interface, 0, len(ads))
	for _, a := range ads {
		out = append(out, health.Interface{Name: a.Name, Up: a.Up, Active: a.Up, Addrs: append([]netip.Addr(nil), a.Addrs...)})
	}
	return out, nil
}

func (e *Env) Resolvers(ctx context.Context) ([]health.Resolver, error) {
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := e.opt.Sys.Routes(ctx)
	if err != nil {
		return nil, err
	}
	nrpt, err := e.opt.Sys.NRPT(ctx)
	if err != nil {
		return nil, err
	}
	return resolversFrom(nrpt, routes, ads), nil
}

// NetworkServices returns the physical adapters (not loopback, not
// tunnels such as Tailscale's or mihomo's wintun), up ones first, ordered
// by interface metric. DNS holds the adapter's servers; DNSSource comes
// from the registry (NameServer set: manual, else DHCP).
func (e *Env) NetworkServices(ctx context.Context) ([]health.NetworkService, error) {
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	phys := physicalAdapters(ads)
	out := make([]health.NetworkService, 0, len(phys))
	for i, a := range phys {
		s := health.NetworkService{Name: a.Name, Device: a.Name, Enabled: a.Up, Order: i + 1, DNS: addrStrings(withoutDefaultV6DNS(a.DNS))}
		switch src := e.opt.Sys.DNSSource(a.GUID); {
		case src == health.DNSSourceAuto && len(s.DNS) == 0:
			s.DNSSource = health.DNSSourceGlobal
		case src != "":
			s.DNSSource = src
		case len(s.DNS) > 0:
			s.DNSSource = health.DNSSourceUnknown
		default:
			s.DNSSource = health.DNSSourceGlobal
		}
		out = append(out, s)
	}
	return out, nil
}

// VPNs returns the up tunnel adapters other than Tailscale's and mihomo's
// TUN. Windows has no system VPN slot: any such adapter is a VPN client
// carrying traffic next to the two.
func (e *Env) VPNs(ctx context.Context) ([]health.VPN, error) {
	ads, err := e.opt.Sys.Adapters(ctx)
	if err != nil {
		return nil, err
	}
	dev := ""
	if cfg, err := e.MihomoConfig(ctx); err == nil && cfg != nil {
		dev = cfg.TUN.Device
	}
	return vpnsFrom(ads, dev), nil
}

// ---- Tailscale ------------------------------------------------------------

func (e *Env) tailscaleBin() (string, error) {
	if e.opt.TailscaleBin != "" {
		return e.opt.TailscaleBin, nil
	}
	def := DefaultTailscaleBin()
	if _, err := os.Stat(def); err == nil {
		return def, nil
	}
	if p, err := e.opt.LookPath("tailscale.exe"); err == nil {
		return p, nil
	}
	return "", errors.New("tailscale CLI not found")
}

func (e *Env) Tailscale(ctx context.Context) (*health.TailscaleStatus, error) {
	if e.opt.TailscaleStatus != nil {
		out, err := e.opt.TailscaleStatus(ctx)
		if err != nil {
			return nil, err
		}
		return common.ParseTailscaleStatus(out)
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
	return common.ParseTailscaleStatus(out)
}

// ---- mihomo ---------------------------------------------------------------

func (e *Env) loadConfig() {
	e.cfgOnce.Do(func() {
		path := e.opt.ConfigPath
		if path == "" {
			cands := DefaultConfigPaths()
			for _, p := range cands {
				if _, err := e.opt.ReadFile(p); err == nil {
					path = p
					break
				}
			}
			if path == "" {
				e.cfgErr = fmt.Errorf("no mihomo config found (looked in %s)", strings.Join(cands, ", "))
				return
			}
		}
		b, err := e.readConfig(path)
		if err != nil {
			e.cfgErr = err
			return
		}
		e.cfg, e.cfgSecret, e.cfgErr = common.ParseMihomoConfigFile(path, b)
	})
}

func (e *Env) MihomoConfig(ctx context.Context) (*health.MihomoConfig, error) {
	e.loadConfig()
	return e.cfg, e.cfgErr
}

func (e *Env) controller() (addr, secret string) {
	e.loadConfig()
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
	addr, secret := e.controller()
	rt := &health.MihomoRuntime{AuthUsed: secret != ""}
	b, err := e.get(ctx, addr, secret, "/version")
	if err != nil {
		return nil, fmt.Errorf("mihomo controller %s: %w", addr, err)
	}
	if rt.Version, rt.Meta, err = common.ParseMihomoVersion(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/configs"); err != nil {
		return nil, err
	}
	if rt.Configs, err = common.ParseMihomoConfigs(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/proxies"); err != nil {
		return nil, err
	}
	if rt.Proxies, err = common.ParseMihomoProxies(b); err != nil {
		return nil, err
	}
	if b, err = e.get(ctx, addr, secret, "/rules"); err != nil {
		return nil, err
	}
	if rt.Rules, err = common.ParseMihomoRules(b); err != nil {
		return nil, err
	}
	return rt, nil
}

func (e *Env) dnsServer() string {
	if e.opt.DNSServer != "" {
		return e.opt.DNSServer
	}
	e.loadConfig()
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

const mihomoDNSTimeout = 2 * time.Second

func (e *Env) LookupMihomo(ctx context.Context, name string) (health.DNSAnswer, error) {
	server := e.dnsServer()
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

// LookupSystem resolves the way ordinary programs do on Windows (the DNS
// client: hosts file, NRPT, adapter DNS).
func (e *Env) LookupSystem(ctx context.Context, name string) (health.DNSAnswer, error) {
	addrs, err := e.opt.LookupHost(ctx, name)
	var de *net.DNSError
	if errors.As(err, &de) && de.IsNotFound {
		return health.DNSAnswer{Name: name, Rcode: "NXDOMAIN"}, nil
	}
	if err != nil {
		return health.DNSAnswer{Name: name}, err
	}
	ans := health.DNSAnswer{Name: name, Rcode: "NOERROR"}
	for _, a := range addrs {
		ans.Addrs = append(ans.Addrs, a.Unmap())
	}
	return ans, nil
}
