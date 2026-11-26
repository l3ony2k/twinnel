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
	if os.Getenv("DUET_SOCKET") == "" {
		hc.Installed = status.FileExists(helperInstalledPaths...)
	}
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
				a.refreshCredentials()
				a.loadHelperData()
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

	// busy names the running operation; helper buttons are disabled
	// meanwhile. msg is the last operation's outcome.
	busy   string
	msg    string
	msgErr bool

	cfg  configView
	hist historyView
	kern kernelView
	take takeoverView
	onb  onboardingView
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
			d.loading = false
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
// (shown while busy), fn does the calls, done (optional) runs on the main
// thread with fn's error. Reloads the lists afterwards.
func (a *app) helperOp(label string, timeout time.Duration, fn func(ctx context.Context, hc *status.HelperConn) error, done func(error)) {
	d := &a.hd
	if d.busy != "" {
		return
	}
	d.busy, d.msg, d.msgErr = label, "", false
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := fn(ctx, hc)
		mygo.RunOnMain(func() {
			d.busy = ""
			if err != nil {
				d.msg, d.msgErr = i18n.T.ErrorPrefix+helperError(err), true
			} else if !readOnlyOp(label) {
				a.toast = fmt.Sprintf(i18n.T.OpDone, label)
			}
			if done != nil {
				done(err)
			}
			a.loadHelperData()
			a.invalidate()
		})
	}()
}

// readOnlyOp reports whether an operation only reads (a preview, a diff, a
// check): its result shows on the page, so it gets no toast.
func readOnlyOp(label string) bool {
	T := i18n.T
	switch label {
	case T.Preview, T.HistoryDiff, T.TakeoverOpen, T.KernelCheck:
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
			Buttons:            []string{i18n.T.Cancel, okLabel},
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

// opNotes shows the read-only notice, the running operation and the last
// error of the helper pages. Successes go to a toast (helperOp).
func (a *app) opNotes(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	d := &a.hd
	if !s.WritesAllowed {
		notice(c, iconSecure, i18n.T.WritesDisabledHint)
	}
	if d.busy != "" {
		ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingX(4).Children(func() {
			ui.Spinner(c)
			small(c, d.busy+"…")
		})
	}
	if d.msg != "" {
		if d.msgErr {
			errorNote(c, d.msg)
		} else {
			ui.Text(c, d.msg).TextColor(t.TextMuted).Wrap().Selectable()
		}
	}
	if d.loadErr != "" {
		errorNote(c, i18n.T.ErrorPrefix+d.loadErr)
	}
}

// helperGate draws the helper's state instead of a page when it is not
// connected, and reports whether the page can draw.
func (a *app) helperGate(c *ui.Context, s status.Snapshot) bool {
	h := s.Helper
	switch h.State {
	case status.HelperConnected:
		return true
	case status.HelperNotInstalled:
		a.onboarding(c, s)
	default:
		t := c.Theme()
		card(c, "", func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				col := t.Warning
				if h.State == status.HelperUnknown {
					col = t.TextMuted
				}
				dot(c, col, 10)
				ui.Text(c, i18n.T.Helper+" · "+h.State.Text()).FontSize(15).Bold()
			})
			switch h.State {
			case status.HelperNotRunning:
				ui.Text(c, i18n.T.HelperNotRunningHint).FontSize(13).Wrap()
			case status.HelperDenied:
				ui.Text(c, i18n.T.HelperDeniedHint).FontSize(13).Wrap()
			}
			kv(c, i18n.T.HelperSocket, h.Socket)
			if h.Err != "" {
				ui.Text(c, h.Err).FontSize(12).TextColor(t.TextMuted).Wrap().Selectable()
			}
			if ui.Button(c.Key("retry"), i18n.T.HelperRetry).Clicked() {
				a.hc.Retry()
			}
		})
	}
	return false
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
