package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/uninstall"
	"github.com/l3ony2k/twinnel/core/version"
)

// The 后台助手 section of 设置: the helper's state and version (against the
// app's), 更新 when the helper bundled with the app is newer, 重启, 查看日志,
// 卸载… and 完全移除 Twinnel…. Every write goes through the CLI (`twinnel helper
// upgrade|restart|uninstall`), which asks for the administrator password
// once, as install does. A helper whose version differs from the app's is
// one row (需要更新后台助手 with 更新) in the sidebar footer and on the
// overview, not errors on every page.

// helperMgmtView is the section's state. Main thread.
type helperMgmtView struct {
	check    *upgradeCheck // `twinnel helper upgrade --check --json`; nil until read
	checking bool
	checkPID int // the helper process the check and info belong to

	info *rpc.HelperInfo // helper.info (nil: not read, or an older helper)

	running string // the label of the running operation
	errMsg  string

	loginErr string
}

// upgradeCheck mirrors the CLI's UpgradeCheck.
type upgradeCheck struct {
	Bundled   string `json:"bundled"`
	Installed string `json:"installed"`
	Running   string `json:"running"`
	Action    string `json:"action"`
}

// refreshHelperMgmt reads the upgrade check and helper.info again when
// the helper process changed since the last read. Main thread.
func (a *app) refreshHelperMgmt(s status.Snapshot) {
	v := &a.hm
	pid := 0
	if st := s.Helper.Status; st != nil {
		pid = st.PID
	}
	if v.checking || (v.check != nil && v.checkPID == pid) {
		return
	}
	v.checking, v.checkPID = true, pid
	hc, connected := a.hc, s.Helper.Connected()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var chk *upgradeCheck
		if out, err := a.runCLI(ctx, upgradeArgs("--check", "--json")...); err == nil {
			var c upgradeCheck
			if json.Unmarshal([]byte(out), &c) == nil {
				chk = &c
			}
		}
		var info *rpc.HelperInfo
		if connected && hc != nil {
			var i rpc.HelperInfo
			if hc.Call(ctx, rpc.MethodHelperInfo, nil, &i) == nil {
				info = &i
			}
		}
		mygo.RunOnMain(func() {
			v.checking = false
			if chk == nil {
				chk = &upgradeCheck{} // unknown: no 更新
			}
			v.check, v.info = chk, info
			a.invalidate()
		})
	}()
}

// upgradeArgs are `twinnel helper upgrade` with the helper bundled next to
// the CLI, when there is one.
func upgradeArgs(extra ...string) []string {
	args := []string{"helper", "upgrade"}
	if cli, err := cliPath(); err == nil {
		if p := filepath.Join(filepath.Dir(cli), "twinnel-helper"); fileExists(p) {
			args = append(args, "--helper-bin", p)
		}
	}
	return append(args, extra...)
}

// legacyHelper: the helper is the one installed before the rename (its
// socket is the old one). 更新 migrates it.
func legacyHelper(s status.Snapshot) bool { return s.Helper.Socket == legacy.Socket }

// helperMatch is how the connected helper's version relates to the app's.
// A helper from before the rename is older whatever its version says.
func helperMatch(s status.Snapshot) version.Match {
	if legacyHelper(s) && s.Helper.State != status.HelperNotInstalled {
		return version.MatchHelperOlder
	}
	if !s.Helper.Connected() || s.Helper.Status == nil {
		return version.MatchSame
	}
	return version.Handshake("", s.Helper.Status.HelperVersion)
}

// helperOutdatedNow is helperOutdated of the last frame's snapshot, for
// words chosen off the frame (helperError). Main thread.
var helperOutdatedNow bool

// helperOutdated: the one version state the UI shows.
func helperOutdated(s status.Snapshot) bool { return helperMatch(s) == version.MatchHelperOlder }

// canUpdateHelper: the bundled helper differs from the installed one and
// is not older (the CLI refuses a downgrade).
func (a *app) canUpdateHelper() bool {
	c := a.hm.check
	return c != nil && (c.Action == "upgrade" || c.Action == "restart" || c.Action == "migrate")
}

