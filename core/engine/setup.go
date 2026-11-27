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
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/binsane"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/sysdns"
)

// dnsTarget is what dns.point sets (sysdns.TargetOf). Its first entry is
// mihomo's listener (what dns.point probes).
func (e *Engine) dnsTarget() []string { return sysdns.TargetOf(e.SystemDNS) }

var tsInstallMu sync.Mutex

// dnsBackupFile keeps what the services used before dns.point.
const dnsBackupFile = "dns-backup.json"

func (e *Engine) dnsManager() *sysdns.Manager {
	return &sysdns.Manager{P: e.SystemDNS, Path: e.Store.Path(dnsBackupFile), Target: e.dnsTarget(), Now: e.now}
}

func toDNSStatus(st sysdns.Status, target []string) rpc.DNSStatus {
	out := rpc.DNSStatus{Target: target, Pointed: st.Pointed, Changed: st.Changed, Supported: true, Services: []rpc.DNSService{},
		Unprotected: st.Unprotected, Recorded: st.Recorded}
	for _, s := range st.Services {
		out.Services = append(out.Services, rpc.DNSService{Name: s.Name, Servers: s.Servers, Connected: s.Connected})
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
		return rpc.DNSStatus{Target: sysdns.DefaultTarget, Services: []rpc.DNSService{}}, nil
	}
	target := e.dnsTarget()
	st, err := e.dnsManager().Status(ctx)
	if err != nil {
		return rpc.DNSStatus{}, err
	}
	out := toDNSStatus(st, target)
	out.Watch = e.DNSWatch()
	return out, nil
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
	target := e.dnsTarget()
	addr := net.JoinHostPort(target[0], "53")
	if err := e.probeDNS(ctx, addr); err != nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeFailed, "mihomo's DNS does not answer at %s (%v); the system DNS was not changed", addr, err)
	}
	e.dnsWatch.mu.Lock()
	st, err := e.dnsManager().Point(ctx)
	if err == nil {
		// Pointed on purpose: a watchdog restore is over.
		if w := e.dnsWatchdog(); w != nil {
			w.Reset()
		}
	}
	e.dnsWatch.mu.Unlock()
	out := toDNSStatus(st, target)
	out.Watch = e.DNSWatch()
	e.log().Info("system DNS pointed at mihomo", "changed", len(st.Changed), "err", err)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	e.triggerHealth("dns point")
	return out, nil
}

// DNSBackup records automatic (DHCP) as the restore value of every
// service that points at mihomo without one (sysdns.Manager.Adopt): the
// system DNS was pointed at mihomo before Twinnel did it (by hand, or by
// another tool), so nothing could put it back if mihomo stopped. It
// changes no DNS setting and works while mihomo is down; the watchdog
// protects those services from then on.
func (e *Engine) DNSBackup(ctx context.Context) (rpc.DNSStatus, error) {
	if e.SystemDNS == nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotImplemented, "setting the system DNS is not supported on this platform yet")
	}
	e.dnsWatch.mu.Lock()
	st, err := e.dnsManager().Adopt(ctx)
	e.dnsWatch.mu.Unlock()
	out := toDNSStatus(st, e.dnsTarget())
	out.Watch = e.DNSWatch()
	e.log().Info("system DNS backup recorded", "recorded", len(st.Recorded), "err", err)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	if len(st.Recorded) > 0 {
		e.triggerHealth("dns backup")
	}
	return out, nil
}

