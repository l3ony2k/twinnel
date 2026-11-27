package engine

// The helper's part of a fresh setup (core/setup drives it): the system
// DNS (dns.status/point/restore), the Tailscale install
// (tailscale.install) and the download proxy of kernel.install and
// tailscale.install.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/sysdns"
)

// DNSTarget is where dns.point sends the system's queries: mihomo's DNS
// listener of the built-in base settings (dns.listen 127.0.0.1:53).
var DNSTarget = []string{"127.0.0.1"}

var tsInstallMu sync.Mutex

func (e *Engine) dnsManager() *sysdns.Manager {
	return &sysdns.Manager{P: e.SystemDNS, Path: e.Store.Path("dns-backup.json"), Now: e.now}
}

func toDNSStatus(st sysdns.Status) rpc.DNSStatus {
	out := rpc.DNSStatus{Target: DNSTarget, Pointed: st.Pointed, Changed: st.Changed, Supported: true, Services: []rpc.DNSService{}}
	for _, s := range st.Services {
		out.Services = append(out.Services, rpc.DNSService{Name: s.Name, Servers: s.Servers})
	}
	if st.Backup != nil {
		out.BackupAt = st.Backup.At
		for _, s := range st.Backup.Services {
			out.Backup = append(out.Backup, rpc.DNSService{Name: s.Name, Servers: s.Servers})
		}
	}
	return out
}

// DNSStatus reads the system DNS.
func (e *Engine) DNSStatus(ctx context.Context) (rpc.DNSStatus, error) {
	if e.SystemDNS == nil {
		return rpc.DNSStatus{Target: DNSTarget, Services: []rpc.DNSService{}}, nil
	}
	st, err := e.dnsManager().Status(ctx, DNSTarget)
	if err != nil {
		return rpc.DNSStatus{}, err
	}
	return toDNSStatus(st), nil
}

// probeDNS reports whether a DNS server answers at addr: any answer,
// NXDOMAIN included, counts.
func (e *Engine) probeDNS(ctx context.Context, addr string) error {
	if e.DNSProbe != nil {
		return e.DNSProbe(ctx, addr)
	}
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := r.LookupHost(ctx, "twinnel-dns-probe.example.com")
	var de *net.DNSError
	if err == nil || errors.As(err, &de) && de.IsNotFound {
		return nil
	}
	return err
}

// DNSPoint points the system DNS at mihomo. It refuses while mihomo's DNS
// does not answer: a system resolver nobody serves cuts the network.
func (e *Engine) DNSPoint(ctx context.Context) (rpc.DNSStatus, error) {
	if e.SystemDNS == nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotImplemented, "setting the system DNS is not supported on this platform yet")
	}
	addr := net.JoinHostPort(DNSTarget[0], "53")
	if err := e.probeDNS(ctx, addr); err != nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeFailed, "mihomo's DNS does not answer at %s (%v); the system DNS was not changed", addr, err)
	}
	st, err := e.dnsManager().Point(ctx, DNSTarget)
	out := toDNSStatus(st)
	e.log().Info("system DNS pointed at mihomo", "changed", len(st.Changed), "err", err)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	e.triggerHealth("dns point")
	return out, nil
}

// DNSRestore puts the system DNS back as it was before DNSPoint.
func (e *Engine) DNSRestore(ctx context.Context) (rpc.DNSStatus, error) {
	if e.SystemDNS == nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotImplemented, "setting the system DNS is not supported on this platform yet")
	}
	st, err := e.dnsManager().Restore(ctx)
	if errors.Is(err, sysdns.ErrNoBackup) {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotFound, "%v", err)
	}
	out := toDNSStatus(st)
	e.log().Info("system DNS restored", "changed", len(st.Changed), "err", err)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	e.triggerHealth("dns restore")
	return out, nil
}

// downloadVia returns the HTTP client for a download proxy choice, how it
// goes, and a stop function (the temporary mihomo of ProxySubscription).
func (e *Engine) downloadVia(ctx context.Context, st *store.Settings, proxy string) (*http.Client, string, func(), error) {
	nop := func() {}
	switch proxy {
	case rpc.ProxyAuto:
		cl, via := e.downloadClient(ctx, st)
		return cl, via, nop, nil
	case rpc.ProxyDirect:
		return e.httpClient(nil), "direct", nop, nil
	case rpc.ProxySubscription:
		u, stop, err := e.subscriptionProxy(ctx, st)
		if err != nil {
			return nil, "", nop, err
		}
		return e.httpClient(u), "subscription", stop, nil
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
		return nil, "", nop, rpc.ErrInvalid("proxy must be %q, %q, %q or an http://, https:// or socks5:// URL", rpc.ProxyDirect, rpc.ProxySubscription, "auto")
	}
	return e.httpClient(u), "url", nop, nil
}

