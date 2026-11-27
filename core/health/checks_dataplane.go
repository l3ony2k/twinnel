package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// TUNDataplane: with mihomo's TUN on, a real TCP connection that the
// routing table sends into the TUN completes its handshake.
//
// Every other check reads the control plane (routes, rules, DNS, the
// controller). They all passed on a Windows machine where mihomo's
// "mixed" stack (system TCP) silently dropped every TCP connection that
// came in through the TUN while ICMP and the proxy port still worked
// (docs/tun-dataplane.md). This check is the only one that would have
// caught it.
//
// Cost: one TCP handshake that mihomo's TUN stack answers itself (both
// stacks complete the handshake locally before they dial out), closed at
// once, so a few milliseconds per round. Only when it gets no answer does
// it spend up to DataplaneTimeout more on the comparison evidence (one
// ICMP echo to the TUN gateway, one CONNECT through mihomo's proxy port).
type TUNDataplane struct{}

func (TUNDataplane) ID() string          { return "tun-dataplane" }
func (c TUNDataplane) Title() string     { return c.title(&zhTexts) }
func (TUNDataplane) Platforms() []string { return AllPlatforms }
func (TUNDataplane) title(L *texts) string {
	return dpFor(L).title
}

// Reasons of tun-dataplane. They are added to the fix catalog in init
// below, next to the check, instead of in reasons.go.
const (
	// TCP through the TUN gets no answer and the stack is not gvisor:
	// switch the stack (a managed override, undoable).
	ReasonDataplaneTCP = "tun-dataplane/tcp-dead"
	// The same with the gvisor stack already: restart mihomo.
	ReasonDataplaneGvisor = "tun-dataplane/tcp-dead-gvisor"
	// The test connection failed for another reason (no route, network
	// error): nothing to conclude; run it again.
	ReasonDataplaneDial = "tun-dataplane/dial-failed"
)

// FixTUNStack (FixHint.AutoFix, FixSpec.Action) adds the override
// TUNStackLayerID that sets tun.stack to gvisor, then applies.
const FixTUNStack = "tun-stack-gvisor"

// TUNStackLayerID is the override the tun-stack-gvisor fix adds, and
// TUNStackLayer its content.
const (
	TUNStackLayerID = "tun-stack"
	TUNStackLayer   = "# Twinnel health fix (tun-dataplane): TCP through the TUN got no answer with the\n" +
		"# system/mixed stack. gvisor handles TCP in user space. Delete this override to go back.\n" +
		"tun:\n  stack: gvisor\n"
)

func init() {
	fixCatalog[ReasonDataplaneTCP] = FixSpec{Kind: FixAuto, Action: FixTUNStack}
	fixCatalog[ReasonDataplaneGvisor] = FixSpec{Kind: FixAuto, Action: FixRestartMihomo}
	fixCatalog[ReasonDataplaneDial] = FixSpec{Kind: FixAuto, Action: FixRerun}
	for tag, tbl := range fixTextTables {
		for k, v := range dpFixTexts[tag] {
			tbl[k] = v
		}
	}
}

// DataplaneEnv is implemented by Envs that can test mihomo's TUN data
// path. Tests and shot fixtures answer without touching the network.
type DataplaneEnv interface {
	// DialTCP connects to addr (ip:port) with an ordinary socket, so the
	// routing table decides where it goes, and closes the connection as
	// soon as the handshake completes. A refused connection is ErrRefused
	// (wrapped). It honours ctx's deadline.
	DialTCP(ctx context.Context, addr string) error
	// DialViaProxy asks the HTTP proxy at proxy (host:port, mihomo's mixed
	// or HTTP port) to CONNECT to addr and, with serverName set, shakes
	// hands with TLS through the tunnel (NetDialViaProxy).
	DialViaProxy(ctx context.Context, proxy, addr, serverName string) error
	// Ping sends one ICMP echo to ip and waits for the reply.
	// ErrNotSupported where the Env cannot (no privilege for ICMP).
	Ping(ctx context.Context, ip string) error
}

// ErrRefused: the connection was refused (a TCP reset). Through mihomo's
// TUN that is mihomo's own answer (a REJECT rule), so the path works.
var ErrRefused = errors.New("connection refused")