// DNSRestore puts the system DNS back as it was before DNSPoint.
func (e *Engine) DNSRestore(ctx context.Context) (rpc.DNSStatus, error) {
	if e.SystemDNS == nil {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotImplemented, "setting the system DNS is not supported on this platform yet")
	}
	// Also how a user keeps the earlier DNS after a watchdog restore
	// (保持原设置): without a backup the watchdog has nothing to watch.
	e.dnsWatch.mu.Lock()
	st, err := e.dnsManager().Restore(ctx)
	if w := e.dnsWatchdog(); w != nil && (err == nil || errors.Is(err, sysdns.ErrNoBackup)) {
		w.Reset()
	}
	e.dnsWatch.mu.Unlock()
	if errors.Is(err, sysdns.ErrNoBackup) {
		return rpc.DNSStatus{}, rpc.Errorf(rpc.CodeNotFound, "%v", err)
	}
	out := toDNSStatus(st, e.dnsTarget())
	out.Watch = e.DNSWatch()
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
	in.Repair = p.Repair
	_, managedDaemon := in.Binaries()
	if existing := probe.FindExisting(); existing != "" {
		kind := probe.Kind(existing)
		bad := in.Damaged(existing, daemonNextTo(existing))
		if len(bad) > 0 && p.Repair && kind == kernel.TSWindows && in.UsesMSI() {
			e.log().Warn("tailscale install damaged: repairing", "files", len(bad))
			r, err := e.tailscaleInstallMSI(ctx, st, p, in)
			r.Repaired = err == nil
			return r, err
		}
		out.Kept, out.Path, out.Kind = true, existing, kind
		out.UsesSystemVPN = out.Kind == kernel.TSStandalone || out.Kind == kernel.TSAppStore
		if len(bad) > 0 {
			out.Damaged = binaryProblems(bad)
			e.log().Warn("tailscale install damaged", "kind", kind, "files", len(bad))
			return out, rpc.Errorf(rpc.CodeFailed, "%v", kernel.DamagedError(bad))
		}
		if v, err := probe.LocalVersion(ctx, existing); err == nil {
			out.Version = v
		}
		e.log().Info("tailscale install kept", "kind", out.Kind)
		return out, nil
	}
	if in.UsesMSI() {
		return e.tailscaleInstallMSI(ctx, st, p, in)
	}
	cur := in.Current()
	if cur != "" && fileExists(managedDaemon) {
		cli, daemon := in.Binaries()
		if bad := in.Damaged(cli, daemon); len(bad) > 0 {
			if !p.Repair {
				out.Kept, out.Kind, out.Path, out.Daemon = true, "twinnel", cli, daemon
				out.Damaged = binaryProblems(bad)
				return out, rpc.Errorf(rpc.CodeFailed, "%v", kernel.DamagedError(bad))
			}
			cur, out.Repaired = "", true // unpack it again
		}
	}
	if cur != "" && fileExists(managedDaemon) {
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
	if p.NoStart && !svc.DryRun {
		out.Stopped = e.stopAfterInstall(ctx)
	}
	e.triggerHealth("tailscale install")
	return out, nil
}

// stopAfterInstall stops tailscaled after an install-only request (the
// installer started it) and reports whether it is stopped.
func (e *Engine) stopAfterInstall(ctx context.Context) bool {
	if _, err := e.ServiceOp(ctx, "tailscaled", service.OpStop); err != nil {
		e.log().Warn("tailscale: stop after install-only failed", "err", err)
		return false
	}
	e.log().Info("tailscale installed and left stopped (install only)")
	return true
}

