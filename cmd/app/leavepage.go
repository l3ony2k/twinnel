package main

import (
	"context"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/status"
)

// The 停止使用 Twinnel page (设置 → 停止使用 Twinnel…): what Twinnel installed on
// this Mac (helper.inventory plus the app's own pieces), then the ways
// out, each with its list of what happens before anything runs:
// 撤销接管 (takeover.undo), 卸载后台助手 and 完全移除 Twinnel (the 后台助手
// section's flows, helpermgmt.go), and 导出配置.

const pageLeave = "leave"

// leaveView is the page's state. Main thread.
type leaveView struct {
	inv      *rpc.Inventory // nil: not read, or no helper
	app      []rpc.InventoryItem
	loading  bool
	loaded   bool
	err      string
	undoBusy bool
	undo     *rpc.TakeoverUndoResult
	undoErr  string
}

// App-side inventory kinds.
const (
	invApp         = "app"
	invAppPrefs    = "app-prefs"
	invAppBackups  = "app-backups"
	invLaunchAgent = "launch-agent"
	invLoginItem   = "login-item"
)

// twinnelAgentPrefix names Twinnel's user LaunchAgents (none ships today; a
// leftover from a development setup is listed and removed all the same).
const twinnelAgentPrefix = "com.l3ony2k.twinnel"

// appBackupsDir is where scripts/install-local.sh keeps old copies.
func appBackupsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "twinnel", "backups")
}

// launchAgents are Twinnel's plists in ~/Library/LaunchAgents.
func launchAgents() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	m, _ := filepath.Glob(filepath.Join(home, "Library", "LaunchAgents", twinnelAgentPrefix+"*.plist"))
	return m
}

// appInventory lists the app's own pieces that exist. Off the main
// thread (it walks dirs); bundle and loginItem come from the caller.
func appInventory(bundle, prefs string, loginItem bool) []rpc.InventoryItem {
	var out []rpc.InventoryItem
	if bundle != "" {
		out = append(out, rpc.InventoryItem{Kind: invApp, Path: bundle, Size: treeSize(bundle)})
	}
	if fi, err := os.Stat(prefs); err == nil {
		out = append(out, rpc.InventoryItem{Kind: invAppPrefs, Path: prefs, Size: fi.Size()})
	}
	if d := appBackupsDir(); d != "" {
		if entries, err := os.ReadDir(d); err == nil {
			out = append(out, rpc.InventoryItem{Kind: invAppBackups, Path: d, Size: treeSize(d), Count: len(entries)})
		}
	}
	for _, p := range launchAgents() {
		out = append(out, rpc.InventoryItem{Kind: invLaunchAgent, Path: p, Label: strings.TrimSuffix(filepath.Base(p), ".plist")})
	}
	if loginItem {
		out = append(out, rpc.InventoryItem{Kind: invLoginItem})
	}
	return out
}

// helperFilesOnDisk is what an unreachable helper still left on disk.
func helperFilesOnDisk() []rpc.InventoryItem {
	var out []rpc.InventoryItem
	kinds := []string{rpc.InvHelperBinary, rpc.InvHelperService}
	for i, p := range helperInstalledPaths { // binary, plist; then the old name's
		if fi, err := os.Stat(p); err == nil {
			out = append(out, rpc.InventoryItem{Kind: kinds[i%len(kinds)], Path: p, Size: fi.Size()})
		}
	}
	return out
}

func treeSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// loadInventory reads helper.inventory (when connected) and the app's
// pieces. Main thread.
func (a *app) loadInventory(s status.Snapshot) {
	v := &a.hd.leave
	if v.loading {
		return
	}
	v.loading = true
	hc, connected := a.hc, s.Helper.Connected()
	bundle, prefs := appBundlePath(), a.cfgPath
	login := appBundled() && a.openAtLogin()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var inv *rpc.Inventory
		var err error
		if connected && hc != nil {
			var i rpc.Inventory
			if err = hc.Call(ctx, rpc.MethodHelperInventory, nil, &i); err == nil {
				inv = &i
			}
		}
		appItems := appInventory(bundle, prefs, login)
		mygo.RunOnMain(func() {
			v.loading, v.loaded = false, true
			v.inv, v.app, v.err = inv, appItems, ""
			if err != nil && !isTooOld(err) {
				v.err = helperError(err)
			}
			a.invalidate()
		})
	}()
}

