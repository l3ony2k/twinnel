package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/version"
)

// Paths that tell an installed helper from none (see `duet helper install
// --dry-run`): the binary and its LaunchDaemon plist.
var helperInstalledPaths = []string{
	"/Library/PrivilegedHelperTools/com.l3ony2k.duet.helper",
	"/Library/LaunchDaemons/com.l3ony2k.duet.helper.plist",
}

// startHelper starts the one helper connection of the app: dial (socket
// from DUET_SOCKET, else the default), one events.wait long-poll, data
// reloads after events. It lives across monitor restarts. Main thread.
func (a *app) startHelper() {
	hc := &status.HelperConn{Socket: rpc.SocketFromEnv()}
	// Installed: the LaunchDaemon plist or the binary exists (readable
	// without root). A development helper named by DUET_SOCKET counts as
	// installed while its socket exists.
	paths := helperInstalledPaths
	if os.Getenv("DUET_SOCKET") != "" {
		paths = []string{hc.Socket}
	}
	hc.Installed = status.FileExists(paths...)
	hc.OnChange = func(h status.Helper) {
		mygo.RunOnMain(func() {
			wasUp := a.hd.connected
			a.hd.connected = h.Connected()
			if a.mon != nil {
				a.mon.SetHelper(h)
			}
			if h.Connected() != wasUp && a.mon != nil {
				a.runHealth() // switch between the helper's report and local checks
			}
			if h.Connected() && !wasUp {
				a.events.backfilled = false // a new connection: fetch the helper's buffer again
				a.refreshCredentials()
				a.loadHelperData()
				a.checkUnapplied()
				switch a.page {
				case pageKernel:
					a.loadServices()
				case pageTakeover:
					a.loadTakeover()
				}
			}
			if h.State == status.HelperNotInstalled && !a.hd.onb.loaded {
				a.loadInstallPlan() // for the first-run view
			}
			a.invalidate()
		})
	}
	hc.OnEvents = func(evs []rpc.Event) {
		mygo.RunOnMain(func() {
			a.hd.lastEvent = evs[len(evs)-1]
			a.recordEvents(evs)
			if credentialsEvent(evs) {
				a.refreshCredentials()
			}
			if serviceEvent(evs) && a.page == pageKernel {
				a.loadServices()
			}
			if takeoverEvent(evs) && a.page == pageTakeover {
				a.loadTakeover()
			}
			if healthEvent(evs) && a.mon != nil {
				a.runHealth() // reads health.last; nothing runs locally
			}
			if composeInputEvent(evs) {
				a.checkUnapplied()
			}
			a.loadHelperData()
		})
	}
	a.hc = hc
	go hc.Run(context.Background())
}

// helperData is what the helper pages show, loaded over RPC. Main thread.
type helperData struct {
	connected bool
	loading   bool
	loadErr   string
	lastEvent rpc.Event

	subs     []rpc.Subscription
	layers   []rpc.Layer
	history  []rpc.HistoryEntry
	kernels  *rpc.KernelList
	settings *rpc.Settings

	// loaded turns true once the first load answered (until then the
	// lists are unknown, not empty); loadStart is when the app first
	// waited for it.
	loaded    bool
	loadStart time.Time

	// busy names the running operation; helper buttons are disabled
	// meanwhile. msg is the last operation's outcome, warns its warnings.
	busy   string
	msg    string
	msgErr bool
	warns  []string
	// span times the last operation (spanOp) for opButton's spinner;
	// busyOnButton is set once an opButton showed the running operation.
	span         busySpan
	spanOp       string
	busyOnButton bool

	// unapplied: the composed config differs from the file mihomo runs
	// (compose.preview's Changed), so 应用 has something to do.
	unapplied      bool
	unappliedCheck bool // a check runs
	unappliedAgain bool // something changed during it: check again
	unappliedErr   bool // the last check failed: whether 应用 has work is unknown

	cfg   configView
	hist  historyView
	kern  kernelView
	take  takeoverView
	onb   onboardingView
	leave leaveView
}

