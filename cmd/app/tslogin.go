package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/cmd/app/internal/qr"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// The Tailscale state panel: every state of the local node with the one
// action that moves it on (install, start, connect, log in, wait for the
// admin), and the interactive login (browser, copyable link, QR code for a
// phone, cancel, timeout). The Tailscale page, the overview's card, the
// flyout and the onboarding wizard all draw it (tsLoginPanel).

// tsPhase is the local node's state as the panel tells it.
type tsPhase int

const (
	tsRunning      tsPhase = iota // connected; the pages show the node
	tsNotInstalled                // no Tailscale found
	tsDaemonDown                  // installed, but the daemon does not answer
	tsStarting                    // connecting (Starting, NoState)
	tsStopped                     // logged in, disconnected
	tsNeedsLogin                  // never logged in on this device
	tsLoggedOut                   // the user logged out
	tsKeyExpired                  // the node key expired: log in again
	tsMachineAuth                 // logged in, waiting for an admin's approval
)

// tsPhaseOf reads the phase from a snapshot.
func tsPhaseOf(s status.Snapshot) tsPhase {
	ts := s.Tailscale
	st := ts.State
	switch {
	case !ts.Reachable && ts.Install.Kind == tailscale.InstallUnknown && ts.Err != "":
		return tsNotInstalled
	case !ts.Reachable:
		return tsDaemonDown
	}
	switch st.BackendState {
	case "Running":
		return tsRunning
	case "Stopped":
		return tsStopped
	case "NeedsMachineAuth":
		return tsMachineAuth
	case "NeedsLogin":
		switch {
		case st.Self.Expired || st.KeyExpiry != nil && !st.KeyExpiry.After(s.Time):
			return tsKeyExpired
		case st.Prefs.LoggedOut:
			return tsLoggedOut
		}
		return tsNeedsLogin
	}
	return tsStarting
}

// tsOS is the system whose words the panel uses: this one, or in shot
// mode TWINNEL_SHOTS_OS (darwin, windows, linux), so one Mac can shoot all
// three.
var tsOS = func() string {
	if v := os.Getenv("TWINNEL_SHOTS_OS"); v != "" && os.Getenv("TWINNEL_SHOTS") != "" {
		return v
	}
	return runtime.GOOS
}()

// osText picks the macOS, Windows or Linux text.
func osText(mac, win, linux string) string {
	switch tsOS {
	case "windows":
		return win
	case "linux":
		return linux
	}
	return mac
}

// tsDownloadURL is Tailscale's download page for this system.
func tsDownloadURL() string {
	return "https://tailscale.com/download/" + osText("mac", "windows", "linux")
}

// --- the login flow ---

// tsLoginTimeout is how long the panel waits for the browser login.
const tsLoginTimeout = 10 * time.Minute

type tsFlowState int

const (
	flowIdle     tsFlowState = iota
	flowStarting             // asking tailscaled for a login
	flowWaiting              // the link is out; waiting for the browser
	flowTimedOut
	flowFailed
)

// tsLoginFlow is the running login: main thread only.
type tsLoginFlow struct {
	state  tsFlowState
	reauth bool
	url    string
	errMsg string // the failure in words (flowFailed)
	raw    string // the system's text, behind 详细信息
	gen    int
	cancel context.CancelFunc
	qr     *ui.Bitmap
	qrFor  string
}

func (f *tsLoginFlow) active() bool { return f.state == flowStarting || f.state == flowWaiting }

// tsLoginBackend is what the flow drives: the helper's tailscale.login
// (as root: never depends on tailscaled granting the user writes) or,
// without a helper, the app's own LocalAPI client.
type tsLoginBackend interface {
	start(ctx context.Context, reauth bool) (rpc.TailscaleLoginResult, error)
	state(ctx context.Context) (rpc.TailscaleLoginResult, error)
}

type helperLogin struct{ hc *status.HelperConn }

func (h helperLogin) call(ctx context.Context, p rpc.TailscaleLoginParams) (rpc.TailscaleLoginResult, error) {
	var r rpc.TailscaleLoginResult
	err := h.hc.Call(ctx, rpc.MethodTailscaleLogin, p, &r)
	return r, err
}

func (h helperLogin) start(ctx context.Context, reauth bool) (rpc.TailscaleLoginResult, error) {
	return h.call(ctx, rpc.TailscaleLoginParams{Start: true, Reauth: reauth})
}

func (h helperLogin) state(ctx context.Context) (rpc.TailscaleLoginResult, error) {
	return h.call(ctx, rpc.TailscaleLoginParams{})
}

