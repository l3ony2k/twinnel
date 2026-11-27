// Package linux implements platform facts for Linux using read-only
// commands (iproute2, resolvectl, nmcli, getent), the Tailscale CLI, GET
// requests to the mihomo controller and plain file reads.
//
// The package has no build tag: every command goes through a CommandRunner,
// so the parsers and the Env are tested with fixtures on any OS.
package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/health/dnswire"
	"github.com/l3ony2k/twinnel/platform/common"
)

// CommandRunner runs a command and returns its stdout. Tests replace it.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExitError is returned by ExecRunner when a command exits non-zero.
type ExitError struct {
	Name   string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: exit status %d: %s", e.Name, e.Code, e.Stderr)
	}
	return fmt.Sprintf("%s: exit status %d", e.Name, e.Code)
}

// ExecRunner runs commands for real with a per-command timeout.
func ExecRunner(timeout time.Duration) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, &ExitError{Name: filepath.Base(name), Code: ee.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
		}
		if err != nil {
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
	// ConfigPath is the mihomo config file. Default: discovered from a
	// systemd unit running mihomo, else /etc/mihomo/config.yaml, else
	// ~/.config/mihomo/config.yaml.
	ConfigPath string
	// DNSServer is mihomo's DNS listener. Default: from dns.listen, else
	// 127.0.0.1:53.
	DNSServer string
	// TailscaleBin is the tailscale CLI. Default: discovered.
	TailscaleBin string
	// TailscaleStatus, if set, replaces `tailscale status --json` (a
	// development helper started with --tailnet-status-file).
	TailscaleStatus func(ctx context.Context) ([]byte, error)
	// ResolvConf is the resolv.conf path. Default /etc/resolv.conf.
	ResolvConf string
	// UnitDirs are searched for a mihomo unit. Default: UnitDirs(home).
	UnitDirs []string
	// Settings are check preferences. Default: health.DefaultSettings().
	Settings *health.Settings
	// Dataplane runs the tun-dataplane probes (health.NetProbes on a real
	// machine). Nil: the check is skipped, so tests never dial out.
	Dataplane health.DataplaneEnv

	Run      CommandRunner
	HTTP     *http.Client
	ReadFile func(string) ([]byte, error)
	// LookPath is exec.LookPath (replaceable for tests).
	LookPath func(string) (string, error)
}

// Env implements health.Env on Linux.
type Env struct {
	opt        Options
	readConfig func(string) ([]byte, error)

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
	// The mihomo config lives in a user's dir and this may run as root:
	// read it without following symlinks out of its dir, unless a test
	// injects its own reader.
	readConfig := opt.ReadFile
	if opt.ReadFile == nil {
		opt.ReadFile = os.ReadFile
		readConfig = config.ReadConfined
	}
	if opt.LookPath == nil {
		opt.LookPath = exec.LookPath
	}
	if opt.ResolvConf == "" {
		opt.ResolvConf = "/etc/resolv.conf"
	}
	if opt.UnitDirs == nil {
		home, _ := os.UserHomeDir()
		opt.UnitDirs = UnitDirs(home)
	}
	return &Env{opt: opt, readConfig: readConfig}
}

func (e *Env) Platform() string { return health.Linux }

func (e *Env) Settings() health.Settings {
	if e.opt.Settings != nil {
		return *e.opt.Settings
	}
	return health.DefaultSettings()
}

func (e *Env) ip(ctx context.Context, args ...string) ([]byte, error) {
	return e.opt.Run(ctx, "ip", append([]string{"-j"}, args...)...)
}

func (e *Env) RouteGet(ctx context.Context, dest string) (health.RouteLookup, error) {
	out, err := e.ip(ctx, "route", "get", dest)
	if err != nil {
		return health.RouteLookup{}, err
	}
	r, err := ParseIPRouteGet(out)
	if err == nil && r.Interface == "" {
		err = fmt.Errorf("route get %s: no interface in output", dest)
	}
	return r, err
}

func (e *Env) Routes(ctx context.Context) ([]health.Route, error) {
	var all []health.Route
	for _, fam := range []int{4, 6} {
		out, err := e.ip(ctx, fmt.Sprintf("-%d", fam), "route", "show", "table", "all")
		if err != nil {
			if fam == 6 {
				continue // IPv6 disabled
			}
			return nil, err
		}
		rs, err := ParseIPRoutes(out, fam)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	return all, nil
}

func (e *Env) links(ctx context.Context) ([]Link, error) {
	out, err := e.ip(ctx, "addr", "show")
	if err != nil {
		return nil, err
	}
	return ParseIPAddr(out)
}

func (e *Env) Interfaces(ctx context.Context) ([]health.Interface, error) {
	ls, err := e.links(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]health.Interface, len(ls))
	for i, l := range ls {
		out[i] = l.Interface
	}
	return out, nil
}

// dnsState reads resolv.conf and, when it points at the resolved stub,
// `resolvectl status`.
func (e *Env) dnsState(ctx context.Context) (ResolvConf, []ResolvedLink, error) {
	b, err := e.opt.ReadFile(e.opt.ResolvConf)
	if err != nil {
		return ResolvConf{}, nil, err
	}
	rc := ParseResolvConf(string(b))
	if !rc.Stub() {
		return rc, nil, nil
	}
	out, err := e.opt.Run(ctx, "resolvectl", "status", "--no-pager")
	if err != nil {
		return rc, nil, fmt.Errorf("resolv.conf uses the systemd-resolved stub but resolvectl failed: %w", err)
	}
	return rc, ParseResolvectl(string(out)), nil
}