// invLabel words an inventory kind.
func invLabel(k string) string {
	T := i18n.T
	switch k {
	case rpc.InvHelperBinary:
		return T.InvHelperBinary
	case rpc.InvHelperService:
		return T.InvHelperService
	case rpc.InvHelperSocket:
		return T.InvHelperSocket
	case rpc.InvStateDir:
		return T.InvStateDir
	case rpc.InvSubscriptions:
		return T.InvSubscriptions
	case rpc.InvLayers:
		return T.InvLayers
	case rpc.InvHistory:
		return T.InvHistory
	case rpc.InvTakeoverBackups:
		return T.InvTakeoverBackups
	case rpc.InvKernels:
		return T.InvKernels
	case rpc.InvLogs:
		return T.InvLogs
	case rpc.InvSecrets:
		return T.InvSecrets
	case rpc.InvMihomoService:
		return T.InvTwinnelMihomo
	case rpc.InvAdoptedService:
		return T.InvAdoptedMihomo
	case invApp:
		return T.InvApp
	case invAppPrefs:
		return T.InvAppPrefs
	case invAppBackups:
		return T.InvAppBackups
	case invLaunchAgent:
		return T.InvLaunchAgent
	case invLoginItem:
		return T.InvLoginItem
	}
	return k
}

// invNested: the state dir's parts, drawn indented under it.
// secretNames words the keychain items: 控制接口密钥、订阅「X」的地址
// (walkthrough 2, N14). A key Twinnel does not know shows as it is.
func secretNames(names []string) string {
	T := i18n.T
	var out []string
	for _, n := range names {
		switch {
		case n == "controller-secret":
			out = append(out, T.SecretController)
		case strings.HasPrefix(n, "subscription-url/"):
			id := strings.TrimPrefix(n, "subscription-url/")
			if id == adoptedSubID {
				out = append(out, T.SecretSubAdopted)
			} else {
				out = append(out, i18n.F(T.SecretSubURL, id))
			}
		default:
			out = append(out, n)
		}
	}
	return strings.Join(out, T.ListSep)
}

func invNested(k string) bool {
	switch k {
	case rpc.InvSubscriptions, rpc.InvLayers, rpc.InvHistory, rpc.InvTakeoverBackups, rpc.InvKernels, rpc.InvLogs:
		return true
	}
	return false
}

// leaveSection is 设置's entry to the page: one row that opens it.
func (a *app) leaveSection(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	t := c.Theme()
	listBox(c, t).Children(func() {
		row := ui.Row(c.Key("leave-entry")).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).PaddingY(listRowPadY).
			Transition(colorMotion).Focusable().Role(ui.RoleButton).Label(T.LeavePage)
		rowFill(c, rowBleed(row), false)
		row.Children(func() {
			ui.Text(c, T.LeavePage).Grow(1)
			ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
		})
		if row.Clicked() {
			a.showPage(pageLeave)
		}
	})
}

// leavePage draws 停止使用 Twinnel.
func (a *app) leavePage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.hd.leave
	a.subPageHeader(c, T.LeavePage)
	if !v.loaded && !v.loading {
		a.loadInventory(s)
	}
	if a.section("inventory") {
		a.inventoryCard(c, s)
	}
	if a.section("actions") {
		a.undoCard(c, s)
		a.uninstallCard(c, s)
		a.exportCard(c, s)
		a.removeAllCard(c, s)
	}
}