// directLogin is the same over the app's LocalAPI client, as the
// signed-in user: the only way on Windows, where tailscaled gives no
// profile to the helper (SYSTEM) and tailscale.login refuses Start.
type directLogin struct{ ts *tailscale.Client }

func (d directLogin) state(ctx context.Context) (rpc.TailscaleLoginResult, error) {
	st, err := d.ts.State(ctx)
	if err != nil {
		return rpc.TailscaleLoginResult{}, err
	}
	return rpc.TailscaleLoginResult{BackendState: st.BackendState, AuthURL: st.AuthURL, Tailnet: st.Tailnet}, nil
}

func (d directLogin) start(ctx context.Context, reauth bool) (rpc.TailscaleLoginResult, error) {
	r, err := d.state(ctx)
	if err != nil {
		return r, err
	}
	if r.BackendState == "Running" && !reauth {
		return r, nil
	}
	if r.BackendState != "Running" {
		// On Windows the login belongs to the signed-in user, and the
		// node must stay up after Twinnel quits: Unattended Mode, as
		// core/tailscale.LoginInSession (the helper refuses Start there).
		up := d.ts.Up
		if runtime.GOOS == "windows" {
			up = d.ts.UpUnattended
		}
		if err := up(ctx); err != nil {
			return r, err
		}
		for i := 0; i < 8 && (r.BackendState == "Stopped" || r.BackendState == "NoState"); i++ {
			select {
			case <-ctx.Done():
				return r, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			if r, err = d.state(ctx); err != nil {
				return r, err
			}
		}
	}
	if reauth || r.BackendState == "NeedsLogin" && r.AuthURL == "" || r.BackendState == "NoState" {
		if err := d.ts.Login(ctx); err != nil {
			return r, err
		}
		r.Started = true
		return d.state(ctx)
	}
	return r, nil
}

// loginBackend picks the helper when it is connected, else the app's
// client.
func (a *app) loginBackend(s status.Snapshot) tsLoginBackend {
	if a.tsLoginBE != nil { // tests
		return a.tsLoginBE
	}
	if a.hc != nil && s.Helper.Connected() {
		return helperLogin{a.hc}
	}
	return directLogin{a.ts}
}

// startTSLogin starts the login (or, for a stopped node, just connects):
// it opens the browser once the link is there and waits until the node
// runs, the user cancels, or tsLoginTimeout passes. reauth logs in again
// on a running node (a key that expires soon).
func (a *app) startTSLogin(s status.Snapshot, reauth bool) {
	f := &a.tsl
	if f.active() {
		return
	}
	if f.cancel != nil {
		f.cancel()
	}
	f.gen++
	gen := f.gen
	ctx, cancel := context.WithTimeout(context.Background(), tsLoginTimeout)
	*f = tsLoginFlow{state: flowStarting, reauth: reauth, gen: gen, cancel: cancel, qr: f.qr, qrFor: f.qrFor}
	be, fallback := a.loginBackend(s), tsLoginBackend(directLogin{a.ts})
	mon := a.mon
	refresh := func() {
		if mon != nil {
			mon.Refresh()
		}
	}
	onMain := a.runOnMain
	update := func(fn func(f *tsLoginFlow)) {
		onMain(func() {
			if a.tsl.gen != gen {
				return // cancelled or replaced
			}
			fn(&a.tsl)
			a.invalidate()
		})
	}
	go func() {
		defer cancel()
		r, err := be.start(ctx, reauth)
		var re *rpc.Error
		if errors.As(err, &re) && (re.Code == rpc.CodeNotImplemented || re.Code == rpc.CodeMethodNotFound) {
			// A helper without tailscale.login (older, or a development
			// one on a synthetic tailnet): the app's own client.
			be = fallback
			r, err = be.start(ctx, reauth)
		}
		if err != nil {
			update(func(f *tsLoginFlow) { f.fail(err) })
			refresh()
			return
		}
		opened, sawURL := "", false
		for {
			if r.AuthURL != "" {
				sawURL = true
				if r.AuthURL != opened {
					opened = r.AuthURL
					u := r.AuthURL
					update(func(f *tsLoginFlow) {
						f.state, f.url = flowWaiting, u
						a.openExternal(u)
					})
				}
			}
			if tsLoginDone(r, reauth, sawURL) {
				tailnet := r.Tailnet
				update(func(f *tsLoginFlow) {
					*f = tsLoginFlow{gen: f.gen, qr: f.qr, qrFor: f.qrFor}
					if r.BackendState == "Running" {
						a.toast = i18n.T.TSLLoggedInBare
						if tailnet != "" {
							a.toast = i18n.F(i18n.T.TSLLoggedIn, tailnet)
						}
					}
				})
				refresh()
				return
			}
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					update(func(f *tsLoginFlow) { f.state = flowTimedOut })
				}
				return
			case <-time.After(time.Second):
			}
			if r2, err := be.state(ctx); err == nil {
				r = r2
			}
		}
	}()
}