// loadHelperData reloads the cheap lists (all local reads in the helper).
// Main thread.
func (a *app) loadHelperData() {
	if !a.hd.connected || a.hd.loading {
		return
	}
	a.hd.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var (
			subs    []rpc.Subscription
			layers  []rpc.Layer
			hist    []rpc.HistoryEntry
			kernels rpc.KernelList
			set     rpc.Settings
		)
		err := errors.Join(
			hc.Call(ctx, rpc.MethodSubList, nil, &subs),
			hc.Call(ctx, rpc.MethodLayerList, nil, &layers),
			hc.Call(ctx, rpc.MethodHistoryList, nil, &hist),
			hc.Call(ctx, rpc.MethodKernelList, nil, &kernels),
			hc.Call(ctx, rpc.MethodSettingsGet, nil, &set),
		)
		mygo.RunOnMain(func() {
			d := &a.hd
			d.loading, d.loaded = false, true
			d.subs, d.layers, d.history, d.kernels, d.settings = subs, layers, hist, &kernels, &set
			d.loadErr = ""
			if err != nil {
				d.loadErr = status.ShortError(err)
			}
			a.layersLoaded()
			a.invalidate()
		})
	}()
}

// helperOp runs one helper operation off the main thread: label names it
// (shown while busy; opButton(…, label) shows it on the button), fn does
// the calls, done (optional) runs on the main thread with fn's error.
// Reloads the lists afterwards.
func (a *app) helperOp(label string, timeout time.Duration, fn func(ctx context.Context, hc *status.HelperConn) error, done func(error)) {
	a.helperOpW(label, timeout, func(ctx context.Context, hc *status.HelperConn) ([]string, error) {
		return nil, fn(ctx, hc)
	}, done)
}

// helperOpW is helperOp for an operation that returns warnings (e.g.
// subscription.put's plain-http warning): opNotes shows them, in the UI
// language, until the next operation.
func (a *app) helperOpW(label string, timeout time.Duration, fn func(ctx context.Context, hc *status.HelperConn) ([]string, error), done func(error)) {
	d := &a.hd
	if d.busy != "" {
		return
	}
	d.busy, d.msg, d.msgErr, d.warns = label, "", false, nil
	a.beginOp(label)
	hc, page, start := a.hc, a.page, time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		warns, err := fn(ctx, hc)
		mygo.RunOnMain(func() {
			d.busy = ""
			a.endOp()
			d.warns = warns
			text, quiet := opResult(label)
			if err != nil {
				d.msg, d.msgErr = i18n.Cur().ErrorPrefix+helperError(err), true
				if a.page != page && a.win != nil { // the note is on a page no longer shown (H7)
					a.toastErr = fmt.Sprintf(i18n.Cur().OpFailedTitle, label)
				}
			} else if !quiet && a.win != nil {
				a.toast, a.toastOp = text, true
			}
			a.notifyOpEnd(label, err, time.Since(start), page)
			if done != nil {
				done(err)
			}
			a.loadHelperData()
			a.invalidate()
		})
	}()
}

// opResult words an operation's success toast and reports whether it
// needs none: a read shows its result on the page, and a switch, an order
// or a 使用中 tag already shows it where the person is looking (kubai 016).
func opResult(label string) (text string, quiet bool) {
	T := i18n.T
	if readOnlyOp(label) {
		return "", true
	}
	toggle, _, _ := strings.Cut(T.LayerToggle, "%s")
	switch {
	case label == T.LayerReorder, strings.HasPrefix(label, toggle), strings.HasPrefix(label, T.SubActivate+" "):
		return "", true
	case label == T.Apply:
		return T.DoneApplied, false
	case label == T.HistoryRollback:
		return T.DoneRolledBack, false
	}
	for _, p := range []struct{ prefix, done string }{
		{T.SubRefresh + " ", T.DoneSubRefreshed},
		{T.SubSave + " ", T.DoneSubSaved},
		{T.Remove + " ", T.DoneDeleted},
		{T.Save + " ", T.DoneLayerSaved},
	} {
		if item, ok := strings.CutPrefix(label, p.prefix); ok && item != "" {
			return fmt.Sprintf(p.done, item), false
		}
	}
	return fmt.Sprintf(T.OpDone, label), false
}