// TailscaleInstall installs the open-source tailscaled and its system
// service, unless a Tailscale install is already there (kept as it is,
// whatever kind: Twinnel never installs or replaces the macOS app).
func (e *Engine) TailscaleInstall(ctx context.Context, p rpc.TailscaleInstallParams) (rpc.TailscaleInstallResult, error) {
	var out rpc.TailscaleInstallResult
	if !tsInstallMu.TryLock() {
		return out, rpc.Errorf(rpc.CodeBusy, "a Tailscale install is already running")
	}
	defer tsInstallMu.Unlock()
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	probe := e.TSProbe
	if probe == nil {
		probe = &kernel.TailscaleProbe{}
	}
	in := e.tsInstaller()
	_, managedDaemon := in.Binaries()
	if existing := probe.FindExisting(); existing != "" {
		out.Kept, out.Path, out.Kind = true, existing, probe.Kind(existing)
		out.UsesSystemVPN = out.Kind == kernel.TSStandalone || out.Kind == kernel.TSAppStore
		if v, err := probe.LocalVersion(ctx, existing); err == nil {
			out.Version = v
		}
		e.log().Info("tailscale install kept", "kind", out.Kind)
		return out, nil
	}
	if cur := in.Current(); cur != "" && fileExists(managedDaemon) {
		// Installed by an earlier run: only make sure the service is there.
		cli, daemon := in.Binaries()
		out.Kept, out.Kind, out.Path, out.Version, out.Daemon = true, "twinnel", cli, cur, daemon
	} else {
		if e.Services == nil {
			return out, rpc.Errorf(rpc.CodeNotImplemented, "no service manager on this platform")
		}
		cl, via, stop, err := e.downloadVia(ctx, st, p.Proxy)
		if err != nil {
			return out, err
		}
		defer stop()
		in.HTTP = cl
		out.Via = via
		a, err := in.Resolve(ctx)
		if err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
		}
		got, err := in.Install(ctx, a)
		if err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
		}
		out.Version, out.Source, out.Path, out.Daemon, out.Kind = got.Version, a.Source, got.CLI, got.Daemon, "twinnel"
		e.log().Info("tailscale installed", "version", got.Version, "source", a.Source, "via", via)
	}
	if _, err := e.Store.UpdateSettings(func(s *store.Settings) error {
		s.Tailscale.Binary, s.Tailscale.DaemonBinary, s.Tailscale.ServiceAdopted = out.Path, out.Daemon, false
		return nil
	}); err != nil {
		return out, err
	}
	svc, err := e.installTailscaled(ctx, st.Tailscale.ServiceLabel, out.Daemon)
	out.Service = svc
	if err != nil {
		return out, err
	}
	e.publish(rpc.EventServiceChanged, map[string]any{"name": "tailscaled", "op": "install", "dry_run": svc.DryRun})
	e.triggerHealth("tailscale install")
	return out, nil
}

func (e *Engine) tsInstaller() *kernel.TailscaleInstaller {
	if e.TSInstaller != nil {
		in := *e.TSInstaller
		return &in
	}
	return &kernel.TailscaleInstaller{Dir: e.Store.Path("tailscale")}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// installTailscaled installs the tailscaled service definition (like
// `tailscaled install-system-daemon`) and starts it.
func (e *Engine) installTailscaled(ctx context.Context, label, daemon string) (*rpc.ServiceOpResult, error) {
	out := &rpc.ServiceOpResult{Name: "tailscaled", Label: label, Op: string(service.OpInstall), Steps: []string{}}
	if e.Services == nil {
		return out, rpc.Errorf(rpc.CodeNotImplemented, "no service manager on this platform")
	}
	if geteuid() == 0 {
		if err := kernel.RequireTrusted(daemon); err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "refusing to install a root service for %v", err)
		}
	}
	steps, err := e.Services.Plan(service.OpInstall, service.TailscaledSpec(label, daemon))
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	for _, s := range steps {
		out.Steps = append(out.Steps, s.String())
	}
	r := e.ServiceRunner
	if r == nil || r.DryRun || r.Exec == nil || r.Files == nil {
		out.DryRun = true
	} else {
		e.serviceMu.Lock()
		err = service.Apply(ctx, steps, r.Exec, r.Files)
		e.serviceMu.Unlock()
		if err != nil {
			out.Status, _ = e.ServiceStatus(ctx, "tailscaled")
			return out, rpc.Errorf(rpc.CodeFailed, "install tailscaled: %v", err)
		}
	}
	out.Status, _ = e.ServiceStatus(ctx, "tailscaled")
	return out, nil
}