func (a *app) inventoryCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.leave
	trailing := func() {
		if iconButton(c.Key("inv-refresh"), iconRefresh, T.Refresh, false).Disabled(v.loading).Clicked() {
			v.loaded = false
			a.loadInventory(s)
		}
	}
	var items []rpc.InventoryItem
	if v.inv != nil {
		items = append(items, v.inv.Items...)
	} else if v.loaded {
		items = append(items, helperFilesOnDisk()...)
	}
	items = append(items, v.app...)
	listCard(c, listHead(c, nil, T.LeaveInstalled, trailing), func() {
		if !v.loaded {
			listRow(c).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					spinner(c)
					small(c, T.LeaveInventoryLoading)
				})
			})
			return
		}
		if v.err != "" {
			listRow(c).Children(func() { errorNote(c, v.err) })
		}
		for i, it := range items {
			listRow(c.Key(fmt.Sprint("inv", i))).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight - 8).Children(func() {
					label := invLabel(it.Kind)
					indent := float32(0)
					if invNested(it.Kind) {
						indent = 16
					}
					// The kv label column: it widens for a long label and
					// wraps one wider than labelMax, never cuts it.
					kvLabel(c, label, "", nil, -indent).Margin(0, 0, 0, indent).Tooltip(label)
					detail := it.Path
					if it.Kind == rpc.InvSecrets {
						detail = secretNames(it.Names)
					} else if it.Label != "" && it.Kind != invLaunchAgent {
						detail = it.Label
					}
					mono(ui.Text(c, tildePath(detail))).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().TruncateMiddle().Grow(1).Shrink(1).MinWidth(0).Selectable().Tooltip(it.Path)
					switch {
					case it.Count > 0 && it.Size > 0:
						num(meta(c, i18n.F(T.InvCount, it.Count)+" · "+status.Bytes(it.Size)))
					case it.Count > 0:
						num(meta(c, i18n.F(T.InvCount, it.Count)))
					case it.Size > 0:
						num(meta(c, status.Bytes(it.Size)))
					}
					// One muted icon per row, the label in its tooltip: a
					// column of accent links would outshout the actions
					// below (kubai R-24, unslop R-10).
					if it.Path != "" && !strings.HasSuffix(it.Path, ".sock") {
						p := it.Path
						if toolIcon(c.Key("finder"), iconReveal, T.ShowInFinder, false).Clicked() {
							mygo.Shell.ShowItemInFolder(p)
						}
					} else {
						ui.Box(c).Size(24, 24).Shrink(0) // keeps the sizes' column
					}
				})
			})
		}
	})
}

// --- 撤销接管 ---

func (a *app) undoCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.leave
	adopted := v.inv != nil && v.inv.Adopted != ""
	hardened := v.inv != nil && v.inv.Hardened
	cardTip(c, T.LeaveUndo, T.TipLeaveUndo, a.openAboutTakeover, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case hardened:
				ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText)
				small(c, T.LeaveUndoHardened).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
			case adopted:
				ui.Icon(c, iconTakeover).FontSize(iconInline).TextColor(t.TextMuted)
				ui.Text(c, T.PlanState(rpc.PlanAdopted)).Grow(1)
			default:
				ui.Text(c, T.LeaveUndoNotAdopted).TextColor(t.TextMuted).Grow(1)
			}
			can := adopted && !hardened && a.helperWrites(s) && !v.undoBusy
			if busyButton(c.Key("undo"), T.LeaveUndoRun+"…", false, v.undoBusy, !can).Clicked() {
				a.askUndo()
			}
		})
		if r := v.undo; r != nil {
			a.undoResult(c, r)
		}
		if v.undoErr != "" {
			errorNote(c, v.undoErr)
		}
	})
}

// undoStepText words one step of takeover.undo.
func undoStepText(st rpc.TakeoverUndoStep) string {
	T := i18n.T
	f, ok := T.UndoSteps[st.Code]
	if !ok {
		return st.Code
	}
	switch {
	case strings.Contains(f, "%s"):
		return i18n.F(f, st.Path)
	case strings.Contains(f, "%d"):
		return i18n.F(f, st.Count)
	}
	return f
}