// helperOpsEnabled: writes allowed and nothing running.
func (a *app) helperOpsEnabled(s status.Snapshot) bool {
	return s.WritesAllowed && a.hm.running == "" && a.hd.busy == ""
}

// helperCLIOp runs a `twinnel helper …` command that elevates on its own;
// done runs on the main thread after success.
func (a *app) helperCLIOp(label, doneText string, args []string, done func()) {
	v := &a.hm
	if !a.cfg.AllowWrites || v.running != "" {
		return
	}
	v.running, v.errMsg = label, ""
	a.beginOp(label)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		out, err := a.runCLI(ctx, args...)
		mygo.RunOnMain(func() {
			v.running = ""
			a.endOp()
			if err != nil {
				v.errMsg = i18n.T.ErrorPrefix + cliFailure(out, err)
			} else {
				if a.win != nil {
					a.toast = doneText
				}
				if done != nil {
					done()
				}
			}
			v.check = nil // read again for the new process
			a.hc.Retry()
			a.invalidate()
		})
	}()
}

// cliFailure is the CLI's own last error line ("twinnel: …"), else err.
func cliFailure(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		// The line may follow a prompt ("Proceed? [y/N] twinnel: …").
		if j := strings.Index(lines[i], "twinnel: "); j >= 0 {
			return strings.TrimSpace(lines[i][j+len("twinnel: "):])
		}
	}
	if err == nil {
		return strings.TrimSpace(out)
	}
	return status.ShortError(err)
}

func (a *app) updateHelper() {
	a.helperCLIOp(i18n.T.HelperUpdate, i18n.T.DoneHelperUpdated, upgradeArgs("--yes"), nil)
}

func (a *app) restartHelper() {
	a.helperCLIOp(i18n.T.HelperRestart, i18n.T.DoneHelperRestarted, []string{"helper", "restart", "--yes"}, nil)
}

// helperLogFile is the helper's log: from helper.info, else the default.
func (a *app) helperLogFile() string {
	if i := a.hm.info; i != nil && i.LogFile != "" {
		return i.LogFile
	}
	return filepath.Join(store.DefaultDir, "logs", "helper.log")
}

// openHelperLog opens the log in its default app (Console); without the
// file, its folder in Finder.
func (a *app) openHelperLog() {
	p := a.helperLogFile()
	if fileExists(p) {
		if err := mygo.Shell.OpenPath(p); err == nil {
			return
		}
	}
	mygo.Shell.ShowItemInFolder(filepath.Dir(p))
}

// --- uninstall ---