// Dataplane probe parameters.
const (
	// DataplaneTimeout bounds each probe (handshake, CONNECT, ICMP).
	DataplaneTimeout = 2500 * time.Millisecond
	// DataplaneProbeIP is dialed first: an address the TUN's split routes
	// cover on every platform, needing no DNS. Port 443.
	DataplaneProbeIP = "1.1.1.1"
	// DataplaneProbeName is resolved through mihomo's DNS when the probe
	// IP does not route into the TUN (an exit node, an exclude): with
	// fake-ip its answer is in the TUN's own range.
	DataplaneProbeName = "connectivitycheck.gstatic.com"
)

func (TUNDataplane) Run(ctx context.Context, env Env) Result {
	L0 := tx(ctx)
	L := dpFor(L0)
	tun, src, err := tunConfig(ctx, env)
	if err != nil {
		return skipErr(L0.msgMihomoUnavailable, err)
	}
	if !tun.Enable {
		return skipText(L.msgTUNOff)
	}
	de, ok := env.(DataplaneEnv)
	if !ok {
		return skip(L.msgNotSupported, ErrNotSupported)
	}
	stack := strings.ToLower(tun.Stack)
	if stack == "" {
		stack = "gvisor" // mihomo's default
	}
	target, sni, via, notes := dataplaneTarget(ctx, env, tun)
	res := Result{Evidence: notes}
	if target == "" {
		res.Status, res.Summary = StatusSkip, L.msgNoTarget
		return res
	}
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evTarget, target, via), fmt.Sprintf(L.evStack, stack, src))

	dctx, cancel := context.WithTimeout(ctx, DataplaneTimeout)
	t0 := time.Now()
	err = de.DialTCP(dctx, target)
	took := time.Since(t0)
	cancel()
	switch {
	case errors.Is(err, ErrNotSupported):
		res.Status, res.Summary = StatusSkip, fmt.Sprintf(L.msgNotSupported, err)
		return res
	case err == nil:
		res.Status, res.Summary = StatusOK, fmt.Sprintf(L.msgOK, stack)
		res.Evidence = append(res.Evidence, fmt.Sprintf(L.evTCPOK, roundMS(took)))
		return res
	case errors.Is(err, ErrRefused):
		res.Status, res.Summary = StatusOK, fmt.Sprintf(L.msgOK, stack)
		res.Evidence = append(res.Evidence, L.evTCPRefused)
		return res
	case !IsTimeout(err) && ctx.Err() == nil:
		res.Status, res.Summary = StatusWarn, fmt.Sprintf(L.msgDialErr, err)
		res.because(ReasonDataplaneDial)
		res.Fix = &FixHint{Text: L.fixRerun, AutoFix: FixRerun}
		return res
	}

	// No answer: gather what tells a broken TUN stack from a dead mihomo.
	res.Status, res.Summary = StatusFail, L.msgDead
	res.Evidence = append(res.Evidence, fmt.Sprintf(L.evTCPTimeout, target, DataplaneTimeout))
	if gw := tunGateway(tun); gw.IsValid() {
		pctx, cancel := context.WithTimeout(ctx, time.Second)
		perr := de.Ping(pctx, gw.String())
		cancel()
		switch {
		case perr == nil:
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evPingOK, gw))
		case !errors.Is(perr, ErrNotSupported):
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evPingFail, gw, perr))
		}
	}
	if proxy := proxyAddr(ctx, env); proxy != "" {
		mctx, cancel := context.WithTimeout(ctx, DataplaneTimeout)
		merr := de.DialViaProxy(mctx, proxy, target, sni)
		cancel()
		if merr == nil {
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evProxyOK, proxy))
		} else {
			res.Evidence = append(res.Evidence, fmt.Sprintf(L.evProxyFail, proxy, merr))
		}
	}
	if stack == "gvisor" {
		res.because(ReasonDataplaneGvisor)
		res.Fix = &FixHint{Text: L.fixRestart, AutoFix: FixRestartMihomo}
	} else {
		res.because(ReasonDataplaneTCP)
		res.Fix = &FixHint{Text: L.fixGvisor, AutoFix: FixTUNStack}
	}
	return res
}