// askUndo: the dry run's steps in the dialog, then the run.
func (a *app) askUndo() {
	T := i18n.T
	v := &a.hd.leave
	if v.undoBusy {
		return
	}
	v.undoBusy, v.undoErr = true, ""
	hc, parent := a.hc, a.win
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var dry rpc.TakeoverUndoResult
		err := hc.Call(ctx, rpc.MethodTakeoverUndo, rpc.TakeoverUndoParams{DryRun: true}, &dry)
		if err != nil {
			mygo.RunOnMain(func() { v.undoBusy, v.undoErr = false, helperError(err); a.invalidate() })
			return
		}
		var lines []string
		for _, st := range dry.Steps {
			lines = append(lines, "• "+undoStepText(st))
		}
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: parent, Type: mygo.MessageWarning,
			Message: T.LeaveUndoTitle, Detail: strings.Join(lines, "\n"),
			Buttons:            []string{T.Cancel, T.LeaveUndoRun},
			DestructiveButtons: []int{1}, CancelButton: 0, DefaultButton: 0,
		})
		if err != nil || res.Button != 1 {
			mygo.RunOnMain(func() { v.undoBusy = false; a.invalidate() })
			return
		}
		mygo.RunOnMain(func() {
			v.undoBusy = false
			a.runUndo()
		})
	}()
}

func (a *app) runUndo() {
	T := i18n.T
	v := &a.hd.leave
	var r rpc.TakeoverUndoResult
	a.helperOp(T.LeaveUndoRun, 3*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodTakeoverUndo, rpc.TakeoverUndoParams{}, &r)
	}, func(err error) {
		v.undo = nil
		if err == nil {
			v.undo = &r
		}
		v.loaded = false // the inventory changed
		a.hd.take.adopt = nil
		a.loadTakeover()
		a.refreshCredentials()
		a.runHealth()
	})
}

func (a *app) undoResult(c *ui.Context, r *rpc.TakeoverUndoResult) {
	t := c.Theme()
	T := i18n.T
	head, icon, col := T.UndoDone, iconOK, t.Success
	if !r.OK {
		head, icon, col = T.UndoFailed, iconError, t.Danger
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
		ui.Text(c, head).FontWeight(600).Grow(1)
		if r.Error != "" && actionLink(c.Key("undo-copy"), T.TkCopyDetails).Clicked() {
			copyText(c, r.Error)
		}
	})
	for i, st := range r.Steps {
		ui.Row(c.Key(fmt.Sprint("u", i))).Gap(8).AlignItems(ui.Center).Children(func() {
			ic, icol := stepIcon(t, st.Status)
			ui.Icon(c, ic).FontSize(iconInline).TextColor(icol)
			small(c, undoStepText(st)).SingleLine().Shrink(1).Tooltip(st.Detail)
		})
	}
}

// --- 卸载后台助手 and 完全移除 ---

// iconReveal is 在 Finder 中显示 on an inventory row.
var iconReveal = svgIcon("folder-search")

func (a *app) uninstallCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	hm := &a.hm
	cardTip(c, T.LeaveUninstall, T.TipLeaveUninstall, nil, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.LeaveUninstallWhat).Grow(1).Shrink(1) // keeps running: in the ⓘ (R-unslop-29)
			installed := s.Helper.State != status.HelperNotInstalled
			if busyButton(c.Key("uninstall"), T.LeaveUninstallRun, false, hm.running == T.HelperUninstall, !installed || !a.helperOpsEnabled(s)).Clicked() {
				a.askUninstall()
			}
		})
		if hm.errMsg != "" {
			errorNote(c, hm.errMsg)
		}
	})
}

func (a *app) removeAllCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	hm := &a.hm
	cardTip(c, T.LeaveRemoveAll, T.TipLeaveRemoveAll, nil, func() {
		// "mihomo 和 Tailscale 继续运行" is said once, by 卸载后台助手
		// above (unslop R-29).
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.LeaveRemoveAllWhat).Grow(1).Shrink(1) // keeps running: in the ⓘ (R-unslop-29)
			b := busyButton(c.Key("remove-all"), T.LeaveRemoveAllRun, false, hm.running == T.HelperRemoveAll, !a.helperOpsEnabled(s))
			b.TextColor(t.Danger)
			if b.Clicked() {
				a.askRemoveAll()
			}
		})
	})
}