// tsLoginDone: the node runs (after the link was used, for a reauth), or
// waits for an admin (the panel shows that state).
func tsLoginDone(r rpc.TailscaleLoginResult, reauth, sawURL bool) bool {
	switch r.BackendState {
	case "Running":
		return r.AuthURL == "" && (!reauth || sawURL)
	case "NeedsMachineAuth", "Starting":
		return r.AuthURL == ""
	}
	return false
}

func (f *tsLoginFlow) fail(err error) {
	f.state = flowFailed
	f.errMsg = helperError(err)
	f.raw = err.Error()
	if errors.Is(err, tailscale.ErrWritesDisabled) || errors.Is(err, tailscale.ErrStatusFile) {
		f.errMsg = i18n.T.WritesDisabledHint
	}
}

// cancelTSLogin stops waiting. tailscaled keeps its pending login; a new
// one starts from the panel.
func (a *app) cancelTSLogin() {
	f := &a.tsl
	if f.cancel != nil {
		f.cancel()
	}
	f.gen++
	*f = tsLoginFlow{gen: f.gen, qr: f.qr, qrFor: f.qrFor}
}

// openExternal opens a link in the browser; never in shot mode, and
// through a.openURL in tests.
func (a *app) openExternal(u string) {
	switch {
	case a.openURL != nil:
		a.openURL(u)
	case a.shotMode:
	default:
		_ = mygo.Shell.OpenExternal(u)
	}
}

// runOnMain is a.onMain, or mygo.RunOnMain.
func (a *app) runOnMain(fn func()) {
	if a.onMain != nil {
		a.onMain(fn)
		return
	}
	mygo.RunOnMain(fn)
}

// qrBitmap is the login link's QR code, encoded once per link.
func (f *tsLoginFlow) qrBitmap(url string) *ui.Bitmap {
	if f.qrFor == url && f.qr != nil {
		return f.qr
	}
	code, err := qr.Encode(url)
	if err != nil {
		return nil
	}
	f.qr, f.qrFor = ui.NewBitmap(code.Image(1, 4)).Pixelated(), url
	return f.qr
}

// --- actions other than the login ---

// startTailscaled starts the Tailscale service through the helper.
func (a *app) startTailscaled() {
	hc := a.hc
	a.do(func(ctx context.Context) error {
		var r rpc.ServiceOpResult
		return hc.Call(ctx, rpc.MethodServiceStart, rpc.ServiceParams{Name: "tailscaled"}, &r)
	})
}

// connectTailscale connects a stopped node: tailscale.login connects
// without a new login when the node is still logged in.
func (a *app) connectTailscale(s status.Snapshot) { a.startTSLogin(s, false) }

// tsInstallAction installs Tailscale: the onboarding wizard sets
// tsInstallHook to open its Tailscale step; without it, 设置这台 Mac.
func (a *app) tsInstallAction() {
	if a.tsInstallHook != nil {
		a.tsInstallHook()
		return
	}
	a.openPage(pageSetup)
}

// --- the panel ---

// tsPanelOpts tunes tsLoginPanel to where it is drawn.
type tsPanelOpts struct {
	// Compact: a card or the flyout (one line of text, no QR code, small
	// buttons). Otherwise a page or wizard step: centered, with the QR
	// code while a login waits.
	Compact bool
	// OnInstall replaces the install action (the wizard's own step).
	OnInstall func()
	// ShowRunning draws a ✓ line for a running node (the wizard);
	// otherwise the panel draws nothing then.
	ShowRunning bool
}

// tsPanelShown reports whether tsLoginPanel draws anything for s: the
// node does not run, or a login is under way.
func (a *app) tsPanelShown(s status.Snapshot) bool {
	return tsPhaseOf(s) != tsRunning || a.tsl.state != flowIdle
}