// seedKernel is a mihomo binary shipped with the app for a machine that
// cannot download one yet (placed by the install; see docs/new-machine.md).
func (e *Engine) seedKernel() string {
	if e.Kernels == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(e.Kernels.Dir), "seed", "mihomo")
}

// tempProxyBinary picks the mihomo the temporary proxy runs: the managed
// kernel, an adopted binary, or the seed.
func (e *Engine) tempProxyBinary(st *store.Settings) (string, error) {
	var cands []string
	if e.Kernels != nil {
		cands = append(cands, e.Kernels.Binary())
	}
	if st.Mihomo.Binary != "" {
		cands = append(cands, st.Mihomo.Binary)
	}
	if s := e.seedKernel(); s != "" {
		cands = append(cands, s)
	}
	for _, c := range cands {
		if fileExists(c) {
			return c, nil
		}
	}
	return "", rpc.Errorf(rpc.CodeFailed, "no mihomo binary to run the subscription's proxy with yet: download the kernel through another proxy (an http:// or socks5:// address) first")
}

// subscriptionProxy starts a temporary mihomo with the composed config of
// the active subscription (TUN, DNS and the controller off, one mixed port
// on loopback) and returns its address.
func (e *Engine) subscriptionProxy(ctx context.Context, st *store.Settings) (*url.URL, func(), error) {
	bin, err := e.tempProxyBinary(st)
	if err != nil {
		return nil, nil, err
	}
	res, _, err := e.compose(ctx, st)
	if err != nil {
		return nil, nil, rpc.Errorf(rpc.CodeFailed, "compose the subscription's config: %v", err)
	}
	port, err := freePort()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := TempProxyConfig(res.Config, port)
	if err != nil {
		return nil, nil, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	start := e.TempProxy
	if start == nil {
		start = startTempMihomo
	}
	stop, err := start(ctx, bin, cfg, port)
	if err != nil {
		return nil, nil, rpc.Errorf(rpc.CodeFailed, "start the subscription's proxy: %v", err)
	}
	e.log().Info("temporary proxy started", "port", port)
	return &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}, stop, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// errNoProxy: the composed config has no proxy group or proxy to send
// downloads through.
var errNoProxy = errors.New("the subscription has no proxy group or proxy")

// TailscaleControl is the part of tailscaled's LocalAPI tailscale.login
// uses (cmd/helper adapts core/tailscale; tests fake it).
type TailscaleControl interface {
	// LoginState returns the backend state, the login URL while one is
	// pending, and the tailnet's name.
	LoginState(ctx context.Context) (backend, authURL, tailnet string, err error)
	// Up sets WantRunning; StartLogin asks for an interactive login.
	Up(ctx context.Context) error
	StartLogin(ctx context.Context) error
}

// TailscaleLogin reads the login state and, with Start, connects and starts
// an interactive login unless the node already runs.
func (e *Engine) TailscaleLogin(ctx context.Context, p rpc.TailscaleLoginParams) (rpc.TailscaleLoginResult, error) {
	var out rpc.TailscaleLoginResult
	if e.TSControl == nil {
		return out, rpc.Errorf(rpc.CodeNotImplemented, "no Tailscale LocalAPI on this helper")
	}
	backend, authURL, tailnet, err := e.TSControl.LoginState(ctx)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "tailscaled: %v", err)
	}
	if p.Start && backend != "Running" {
		if err := e.TSControl.Up(ctx); err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "tailscale up: %v", err)
		}
		if err := e.TSControl.StartLogin(ctx); err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "tailscale login: %v", err)
		}
		out.Started = true
		e.log().Info("tailscale login started")
		if backend, authURL, tailnet, err = e.TSControl.LoginState(ctx); err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "tailscaled: %v", err)
		}
	}
	out.BackendState, out.AuthURL, out.Tailnet = backend, authURL, tailnet
	return out, nil
}