// uninstallPlan asks the CLI what uninstalling does (read-only:
// --dry-run --json), with the move back of a hardened mihomo whenever it
// is needed: the app never offers to leave mihomo unmanaged.
func (a *app) uninstallPlan(ctx context.Context, purge bool) (*uninstall.Plan, *uninstall.Refusal, error) {
	for _, restore := range []bool{false, true} {
		args := []string{"helper", "uninstall", "--dry-run", "--json"}
		if purge {
			args = append(args, "--purge")
		}
		if restore {
			args = append(args, "--restore-mihomo")
		}
		out, err := a.runCLI(ctx, args...)
		if err != nil {
			return nil, nil, errors.New(cliFailure(out, err))
		}
		var r struct {
			Plan    *uninstall.Plan    `json:"plan"`
			Refusal *uninstall.Refusal `json:"refusal"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			return nil, nil, err
		}
		if r.Refusal != nil && r.Refusal.Code == uninstall.NeedsRestore && !restore {
			continue
		}
		return r.Plan, r.Refusal, nil
	}
	return nil, nil, errors.New("no plan")
}

// uninstallFlags are the CLI flags that run a plan.
func uninstallFlags(p *uninstall.Plan, purge bool) []string {
	args := []string{"helper", "uninstall", "--yes"}
	if purge {
		args = append(args, "--purge")
	}
	if p.Restore != nil {
		args = append(args, "--restore-mihomo")
	}
	return args
}

// planText is the dialog's detail: what goes and what stays, one line
// each. all adds the app's own pieces (完全移除).
func planText(p *uninstall.Plan, all bool) string {
	T := i18n.T
	var rm, keep []string
	add := func(list *[]string, it uninstall.Item) {
		switch it.Kind {
		case uninstall.HelperService:
			*list = append(*list, T.ItemHelper)
		case uninstall.StateDir:
			if it.Subscriptions > 0 || it.Layers > 0 {
				*list = append(*list, i18n.F(T.ItemDataCounts, it.Subscriptions, it.Layers))
			} else {
				*list = append(*list, T.ItemData)
			}
		case uninstall.Secrets:
			*list = append(*list, T.ItemSecrets)
		case uninstall.TwinnelMihomo:
			if list == &rm && p.Restore != nil {
				*list = append(*list, fmt.Sprintf(T.ItemTwinnelMihomo, p.Restore.ToLabel))
			} else {
				*list = append(*list, T.ItemTwinnelMihomoKeep)
			}
		case uninstall.OriginalMihomo:
			*list = append(*list, T.ItemOriginalMihomo)
		case uninstall.Tailscale:
			*list = append(*list, T.ItemTailscale)
		case uninstall.App:
			if !all {
				*list = append(*list, T.ItemApp)
			}
		}
	}
	for _, it := range p.Remove {
		add(&rm, it)
	}
	for _, it := range p.Keep {
		add(&keep, it)
	}
	if all {
		rm = append(rm, appExtraItems()...)
		rm = append(rm, T.ItemLoginItem, T.ItemAppTrash)
	}
	text := fmt.Sprintf(T.UninstallRemoves, strings.Join(rm, T.ListSep))
	if len(keep) > 0 {
		text += "\n" + fmt.Sprintf(T.UninstallKeeps, strings.Join(keep, T.ListSep))
	}
	return text
}

// refusalText words a refusal for the dialog.
func refusalText(r *uninstall.Refusal) string {
	if r.Code == uninstall.OwnMihomo {
		return i18n.T.UninstallOwnMihomo
	}
	return fmt.Sprintf(i18n.T.UninstallRefused, r.Message)
}

// askUninstall: 卸载…. The dialog lists what goes and what stays without
// the data; its checkbox 同时删除数据 adds the data (checked again with
// --purge before it runs).
func (a *app) askUninstall() {
	a.askRemove(false)
}

// askRemoveAll: 完全移除 Twinnel…: the helper and the data (with the move
// back of a hardened mihomo), then the app's own settings, 登录时打开 and
// the app itself (to the Trash), and the app quits.
func (a *app) askRemoveAll() {
	a.askRemove(true)
}

func (a *app) askRemove(all bool) {
	v := &a.hm
	if !a.cfg.AllowWrites || v.running != "" {
		return
	}
	T := i18n.T
	label := T.HelperUninstall
	if all {
		label = T.HelperRemoveAll
	}
	v.running, v.errMsg = label, ""
	parent := a.win
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		fail := func(msg string) {
			mygo.RunOnMain(func() { v.running, v.errMsg = "", msg; a.invalidate() })
		}
		plan, refusal, err := a.uninstallPlan(ctx, all)
		if err != nil {
			fail(T.ErrorPrefix + err.Error())
			return
		}
		if refusal != nil {
			fail(refusalText(refusal))
			return
		}
		title, ok := T.UninstallTitle, T.UninstallConfirm
		opts := mygo.MessageOptions{Parent: parent, Type: mygo.MessageWarning}
		if all {
			title, ok = T.RemoveAllTitle, T.RemoveAllConfirm
		} else {
			opts.CheckboxLabel = T.UninstallPurge
		}
		opts.Message, opts.Detail = title, planText(plan, all)
		opts.Buttons = []string{T.Cancel, ok}
		opts.DestructiveButtons, opts.CancelButton, opts.DefaultButton = []int{1}, 0, 0
		res, err := mygo.Dialog.Message(opts)
		if err != nil || res.Button != 1 {
			mygo.RunOnMain(func() { v.running = ""; a.invalidate() })
			return
		}
		purge := all || res.CheckboxChecked
		if purge && !all {
			// The data was only a checkbox: check the purge plan too.
			plan, refusal, err = a.uninstallPlan(ctx, true)
			switch {
			case err != nil:
				fail(T.ErrorPrefix + err.Error())
				return
			case refusal != nil:
				fail(refusalText(refusal))
				return
			}
		}
		args := uninstallFlags(plan, purge)
		mygo.RunOnMain(func() {
			v.running = ""
			done := func() {}
			if all {
				done = a.removeAppPieces
			}
			a.helperCLIOp(label, T.DoneHelperUninstalled, args, done)
		})
	}()
}

// removeAppPieces finishes 完全移除: 登录时打开 off, the app's settings
// gone, the app bundle to the Trash, and quit. A development run (no
// bundle) only quits.
func (a *app) removeAppPieces() {
	if a.shotMode {
		return
	}
	if appBundled() {
		mygo.App.SetOpenAtLogin(false)
	}
	os.Remove(a.cfgPath)
	removeAppExtras()
	if b := appBundlePath(); b != "" {
		mygo.Shell.TrashItem(b)
	}
	mygo.App.Quit()
}

// --- 登录时打开 ---

// appBundlePath is the .app the app runs from, "" for a development run.
func appBundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}

// appBundled: 登录时打开 needs an app bundle (SMAppService).
var appBundled = func() bool { return appBundlePath() != "" }

// needsBundleTag is the 需打包版 chip for a control a dev run can't use,
// and "" in a packaged app: there, a control that can't work is simply
// off, never "needs the packaged app" (walkthrough 2, N28).
func needsBundleTag(bundled bool) string {
	if bundled {
		return ""
	}
	return i18n.T.NotificationsUnsupported
}

func (a *app) setOpenAtLogin(on bool) {
	a.hm.loginErr = ""
	if err := mygo.App.SetOpenAtLogin(on); err != nil {
		a.hm.loginErr = err.Error()
		if strings.Contains(err.Error(), "Login Items") {
			a.hm.loginErr = i18n.T.OpenAtLoginApprove
		}
	}
}

// --- drawing ---

// busyButton is a push (or primary) button that shows a spinner in front
// of its label while busy (after busyDelay), keeping its size.
func busyButton(c *ui.Context, label string, primary, busy, disabled bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = pushFace(c, ui.Button(c, ""))
	}
	b.Label(label).Disabled(disabled)
	spin := showBusy(c, b, busy)
	return b.Busy(spin).Children(func() {
		if spin {
			sp := spinnerIcon(c)
			if primary {
				sp.TextColor(c.Theme().AccentText)
			}
		}
		ui.Text(c, label).SingleLine()
	})
}

// helperSection is the 后台助手 card of 设置.
func (a *app) helperSection(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	v := &a.hm
	h := s.Helper
	// The card's actions sit in its heading (R-unslop-37): 更新 when the
	// helper is older than the app, then 操作 (重启, 查看日志).
	var trailing func()
	if h.State != status.HelperNotInstalled {
		trailing = func() {
			on := a.helperOpsEnabled(s)
			if a.canUpdateHelper() {
				if disabledPrimaryFace(c, busyButton(c.Key("hm-update"), T.HelperUpdate, true, v.running == T.HelperUpdate, !on)).Clicked() {
					a.updateHelper()
				}
			}
			restarting := v.running == T.HelperRestart
			if restarting { // 重启 runs from the menu: its progress shows beside it
				spinnerIcon(c).Label(T.HelperRestart)
			}
			pushMenu(c.Key("hm-actions"), T.Actions, func(m *ui.Menu) {
				if m.Item(T.HelperRestart).Disabled(!on || restarting).Chosen() {
					a.restartHelper()
				}
				if m.Item(T.HelperViewLog).Chosen() {
					a.openHelperLog()
				}
			})
		}
	}
	cardTipWith(c, T.HelperSection, T.TipHelper, nil, trailing, func() {
		kvWith(c, T.State, func() {
			m, col := markDown, t.TextMuted
			switch h.State {
			case status.HelperConnected:
				m, col = markOK, t.Success
			case status.HelperNotRunning, status.HelperDenied, status.HelperNotInstalled:
				m, col = markAttention, t.Warning
			}
			stateDot(c, m, col, 8)
			ui.Text(c, h.State.Text()).SingleLine()
		})
		if h.State == status.HelperNotInstalled {
			kvWith(c, "", func() {
				if pushButton(c.Key("hm-install"), T.OnboardInstall).Disabled(!s.WritesAllowed).Clicked() {
					a.showPage(pageInstall)
				}
			})
			return
		}
		if st := h.Status; st != nil && st.HelperVersion != "" {
			a.helperVersionRow(c, s)
		}
		if v.errMsg != "" {
			errorNote(c, v.errMsg)
		}
	})
	// The ways out (撤销接管, 卸载, 完全移除) are on their own page.
	a.leaveSection(c, s)
}

// helperVersionRow: the helper's version, and the app's when they differ;
// 需要更新后台助手 is a tag on the same row.
func (a *app) helperVersionRow(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	hv := s.Helper.Status.HelperVersion
	kvWith(c, T.Version, func() {
		switch helperMatch(s) {
		case version.MatchHelperOlder:
			// The helper's version → the app's, with the attention ring;
			// 更新 is the card's action (R-unslop-37).
			stateDot(c, markAttention, t.Warning, 8)
			monoText(c, hv+" → "+version.Version).SingleLine().Selectable().Tooltip(T.HelperNeedsUpdate)
			ui.Text(c, T.HelperNeedsUpdate).FontSize(plat.calloutSize).TextColor(t.TextMuted).SingleLine().Shrink(1)
		case version.MatchHelperNewer:
			num(ui.Text(c, fmt.Sprintf(T.HelperVersionVsApp, hv, version.Version)).SingleLine().Selectable())
			tag(c, T.HelperNewerThanApp)
		default:
			monoText(c, hv).SingleLine().Selectable()
		}
	})
}

// helperUpdateRow is the version state where it shows outside 设置 (the
// overview's helper card): 需要更新后台助手 with 更新. Nothing when the
// versions match.
func (a *app) helperUpdateRow(c *ui.Context, s status.Snapshot) bool {
	if !helperOutdated(s) {
		return false
	}
	T := i18n.T
	t := c.Theme()
	kvWith(c, T.Version, func() {
		stateDot(c, markAttention, t.Warning, 8)
		ui.Text(c, T.HelperNeedsUpdate).SingleLine().Shrink(1)
		ui.Spacer(c)
		a.helperUpdateButton(c, s, "ov-update")
	})
	return true
}

// helperUpdateButton is 更新 wherever the version state shows; it opens
// 设置 when the bundled helper cannot do it (a development run, an
// older bundle), where the section says what there is.
func (a *app) helperUpdateButton(c *ui.Context, s status.Snapshot, key string) {
	T := i18n.T
	if busyButton(c.Key(key), T.HelperUpdate, false, a.hm.running == T.HelperUpdate, !a.helperOpsEnabled(s)).Clicked() {
		if a.canUpdateHelper() {
			a.updateHelper()
		} else {
			a.showPage(pageSettings)
		}
	}
}

// loginRow is 设置 → 通用 → 登录时打开 (a packaged app only).
func (a *app) loginRow(c *ui.Context) {
	T := i18n.T
	ok := appBundled()
	on := ok && a.openAtLogin()
	settingsRow(c, T.OpenAtLogin, T.TipOpenAtLogin, func() { a.openAboutFAQ(T.FAQQuitQ) }, func() {
		if tg := needsBundleTag(ok); tg != "" {
			tag(c, tg).Tooltip(T.OpenAtLoginUnsupportedTip)
		}
		if ui.Switch(c.Key("login"), &on).Label(T.OpenAtLogin).Disabled(!ok).Changed() {
			a.setOpenAtLogin(on)
		}
	})
	if a.hm.loginErr != "" {
		errorNote(c, a.hm.loginErr)
	}
}

// openAtLogin reads the login item state (a system call: only for a
// bundled app, and not in shots).
func (a *app) openAtLogin() bool {
	if a.shotMode {
		return false
	}
	return mygo.App.OpenAtLogin()
}