// daemonNextTo is the tailscaled next to a tailscale CLI, if there is one.
func daemonNextTo(cli string) string {
	dir, base := filepath.Split(cli)
	name := "tailscaled"
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		name += ".exe"
	} else if strings.Contains(cli, `\`) {
		return ""
	}
	if d := filepath.Join(dir, name); fileExists(d) {
		return d
	}
	return ""
}

func binaryProblems(rs []binsane.Result) []rpc.BinaryProblem {
	out := make([]rpc.BinaryProblem, 0, len(rs))
	for _, r := range rs {
		out = append(out, rpc.BinaryProblem{Path: r.Path, Problem: string(r.Problem), Detail: r.Detail})
	}
	return out
}

// tailscaleInstallMSI installs the official Windows client from its MSI
// (core/kernel tailscale_msi.go) and waits for its "Tailscale" service.
// The service is the MSI's: Twinnel records it as adopted, so it starts
// and stops it but never replaces or removes it.
func (e *Engine) tailscaleInstallMSI(ctx context.Context, st *store.Settings, p rpc.TailscaleInstallParams, in *kernel.TailscaleInstaller) (rpc.TailscaleInstallResult, error) {
	out := rpc.TailscaleInstallResult{Kind: kernel.TSWindows}
	label := st.Tailscale.ServiceLabel
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
	out.Source = a.Source
	logPath := e.Store.Path("tailscale-msi.log")
	op := &rpc.ServiceOpResult{Name: "tailscaled", Label: label, Op: string(service.OpInstall),
		Steps: []string{"download " + a.URL + " (sha256 " + a.SHA256 + ")", kernel.MSIExec() + " " + strings.Join(kernel.MSIArgs(a.Name, logPath), " ")}}
	out.Service = op
	if r := e.ServiceRunner; r == nil || r.DryRun || r.Exec == nil {
		// A development helper (not elevated): only the plan.
		op.DryRun = true
		out.Version = a.Version
		return out, nil
	}
	got, err := in.InstallMSI(ctx, a, logPath)
	if err != nil {
		e.log().Warn("tailscale msi install failed", "version", a.Version, "err", err)
		if bad := in.Damaged(got.CLI, got.Daemon); len(bad) > 0 {
			out.Damaged = binaryProblems(bad)
		}
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	out.Version, out.Path, out.Daemon, out.RestartNeeded = got.Version, got.CLI, got.Daemon, got.RestartNeeded
	e.log().Info("tailscale installed", "version", got.Version, "source", a.Source, "via", via, "restart_needed", got.RestartNeeded)
	if _, err := e.Store.UpdateSettings(func(s *store.Settings) error {
		s.Tailscale.Binary, s.Tailscale.DaemonBinary, s.Tailscale.ServiceAdopted = got.CLI, got.Daemon, true
		return nil
	}); err != nil {
		return out, err
	}
	svc, err := e.waitService(ctx, "tailscaled", 120)
	op.Status = svc
	e.publish(rpc.EventServiceChanged, map[string]any{"name": "tailscaled", "op": "install"})
	e.triggerHealth("tailscale install")
	if err != nil {
		return out, err
	}
	if !svc.Running {
		return out, rpc.Errorf(rpc.CodeFailed, "Tailscale was installed but its service did not start; restart the computer and try again")
	}
	if p.NoStart {
		// The MSI starts its service; the user asked for the install only.
		out.Stopped = e.stopAfterInstall(ctx)
	}
	return out, nil
}

// waitService polls a service until it runs, up to tries half seconds;
// err is only for a canceled wait.
func (e *Engine) waitService(ctx context.Context, name string, tries int) (rpc.ServiceStatus, error) {
	var last rpc.ServiceStatus
	for i := range tries {
		s, err := e.ServiceStatus(ctx, name)
		if err == nil {
			last = s
			if s.Running {
				return s, nil
			}
		}
		if i == tries-1 {
			break
		}
		if err := e.sleep(ctx, 500*time.Millisecond); err != nil {
			return last, err
		}
	}
	return last, nil
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

// loginPoll is how often TailscaleLogin re-reads the state after Up.
var loginPoll = 250 * time.Millisecond

// loginUndecided: the node has not said yet whether it runs or needs a
// login.
func loginUndecided(backend string) bool {
	return backend == "Stopped" || backend == "NoState"
}

// ErrLoginInSession is tailscale.login's answer to Start where the login
// must run in the signed-in user's own process (Engine.TSLoginInSession).
const ErrLoginInSession = "Tailscale logs in from the signed-in user's session on this system, not from the helper"

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
	if p.Start && (backend != "Running" || p.Reauth) {
		if e.TSLoginInSession {
			return out, rpc.Errorf(rpc.CodeNotImplemented, "%s", ErrLoginInSession)
		}
		if backend != "Running" {
			if err := e.TSControl.Up(ctx); err != nil {
				return out, rpc.Errorf(rpc.CodeFailed, "tailscale up: %v", err)
			}
			// A node that was only stopped connects now; one that is
			// logged out (or new) says NeedsLogin. Wait a moment to know.
			for i := 0; i < 8 && loginUndecided(backend); i++ {
				if i > 0 || backend == "Stopped" {
					select {
					case <-ctx.Done():
						return out, rpc.Errorf(rpc.CodeFailed, "tailscaled: %v", ctx.Err())
					case <-time.After(loginPoll):
					}
				}
				if backend, authURL, tailnet, err = e.TSControl.LoginState(ctx); err != nil {
					return out, rpc.Errorf(rpc.CodeFailed, "tailscaled: %v", err)
				}
			}
		}
		// A login already pending keeps its URL (another client may have
		// opened it); otherwise ask for one.
		if p.Reauth || backend == "NeedsLogin" && authURL == "" || backend == "NoState" {
			if err := e.TSControl.StartLogin(ctx); err != nil {
				return out, rpc.Errorf(rpc.CodeFailed, "tailscale login: %v", err)
			}
			out.Started = true
			e.log().Info("tailscale login started", "reauth", p.Reauth)
			if backend, authURL, tailnet, err = e.TSControl.LoginState(ctx); err != nil {
				return out, rpc.Errorf(rpc.CodeFailed, "tailscaled: %v", err)
			}
		}
	}
	out.BackendState, out.AuthURL, out.Tailnet = backend, authURL, tailnet
	return out, nil
}