// checkUnapplied asks the helper whether the composed config differs from
// the one mihomo runs (compose.preview, read-only), for unappliedNote.
// One check at a time; a change meanwhile asks again. Main thread.
func (a *app) checkUnapplied() {
	d := &a.hd
	if !d.connected {
		return
	}
	if d.unappliedCheck {
		d.unappliedAgain = true
		return
	}
	d.unappliedCheck = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var r rpc.ComposeResult
		err := hc.Call(ctx, rpc.MethodComposePreview, nil, &r)
		mygo.RunOnMain(func() {
			d.unappliedCheck = false
			d.unapplied = err == nil && r.Changed // unknown reads as nothing to apply
			d.unappliedErr = err != nil
			if d.unappliedAgain {
				d.unappliedAgain = false
				a.checkUnapplied()
			}
			a.invalidate()
		})
	}()
}

// composeInputEvent reports whether events change what compose builds, or
// what mihomo runs: the unapplied mark is then checked again.
func composeInputEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		switch e.Type {
		case rpc.EventStateChanged, rpc.EventSubUpdated, rpc.EventTailnetChanged,
			rpc.EventComposeApplied, rpc.EventComposeFailed, rpc.EventTakeoverChanged:
			return true
		}
	}
	return false
}

// readOnlyOp reports whether an operation only reads (a preview, a diff, a
// check): its result shows on the page, so it gets no toast.
func readOnlyOp(label string) bool {
	T := i18n.T
	switch label {
	case T.Preview, T.HistoryDiff, T.TakeoverOpen, T.KernelCheck, T.TakeoverPlan:
		return true
	}
	return strings.HasSuffix(label, " · "+T.KernelCheck)
}

// helperError shortens an RPC error for the UI: the helper's message
// without the "rpc error -32002:" prefix.
func helperError(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		if re.Code == rpc.CodeMethodNotFound || re.Code == rpc.CodeNotImplemented {
			if helperOutdatedNow {
				return i18n.T.HelperNeedsUpdate
			}
			return i18n.T.HelperTooOld
		}
		return re.Message
	}
	return status.ShortError(err)
}

// confirm asks before a destructive action; ok runs on the main thread.
func (a *app) confirm(title, detail, okLabel string, ok func()) {
	parent := a.win
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             parent,
			Type:               mygo.MessageWarning,
			Message:            title,
			Detail:             detail,
			Buttons:            []string{i18n.Cur().Cancel, okLabel},
			DestructiveButtons: []int{1},
			CancelButton:       0,
			DefaultButton:      0,
		})
		if err == nil && res.Button == 1 {
			mygo.RunOnMain(ok)
		}
	}()
}

// helperWrites reports whether helper write buttons are enabled: connected,
// writes allowed, nothing running.
func (a *app) helperWrites(s status.Snapshot) bool {
	return s.Helper.Connected() && s.WritesAllowed && a.hd.busy == ""
}

// opNotes shows the running operation (when no
// opButton shows it), and the last error and warnings of the helper
// pages. Successes go to a toast (helperOp).
func (a *app) opNotes(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	d := &a.hd
	if d.busy != "" && !d.busyOnButton && d.span.visible(time.Now()) {
		// No opButton shows this operation: say it here (after busyDelay).
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(4).Children(func() {
			ui.Spinner(c)
			small(c, d.busy+"…")
		})
	}
	for i, w := range d.warns {
		warningNote(c.Key(fmt.Sprint("op-warn-", i)), w)
	}
	if d.msg != "" {
		if d.msgErr {
			errorNote(c, d.msg)
		} else {
			ui.Text(c, d.msg).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
		}
	}
	if d.loadErr != "" && !helperOutdated(s) {
		// An outdated helper lacks newer methods: its version row says
		// so (helperUpdateRow), not an error on every page.
		errorNote(c, i18n.T.ErrorPrefix+d.loadErr)
	}
}

