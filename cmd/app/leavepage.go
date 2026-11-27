package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 停止使用 duet page (设置 → 停止使用 duet…): what duet installed on
// this Mac (helper.inventory plus the app's own pieces), then the ways
// out, each with its list of what happens before anything runs:
// 撤销接管 (takeover.undo), 卸载后台助手 and 完全移除 duet (the 后台助手
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

// duetAgentPrefix names duet's user LaunchAgents (none ships today; a
// leftover from a development setup is listed and removed all the same).
const duetAgentPrefix = "com.l3ony2k.duet"

// appBackupsDir is where scripts/install-local.sh keeps old copies.
func appBackupsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "duet", "backups")
}

// launchAgents are duet's plists in ~/Library/LaunchAgents.
func launchAgents() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	m, _ := filepath.Glob(filepath.Join(home, "Library", "LaunchAgents", duetAgentPrefix+"*.plist"))
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
	for i, p := range helperInstalledPaths {
		if fi, err := os.Stat(p); err == nil {
			out = append(out, rpc.InventoryItem{Kind: kinds[i], Path: p, Size: fi.Size()})
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
		return T.InvDuetMihomo
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
		row := ui.Row(c.Key("leave-entry")).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).PaddingY(8).
			Focusable().Role(ui.RoleButton).Label(T.LeavePage)
		row.Children(func() {
			ui.Text(c, T.LeavePage).Grow(1)
			ui.Icon(c, iconCollapsed).FontSize(iconSmall).TextColor(t.TextMuted)
		})
		if row.Clicked() {
			a.showPage(pageLeave)
		}
	})
}

// leavePage draws 停止使用 duet.
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
					ui.Spinner(c)
					small(c, T.LeaveInventoryLoading)
				})
			})
			return
		}
		if v.err != "" {
			listRow(c).Children(func() { errorNote(c, T.ErrorPrefix+v.err) })
		}
		for i, it := range items {
			listRow(c.Key(fmt.Sprint("inv", i))).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight - 8).Children(func() {
					label := invLabel(it.Kind)
					indent := float32(0)
					if invNested(it.Kind) {
						indent = 16
					}
					ui.Text(c, label).Width(200-indent).Margin(0, 0, 0, indent).Shrink(0).SingleLine().Tooltip(label)
					detail := it.Path
					if it.Kind == rpc.InvSecrets {
						detail = strings.Join(it.Names, T.ListSep)
					} else if it.Label != "" && it.Kind != invLaunchAgent {
						detail = it.Label
					}
					ui.Text(c, tildePath(detail)).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Grow(1).Shrink(1).Selectable().Tooltip(it.Path)
					switch {
					case it.Count > 0 && it.Size > 0:
						num(meta(c, i18n.F(T.InvCount, it.Count)+" · "+status.Bytes(it.Size)))
					case it.Count > 0:
						num(meta(c, i18n.F(T.InvCount, it.Count)))
					case it.Size > 0:
						num(meta(c, status.Bytes(it.Size)))
					}
					if it.Path != "" && !strings.HasSuffix(it.Path, ".sock") {
						p := it.Path
						if actionLink(c.Key("finder"), T.ShowInFinder).Clicked() {
							mygo.Shell.ShowItemInFolder(p)
						}
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
	card(c, T.LeaveUndo, func() {
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
			mygo.RunOnMain(func() { v.undoBusy, v.undoErr = false, T.ErrorPrefix+helperError(err); a.invalidate() })
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

func (a *app) uninstallCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	hm := &a.hm
	card(c, T.LeaveUninstall, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.Success)
			ui.Text(c, T.LeaveKeepsRunning).Grow(1).Shrink(1)
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
	card(c, T.LeaveRemoveAll, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.Success)
			ui.Text(c, T.LeaveKeepsRunning).Grow(1).Shrink(1)
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
	card(c, T.LeaveExport, func() {
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
	layers := append([]rpc.Layer(nil), a.hd.layers...)
	go func() {
		to, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: parent, Title: i18n.Cur().LeaveExport,
			DefaultPath: "duet-config-" + time.Now().Format("20060102") + ".zip",
			Filters:     []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
		})
		if err != nil || to == "" {
			return
		}
		mygo.RunOnMain(func() {
			a.helperOp(T.LeaveExport, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
				return writeExport(ctx, hc, to, layers)
			}, func(err error) {
				if err == nil {
					a.toast = i18n.F(T.LeaveExportDone, filepath.Base(to))
				}
			})
		})
	}()
}

// exportCaller is the part of the helper connection the export uses.
type exportCaller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// writeExport writes config.yaml (the live config as history keeps it,
// without the controller secret) and overrides/<id>.yaml.
func writeExport(ctx context.Context, hc exportCaller, to string, layers []rpc.Layer) error {
	var live rpc.ConfigText
	if err := hc.Call(ctx, rpc.MethodConfigGet, rpc.ConfigGetParams{Source: rpc.ConfigLive}, &live); err != nil {
		return err
	}
	tmp := to + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	put := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(content))
		return err
	}
	err = put("config.yaml", live.Content)
	for _, l := range layers {
		if err != nil || l.Managed {
			continue
		}
		var lc rpc.LayerContent
		if err = hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: l.ID}, &lc); err == nil {
			err = put("overrides/"+l.ID+".yaml", lc.Content)
		}
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

// --- the app's own pieces (完全移除) ---

// bootoutAgent unloads a user LaunchAgent before its plist goes; a
// variable for tests.
var bootoutAgent = func(plist string) error {
	return exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), plist).Run()
}

// removeAppExtras removes the install backups and duet's LaunchAgents.
// Only a backups dir named …/duet/backups is removed.
func removeAppExtras() {
	if d := appBackupsDir(); d != "" && filepath.Base(d) == "backups" && filepath.Base(filepath.Dir(d)) == "duet" {
		os.RemoveAll(d)
	}
	for _, p := range launchAgents() {
		bootoutAgent(p)
		os.Remove(p)
	}
}

// appExtraItems words the app-side extras that exist, for the 完全移除
// dialog.
func appExtraItems() []string {
	T := i18n.T
	var out []string
	if d := appBackupsDir(); d != "" {
		if _, err := os.Stat(d); err == nil {
			out = append(out, T.ItemAppBackups)
		}
	}
	if len(launchAgents()) > 0 {
		out = append(out, T.ItemLaunchAgent)
	}
	return out
}

// tildePath shows a path under the home dir as ~/….
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