func (e *Env) Resolvers(ctx context.Context) ([]health.Resolver, error) {
	rc, links, err := e.dnsState(ctx)
	if err != nil {
		return nil, err
	}
	return ResolversFrom(rc, links), nil
}

// NetworkServices returns the physical links (not loopback, not tun
// devices such as tailscale0 or mihomo's TUN), ordered by default route
// metric, then by name. Name is the active NetworkManager connection when
// nmcli is available, else the device. DNS holds the servers that answer
// for the link: its own servers under systemd-resolved, else resolv.conf's.
func (e *Env) NetworkServices(ctx context.Context) ([]health.NetworkService, error) {
	ls, err := e.links(ctx)
	if err != nil {
		return nil, err
	}
	metric := map[string]int{}
	if out, err := e.ip(ctx, "-4", "route", "show", "default"); err == nil {
		if rs, err := ParseIPRoutes(out, 4); err == nil {
			for i, r := range rs {
				if _, ok := metric[r.Interface]; !ok {
					metric[r.Interface] = i
				}
			}
		}
	}
	names := map[string]string{}
	uuids := map[string]string{}
	for _, c := range e.nmConnections(ctx) {
		if c.Device != "" && c.State == "activated" {
			names[c.Device] = c.Name
			uuids[c.Device] = c.UUID
		}
	}
	rc, rlinks, _ := e.dnsState(ctx)
	linkDNS := map[string][]string{}
	for _, l := range rlinks {
		if l.Index > 0 {
			linkDNS[l.Name] = l.Servers
		}
	}

	var phys []Link
	for _, l := range ls {
		if l.Physical() {
			phys = append(phys, l)
		}
	}
	sort.SliceStable(phys, func(i, j int) bool {
		mi, iok := metric[phys[i].Name]
		mj, jok := metric[phys[j].Name]
		if iok != jok {
			return iok
		}
		if iok && mi != mj {
			return mi < mj
		}
		return phys[i].Name < phys[j].Name
	})
	out := make([]health.NetworkService, 0, len(phys))
	for i, l := range phys {
		s := health.NetworkService{Name: l.Name, Device: l.Name, Enabled: l.Up, Order: i + 1}
		if n := names[l.Name]; n != "" {
			s.Name = n
		}
		if rlinks != nil {
			s.DNS = linkDNS[l.Name]
		} else {
			s.DNS = rc.Nameservers
		}
		s.DNSSource = e.dnsSource(ctx, uuids[l.Name], rlinks != nil, len(s.DNS) > 0)
		out = append(out, s)
	}
	return out, nil
}

// dnsSource tells the system-dns check where a link's DNS comes from:
// NetworkManager's connection settings when the link has an active
// connection, else only what resolved/resolv.conf show (per-link servers of
// unknown origin, or the global ones).
func (e *Env) dnsSource(ctx context.Context, uuid string, resolved, perLink bool) string {
	if uuid != "" {
		out, err := e.opt.Run(ctx, "nmcli", "-g", NMCLIDNSFields, "connection", "show", "uuid", uuid)
		if err == nil {
			if src := ParseNmcliDNSSource(string(out)); src != "" {
				if src == health.DNSSourceAuto && !perLink {
					return health.DNSSourceGlobal // DHCP gave no servers
				}
				return src
			}
		}
	}
	if resolved && perLink {
		return health.DNSSourceUnknown
	}
	return health.DNSSourceGlobal
}

func (e *Env) nmConnections(ctx context.Context) []NMConnection {
	if _, err := e.opt.LookPath("nmcli"); err != nil {
		return nil
	}
	out, err := e.opt.Run(ctx, "nmcli", "-t", "-f", "NAME,UUID,TYPE,DEVICE,STATE", "connection", "show")
	if err != nil {
		return nil
	}
	return ParseNmcliConnections(string(out))
}

// VPNs returns NetworkManager VPN and WireGuard connections. Without
// NetworkManager there is no system VPN registry; the vpn-conflict check
// then relies on the default route alone.
func (e *Env) VPNs(ctx context.Context) ([]health.VPN, error) {
	if _, err := e.opt.LookPath("nmcli"); err != nil {
		return nil, nil
	}
	out, err := e.opt.Run(ctx, "nmcli", "-t", "-f", "NAME,UUID,TYPE,DEVICE,STATE", "connection", "show")
	if err != nil {
		return nil, err
	}
	return VPNsFromNM(ParseNmcliConnections(string(out))), nil
}

func (e *Env) tailscaleBin() (string, error) {
	if e.opt.TailscaleBin != "" {
		return e.opt.TailscaleBin, nil
	}
	if p, err := e.opt.LookPath("tailscale"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale", "/usr/sbin/tailscale"} {
		if _, err := e.opt.LookPath(p); err == nil {
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

func (e *Env) loadConfig(ctx context.Context) {
	e.cfgOnce.Do(func() {
		path := e.opt.ConfigPath
		if path == "" {
			path = DiscoverConfigPath(e.opt.UnitDirs, e.opt.ReadFile)
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

// LookupSystem resolves through NSS (`getent ahostsv4`), i.e. the path
// ordinary programs take: /etc/hosts, resolved or resolv.conf.
func (e *Env) LookupSystem(ctx context.Context, name string) (health.DNSAnswer, error) {
	out, err := e.opt.Run(ctx, "getent", "ahostsv4", name)
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code == 2 { // key not found
		return health.DNSAnswer{Name: name, Rcode: "NXDOMAIN"}, nil
	}
	if err != nil {
		return health.DNSAnswer{Name: name}, err
	}
	return ParseGetent(name, string(out)), nil
}