// helperGate draws the helper's state instead of a page when it is not
// connected, and reports whether the page can draw. Three states: not
// installed (the install guide), not running (重新启动, 查看日志 when the
// log exists, 复制诊断信息), and connected. No socket path or raw error
// on screen: those go to 复制诊断信息.
func (a *app) helperGate(c *ui.Context, s status.Snapshot) bool {
	h := s.Helper
	switch h.State {
	case status.HelperConnected:
		return true
	case status.HelperNotInstalled:
		a.onboarding(c, s)
		return false
	}
	t := c.Theme()
	T := i18n.T
	groupBox(c, t).Padding(12, groupPadX).Gap(8).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(32).Children(func() {
			switch h.State {
			case status.HelperUnknown:
				ui.Spinner(c)
				ui.Text(c, T.Helper+" · "+h.State.Text()).FontWeight(600)
			case status.HelperDenied:
				stateDot(c, markAttention, t.Warning, 10)
				ui.Text(c, T.Helper+" · "+h.State.Text()).FontWeight(600)
			default:
				stateDot(c, markAttention, t.Warning, 10)
				ui.Text(c, T.HelperStateNotRunning).FontWeight(600)
			}
			ui.Spacer(c)
			if actionLink(c.Key("diag"), T.HelperCopyDiag).Clicked() {
				copyText(c, a.helperDiagnostics(s))
			}
			if h.State == status.HelperUnknown {
				return
			}
			if fileExists(a.helperLogFile()) && pushButton(c.Key("log"), T.HelperViewLog).Clicked() {
				a.openHelperLog()
			}
			if h.State == status.HelperNotRunning {
				if !s.WritesAllowed {
					ui.Icon(c, iconSecure).FontSize(iconInline).TextColor(t.TextMuted).Tooltip(T.InstallNeedsWrites)
				}
				if busyButton(c.Key("restart"), T.HelperRestartAdmin, true, a.hm.running == T.HelperRestart, !a.helperOpsEnabled(s)).Clicked() {
					a.restartHelper()
				}
			} else if pushButton(c.Key("retry"), T.HelperRetry).Clicked() {
				a.hc.Retry()
			}
		})
		if h.State == status.HelperDenied {
			small(c, T.HelperDeniedHint).Wrap().LineHeight(lineText)
		}
		if a.hm.errMsg != "" {
			errorNote(c, a.hm.errMsg)
		}
	})
	return false
}

// helperDiagnostics is 复制诊断信息: socket, state, error and versions,
// for a bug report.
func (a *app) helperDiagnostics(s status.Snapshot) string {
	h := s.Helper
	var b strings.Builder
	fmt.Fprintf(&b, "duet %s\n", version.Version)
	fmt.Fprintf(&b, "helper state: %d\n", h.State)
	fmt.Fprintf(&b, "socket: %s\n", h.Socket)
	if h.Err != "" {
		fmt.Fprintf(&b, "error: %s\n", h.Err)
	}
	if st := h.Status; st != nil {
		fmt.Fprintf(&b, "helper version: %s\n", st.HelperVersion)
	}
	fmt.Fprintf(&b, "log: %s\n", a.helperLogFile())
	for _, p := range helperInstalledPaths {
		fmt.Fprintf(&b, "%s exists: %v\n", p, fileExists(p))
	}
	return b.String()
}

// --- config text (for the diff views) ---

// Against an older helper without config.get, "live" falls back to reading
// the config file when this user can, and the others say the helper is too
// old.
var secretLine = regexp.MustCompile(`(?m)^secret:.*\n?`)

// fetchConfig returns the text of a config (rpc.MethodConfigGet).
func fetchConfig(ctx context.Context, hc *status.HelperConn, configPath, source, id string) (string, error) {
	var r rpc.ConfigText
	err := hc.Call(ctx, rpc.MethodConfigGet, rpc.ConfigGetParams{Source: source, ID: id}, &r)
	if err == nil {
		return r.Content, nil
	}
	var re *rpc.Error
	if !errors.As(err, &re) {
		return "", err
	}
	if source == rpc.ConfigLive && re.Code == rpc.CodeNotFound {
		return "", nil // nothing applied yet: the diff shows all lines added
	}
	if re.Code != rpc.CodeMethodNotFound {
		return "", err
	}
	if source == rpc.ConfigLive && configPath != "" {
		b, ferr := os.ReadFile(configPath)
		if ferr == nil {
			return secretLine.ReplaceAllString(string(b), ""), nil
		}
		if os.IsNotExist(ferr) {
			return "", nil // nothing applied yet: the diff shows all lines added
		}
	}
	return "", errors.New(i18n.T.ConfigTextUnsupported)
}