// tsLoginPanel draws the node's state with its action, or the running
// login. Callers check tsPanelShown first unless ShowRunning.
func (a *app) tsLoginPanel(c *ui.Context, s status.Snapshot, o tsPanelOpts) {
	T := i18n.T
	t := c.Theme()
	ph := tsPhaseOf(s)
	f := &a.tsl
	can := s.WritesAllowed
	var (
		title, body string
		icon        = iconTailscale
		iconCol     = t.TextMuted
		raw         string
		actions     func()
		busy        bool
	)
	primary := func(key, label string, run func()) {
		b := disabledWhy(c, !can, T.WritesDisabledHint, func() ui.Element {
			if o.Compact {
				return pushButton(c.Key(key), label)
			}
			return primaryButton(c.Key(key), label, false)
		})
		if b.Clicked() && can {
			run()
		}
	}
	secondary := func(key, label string, run func()) {
		if pushButton(c.Key(key), label).Clicked() {
			run()
		}
	}
	login := func(reauth bool, label string) func() {
		return func() { primary("ts-login", label, func() { a.startTSLogin(s, reauth) }) }
	}

	switch {
	case f.state == flowStarting || f.state == flowWaiting:
		a.tsWaitingPanel(c, o)
		return
	case f.state == flowTimedOut:
		title, body = T.TSLTimedOutTitle, T.TSLTimedOutBody
		icon, iconCol = iconWarning, pal(t).warnText
		actions = func() {
			primary("ts-login", T.TSLRelogin, func() { a.startTSLogin(s, f.reauth) })
			secondary("ts-cancel", T.Cancel, a.cancelTSLogin)
		}
	case f.state == flowFailed:
		title, body, raw = T.TSLFailedTitle, T.TSLFailedBody, f.raw
		if f.errMsg == T.WritesDisabledHint {
			body = f.errMsg
		}
		icon, iconCol = iconError, pal(t).badText
		actions = func() {
			primary("ts-login", T.Retry, func() { a.startTSLogin(s, f.reauth) })
			secondary("ts-cancel", T.Cancel, a.cancelTSLogin)
		}
	case ph == tsNotInstalled:
		title, body = T.TSLNotInstalledTitle, T.TSLNotInstalledBody
		install := a.tsInstallAction
		if o.OnInstall != nil {
			install = o.OnInstall
		}
		actions = func() {
			primary("ts-install", T.TSLInstall, install)
			ui.Link(c.Key("ts-download"), T.TSLDownload, tsDownloadURL()).FontSize(plat.calloutSize)
		}
	case ph == tsDaemonDown:
		title, raw = T.TSLDownTitle, s.Tailscale.Err
		icon = iconError
		app := s.Tailscale.Install.Kind == tailscale.InstallMacStandalone || s.Tailscale.Install.Kind == tailscale.InstallMacAppStore
		body = osText(T.TSLDownBodyMac, T.TSLDownBodyWin, T.TSLDownBodyLinux)
		if app {
			body = T.TSLDownBodyApp
		}
		actions = func() {
			switch {
			case app:
				secondary("ts-open-app", T.TSLOpenApp, func() { a.openExternal("file:///Applications/Tailscale.app") })
			case s.Helper.Connected():
				primary("ts-start", T.TSLStartService, a.startTailscaled)
			}
			a.retryButton(c.Key("ts-retry"))
		}
	case ph == tsStarting:
		title, busy = T.TSLStartingTitle, true
	case ph == tsStopped:
		title, body = T.TSLStoppedTitle, T.TSLStoppedBody
		actions = func() { primary("ts-connect", T.TSLConnect, func() { a.connectTailscale(s) }) }
	case ph == tsNeedsLogin:
		title, body = T.TSLNeedsLoginTitle, T.TSLNeedsLoginBody
		actions = login(false, T.TSLLogIn)
	case ph == tsLoggedOut:
		title, body = T.TSLLoggedOutTitle, T.TSLLoggedOutBody
		actions = login(false, T.TSLLogIn)
	case ph == tsKeyExpired:
		title, body = T.TSLExpiredTitle, T.TSLExpiredBody
		icon, iconCol = iconWarning, pal(t).warnText
		actions = login(false, T.TSLRelogin)
	case ph == tsMachineAuth:
		title, body = T.TSLMachineAuthTitle, T.TSLMachineAuthBody
		busy = true
		actions = func() {
			secondary("ts-admin", T.TSLAdminConsole, func() { a.openExternal(tailscaleAdminURL) })
			a.retryButton(c.Key("ts-retry"))
		}
	default: // running
		if !o.ShowRunning {
			return
		}
		st := s.Tailscale.State
		title, icon, iconCol = T.TSState(st.BackendState), iconOK, pal(t).okText
		body = st.User
		if st.Tailnet != "" {
			body += " · " + st.Tailnet
		}
	}
	a.tsPanelBox(c, o, tsBox{title: title, body: body, icon: icon, iconCol: iconCol, raw: raw, busy: busy, actions: actions,
		tip: f.state == flowIdle && (ph == tsNotInstalled || ph == tsNeedsLogin)})
}