// --- 导出配置 ---

func (a *app) exportCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	n := 0
	for _, l := range a.hd.layers {
		if !l.Managed {
			n++
		}
	}
	if inv := a.hd.leave.inv; inv != nil { // the inventory's count when it is read
		for _, it := range inv.Items {
			if it.Kind == rpc.InvLayers {
				n = it.Count
			}
		}
	}
	cardTip(c, T.LeaveExport, T.TipLeaveExport, nil, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.F(T.LeaveExportWhat, n)).Grow(1).Shrink(1)
			if pushButton(c.Key("export"), T.LeaveExportRun).Disabled(!s.Helper.Connected() || a.hd.busy != "").Clicked() {
				a.exportConfig()
			}
		})
	})
}

// exportConfig saves the running config (without the controller secret)
// and every override as a zip the person picks the place of.
func (a *app) exportConfig() {
	T := i18n.T
	parent := a.win
	go func() {
		to, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: parent, Title: i18n.Cur().LeaveExport,
			DefaultPath: setup.ExportFileName(time.Now()),
			Filters:     []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
		})
		if err != nil || to == "" {
			return
		}
		mygo.RunOnMain(func() {
			a.helperOp(T.LeaveExport, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
				return writeExport(ctx, hc, to)
			}, func(err error) {
				if err == nil {
					a.toast = i18n.F(T.LeaveExportDone, filepath.Base(to))
				}
			})
		})
	}()
}

// exportCaller is the part of the helper connection the export uses.
type exportCaller = setup.Caller

// writeExport writes the export zip to to (core/setup.ExportConfig: the
// same file `twinnel export` writes).
func writeExport(ctx context.Context, hc exportCaller, to string) error {
	x, err := setup.ExportConfig(ctx, hc, rpc.ConfigExportParams{})
	if err != nil {
		return err
	}
	return setup.SaveExport(to, x.Zip)
}

// --- the app's own pieces (完全移除) ---

// bootoutAgent unloads a user LaunchAgent before its plist goes; a
// variable for tests.
var bootoutAgent = func(plist string) error {
	return exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), plist).Run()
}

// removeAppExtras removes the install backups and Twinnel's LaunchAgents.
// Only a backups dir named …/twinnel/backups is removed.
func removeAppExtras() {
	if d := appBackupsDir(); d != "" && filepath.Base(d) == "backups" && filepath.Base(filepath.Dir(d)) == "twinnel" {
		os.RemoveAll(d)
	}
	for _, p := range launchAgents() {
		bootoutAgent(p)
		os.Remove(p)
	}
	removeCLILinks()       // 设置 → 本地 Agent → 命令行工具
	removeTailscaleLinks() // and the tailscale command a setup linked
	if d := legacyPrefsDir(); d != "" {
		removeLegacyPrefs(d) // the app from before the rename
	}
}

// appExtraItems words the app-side extras that exist, for the 完全移除
// dialog.
func appExtraItems() []string {
	T := i18n.T
	var out []string
	if d := appBackupsDir(); d != "" {
		if _, err := os.Stat(d); err == nil || legacyLeftovers() {
			out = append(out, T.ItemAppBackups)
		}
	} else if legacyLeftovers() {
		out = append(out, T.ItemAppBackups)
	}
	if len(launchAgents()) > 0 {
		out = append(out, T.ItemLaunchAgent)
	}
	for _, d := range cliTargets() {
		if isTwinnelLink(filepath.Join(d, "twinnel")) || isLegacyLink(filepath.Join(d, legacy.CLI)) {
			out = append(out, T.AgentCLI)
			break
		}
	}
	return out
}

// tildePath shows a path under the home dir as ~/….
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
