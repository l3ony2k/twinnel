package main

// 设置 → 本地 Agent → 命令行工具 on Windows: there are no links there.
// The CLI lives in the install's bin\ folder, which install.ps1 puts on
// the user's PATH; this row shows whether it is there and adds or removes
// it (HKCU, no administrator approval), then tells Explorer
// (platform/windows/userpath). Only terminals opened afterwards find
// twinnel, which the toast says.

import (
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/platform/windows/userpath"
)

// userPath is the registry behind the row (a var: tests fake it).
var userPath = struct {
	Has    func(dir string) (bool, error)
	Add    func(dir string) (bool, error)
	Remove func(dir string) (bool, error)
}{userpath.Has, userpath.AddDir, userpath.RemoveDir}

// cliBinDir is the folder holding twinnel.exe ("" when there is none).
func cliBinDir() string {
	p, err := cliPath()
	if err != nil {
		return ""
	}
	return filepath.Dir(p)
}

// refreshCLIPath reads whether the CLI's folder is on the user's PATH
// (once per change; the row does not read the registry every frame).
func (a *app) refreshCLIPath() {
	v := &a.agent
	v.pathRead = true
	if a.shotMode {
		return
	}
	v.binDir = cliBinDir()
	v.onPath = false
	if v.binDir != "" {
		v.onPath, _ = userPath.Has(v.binDir)
	}
}

func (a *app) agentCLIPathRow(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.agent
	if !v.pathRead {
		a.refreshCLIPath()
	}
	if v.binDir == "" {
		return // a development build without the CLI beside it
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		settingsLabel(c, T.AgentCLI, T.TipAgentCLI)
		if v.onPath {
			monoText(c, v.binDir).SingleLine().Selectable().Shrink(1)
			ui.Spacer(c)
			if busyButton(c.Key("ag-cli-rm"), T.AgentCLIRemove, false, v.cliBusy, !s.WritesAllowed).Clicked() {
				a.runCLIPath(false)
			}
			return
		}
		ui.Text(c, T.AgentCLINone).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if busyButton(c.Key("ag-cli-add"), T.AgentCLIInstall, false, v.cliBusy, !s.WritesAllowed).Clicked() {
			a.runCLIPath(true)
		}
	})
	if v.cliErr != "" {
		failedRow(c.Key("ag-cli-failed"), T.AgentCLIFailed, v.cliErr, nil, false)
	}
}

// runCLIPath adds the CLI's folder to the user's PATH, or removes it.
func (a *app) runCLIPath(add bool) {
	v := &a.agent
	if v.cliBusy || !a.cfg.AllowWrites || a.shotMode || v.binDir == "" {
		return
	}
	dir := v.binDir
	v.cliBusy, v.cliErr = true, ""
	go func() {
		var err error
		if add {
			_, err = userPath.Add(dir)
		} else {
			_, err = userPath.Remove(dir)
		}
		mygo.RunOnMain(func() {
			v.cliBusy = false
			a.refreshCLIPath()
			switch {
			case err != nil:
				v.cliErr = err.Error()
			case add:
				a.toast = i18n.F(i18n.T.AgentCLIOnPath, dir)
			default:
				a.toast = i18n.T.AgentCLIRemoved
			}
			a.invalidate()
		})
	}()
}