// dataplaneTarget picks an address whose route goes into mihomo's TUN:
// DataplaneProbeIP, else the fake-ip mihomo's DNS gives
// DataplaneProbeName. It returns "ip:443", the name to send in a TLS
// handshake to it, the TUN interface, and evidence about candidates that
// did not route into the TUN.
func dataplaneTarget(ctx context.Context, env Env, tun TUNConfig) (target, sni, via string, notes []string) {
	L := dpFor(tx(ctx))
	try := func(ip, name string) bool {
		rl, err := env.RouteGet(ctx, ip)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s → %v", ip, err))
			return false
		}
		if !isMihomoTUN(env.Platform(), rl.Interface, tun) {
			notes = append(notes, fmt.Sprintf(L.evNotTUN, ip, orDash(rl.Interface)))
			return false
		}
		target, sni, via = net.JoinHostPort(ip, "443"), name, rl.Interface
		return true
	}
	if try(DataplaneProbeIP, DataplaneProbeIP) {
		return
	}
	cfg, err := env.MihomoConfig(ctx)
	if err != nil || !strings.EqualFold(cfg.DNS.EnhancedMode, "fake-ip") {
		return
	}
	ans, err := env.LookupMihomo(ctx, DataplaneProbeName)
	if err != nil {
		notes = append(notes, fmt.Sprintf("%s → %v", DataplaneProbeName, err))
		return
	}
	fr := fakeIPRange(cfg)
	for _, a := range ans.Addrs {
		if a.Is4() && fr.Contains(a) && try(a.String(), DataplaneProbeName) {
			return
		}
	}
	return "", "", "", notes
}

// isMihomoTUN reports whether iface is mihomo's TUN device. A device the
// config leaves to the system ("" or a bare "utun" on macOS) is matched by
// mihomo's naming: utunN on macOS, Meta/MetaN elsewhere.
func isMihomoTUN(platform, iface string, tun TUNConfig) bool {
	if iface == "" {
		return false
	}
	dev := tun.Device
	if platform == Darwin && (dev == "" || dev == "utun") {
		return strings.HasPrefix(iface, "utun")
	}
	if dev == "" {
		return strings.HasPrefix(iface, "Meta")
	}
	return iface == dev
}

// tunGateway is the peer address of the TUN's inet4 network (the TUN's
// own address + 1, 198.18.0.2 for mihomo's default 198.18.0.1/30): the
// address the report pinged.
func tunGateway(tun TUNConfig) netip.Addr {
	pfx := netip.MustParsePrefix("198.18.0.1/30")
	for _, s := range tun.Inet4Address {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil && p.Addr().Is4() {
			pfx = p
			break
		}
	}
	gw := pfx.Addr().Next()
	if !pfx.Contains(gw) {
		return netip.Addr{}
	}
	return gw
}

// proxyAddr is mihomo's mixed (else HTTP) port on loopback, or "".
func proxyAddr(ctx context.Context, env Env) string {
	rt, err := env.MihomoRuntime(ctx)
	if err != nil {
		return ""
	}
	port := rt.Configs.MixedPort
	if port == 0 {
		port = rt.Configs.Port
	}
	if port == 0 {
		return ""
	}
	host := "127.0.0.1"
	if b := rt.Configs.BindAddress; b != "" && b != "*" && b != "0.0.0.0" && b != "::" {
		host = b
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// IsTimeout reports whether err is a timeout (a deadline or a net.Error
// that timed out).
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func roundMS(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(time.Millisecond)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// memoEnv passes the dataplane probes through, unmemoized: each runs once
// per report and needs the calling check's deadline.

func (m *memoEnv) DialTCP(ctx context.Context, addr string) error {
	if e, ok := m.inner.(DataplaneEnv); ok {
		return e.DialTCP(ctx, addr)
	}
	return ErrNotSupported
}

func (m *memoEnv) DialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	if e, ok := m.inner.(DataplaneEnv); ok {
		return e.DialViaProxy(ctx, proxy, addr, serverName)
	}
	return ErrNotSupported
}

func (m *memoEnv) Ping(ctx context.Context, ip string) error {
	if e, ok := m.inner.(DataplaneEnv); ok {
		return e.Ping(ctx, ip)
	}
	return ErrNotSupported
}