// tsBox is what one state of the panel shows.
type tsBox struct {
	title, body string
	icon        *ui.SVG
	iconCol     ui.Color
	raw         string // the system's text, behind 详细信息
	busy        bool   // a spinner beside the title: the state moves by itself
	tip         bool   // ⓘ after the title: what Tailscale is (states a newcomer meets)
	actions     func()
}

// tsPanelBox lays a state out: a page's centered box, or a card's rows.
func (a *app) tsPanelBox(c *ui.Context, o tsPanelOpts, b tsBox) {
	t := c.Theme()
	T := i18n.T
	titleRow := func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if b.busy {
				spinnerIcon(c)
			}
			ui.Text(c, b.title).FontWeight(600).SingleLine().Shrink(1)
			if b.tip {
				infoTip(c, T.Tailscale, T.TSLWhat, nil)
			}
		})
	}
	if o.Compact {
		ui.Column(c.Key("ts-panel")).Gap(6).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, b.icon).FontSize(iconInline).TextColor(b.iconCol).Margin(2, 0, 0, 0).Shrink(0)
				ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
					titleRow()
					if b.body != "" {
						ui.Text(c, b.body).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
					}
				})
			})
			if b.actions != nil {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(b.actions)
			}
			if b.raw != "" {
				rawDetails(c.Key("ts-raw"), b.raw)
			}
		})
		return
	}
	ui.Column(c.Key("ts-panel")).Gap(6).AlignItems(ui.Center).Padding(28, 16).Children(func() {
		ui.Icon(c, b.icon).FontSize(iconLarge).TextColor(b.iconCol)
		titleRow()
		if b.body != "" {
			ui.Text(c, b.body).FontSize(plat.calloutSize).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap().LineHeight(lineText).MaxWidth(420)
		}
		if b.actions != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(b.actions)
		}
		if b.raw != "" {
			rawDetails(c.Key("ts-raw"), b.raw).MaxWidth(420).Margin(4, 0, 0, 0)
		}
	})
}

// tsWaitingPanel is the login under way: finish it in the browser, the
// link (copy, open again), the QR code for a phone, 取消.
func (a *app) tsWaitingPanel(c *ui.Context, o tsPanelOpts) {
	t := c.Theme()
	T := i18n.T
	f := &a.tsl
	link := func() {
		if f.url == "" {
			ui.Text(c, T.TSLFetchingURL).FontSize(plat.calloutSize).TextColor(t.TextMuted)
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).MaxWidth(460).Children(func() {
			mono(middleText(c, f.url)).FontSize(plat.calloutSize).TextColor(t.TextMuted).Selectable().Label(T.TSLLinkLabel).Shrink(1).Tooltip(f.url)
		})
	}
	buttons := func() {
		if f.url != "" {
			url := f.url
			if pushButton(c.Key("ts-copy"), T.TSLCopyLink).Clicked() {
				copyText(c, url)
			}
			if pushButton(c.Key("ts-open"), T.TSLOpenAgain).Clicked() {
				a.openExternal(url)
			}
		}
		if pushButton(c.Key("ts-cancel"), T.Cancel).Clicked() {
			a.cancelTSLogin()
		}
	}
	title := func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			spinnerIcon(c)
			ui.Text(c, T.TSLWaitingTitle).FontWeight(600).SingleLine().Shrink(1)
		})
	}
	if o.Compact {
		ui.Column(c.Key("ts-panel")).Gap(6).Children(func() {
			title()
			link()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(buttons)
		})
		return
	}
	ui.Column(c.Key("ts-panel")).Gap(8).AlignItems(ui.Center).Padding(24, 16).Children(func() {
		title()
		link()
		if f.url != "" {
			if bm := f.qrBitmap(f.url); bm != nil {
				ui.Column(c).Gap(6).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
					ui.Image(c, bm).Size(148, 148).Radius(plat.innerRadius).Label(T.TSLScanQR)
					ui.Text(c, T.TSLScanQR).FontSize(plat.smallSize).TextColor(t.TextMuted)
				})
			}
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(buttons)
	})
}

// tsReauthButton logs in again on a running node whose key expires soon,
// which renews the key.
func (a *app) tsReauthButton(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	b := disabledWhy(c, !s.WritesAllowed, T.WritesDisabledHint, func() ui.Element {
		return pushButton(c.Key("ts-reauth"), T.TSLRelogin)
	})
	if b.Clicked() && s.WritesAllowed {
		a.startTSLogin(s, true)
		a.openPage(pageTailscale)
	}
}
