package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// 设置 → 本地 Agent: the MCP switches (kept in the helper's settings, so
// they hold for every client), the command line tool (a symlink to the
// bundled CLI in a folder the person picks, removable, removed by 完全移除),
// copyable client snippets (Twinnel never edits another app's config), and
// the agents' recent actions (agent.activity). docs/agents.md.

// agentView is the section's state. Main thread.
type agentView struct {
	activity  []rpc.AgentAction
	actSeq    uint64 // the event seq the activity was read at
	actLoaded bool
	actBusy   bool
	actErr    string

	target int // the command line tool's folder (cliTargets)
	client int // the snippet shown

	cliBusy bool
	cliErr  string

	// Windows (clipath.go): whether the CLI's folder is on the user's PATH.
	pathRead bool
	onPath   bool
	binDir   string

	shotLinks []string // shot mode: the installed links (no file system reads)
}

// agentShownActions is how many actions the section lists.
const agentShownActions = 6

// cliTargets are the folders the command line tool can go in (a var:
// tests keep it inside a temp dir).
var cliTargets = func() []string {
	t := []string{"/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		t = append(t, filepath.Join(home, ".local", "bin"))
	}
	return t
}

// isTwinnelLink reports whether path is a symlink Twinnel made: it points to a
// file named Twinnel inside a Twinnel app bundle or next to a twinnel-helper.
func isTwinnelLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dst, err := os.Readlink(path)
	if err != nil || filepath.Base(dst) != "twinnel" {
		return false
	}
	if strings.Contains(dst, ".app/Contents/Resources/bin/") {
		return true
	}
	if own, err := cliPath(); err == nil && own == dst {
		return true
	}
	return false
}

// cliLinks lists the installed links to the CLI.
func (a *app) cliLinks() []string {
	if a.shotMode {
		return a.agent.shotLinks
	}
	var out []string
	for _, d := range cliTargets() {
		if p := filepath.Join(d, "twinnel"); isTwinnelLink(p) {
			out = append(out, p)
		}
	}
	return out
}

// installCLI links the bundled CLI into dir.
func installCLI(dir string) (string, error) {
	src, err := cliPath()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "twinnel")
	if _, err := os.Lstat(dst); err == nil {
		if !isTwinnelLink(dst) {
			return "", errors.New(i18n.F(i18n.T.AgentCLIForeign, tildePath(dst)))
		}
		if err := os.Remove(dst); err != nil {
			return "", err
		}
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	return dst, os.Symlink(src, dst)
}

// removeCLILinks removes every link to the CLI Twinnel made (停止使用 Twinnel).
func removeCLILinks() {
	for _, d := range cliTargets() {
		if p := filepath.Join(d, "twinnel"); isTwinnelLink(p) {
			os.Remove(p)
		}
		if p := filepath.Join(d, legacy.CLI); isLegacyLink(p) {
			os.Remove(p) // made by the app before the rename
		}
	}
}

// agentCommand is the command a client runs: the installed link, else
// the bundled CLI.
func (a *app) agentCommand() string {
	if l := a.cliLinks(); len(l) > 0 {
		return l[0]
	}
	if a.shotMode {
		return "/usr/local/bin/twinnel"
	}
	if p, err := cliPath(); err == nil {
		return p
	}
	return "twinnel"
}

// agentSnippet is the setup text for client i (Claude Code, Codex,
// Cursor, any other).
func agentSnippet(i int, cmd string) string {
	q, _ := json.Marshal(cmd)
	switch i {
	case 0:
		return "claude mcp add twinnel -- " + cmdQuote(cmd) + " mcp"
	case 1:
		return "codex mcp add twinnel -- " + cmdQuote(cmd) + " mcp"
	}
	// Cursor (~/.cursor/mcp.json) and most others take this JSON.
	return `{
  "mcpServers": {
    "twinnel": { "command": ` + string(q) + `, "args": ["mcp"] }
  }
}`
}

// cmdQuote quotes a program path for the platform's shell: POSIX quoting
// on macOS and Linux; on Windows double quotes, which PowerShell and
// cmd.exe both take (a Windows path has no double quote in it).
func cmdQuote(s string) string { return cmdQuoteFor(runtime.GOOS, s) }

func cmdQuoteFor(goos, s string) string {
	if goos != "windows" {
		return shellQuote(s)
	}
	if s != "" && !strings.ContainsAny(s, " &()[]{}^=;!'+,`~$@#%") {
		return s
	}
	return `"` + s + `"`
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " '\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// loadAgentActivity reads the agents' recent actions.
func (a *app) loadAgentActivity(seq uint64) {
	v := &a.agent
	if v.actBusy || a.hc == nil {
		return
	}
	v.actBusy, v.actSeq = true, seq
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var list []rpc.AgentAction
		err := hc.Call(ctx, rpc.MethodAgentActivity, rpc.AgentActivityParams{Limit: 50}, &list)
		mygo.RunOnMain(func() {
			v.actBusy, v.actLoaded = false, true
			v.actErr = ""
			if err != nil {
				v.actErr = helperError(err)
			} else {
				v.activity = list
			}
			a.invalidate()
		})
	}()
}

// setAgentSwitch changes one switch in the helper's settings.
func (a *app) setAgentSwitch(u rpc.AgentSettingsUpdate) {
	a.helperOp(i18n.T.AgentSection, 10*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{Agent: &u}, nil)
	}, func(err error) {
		if err != nil {
			a.toastErr = i18n.T.ErrorText(helperError(err))
		}
	})
}

func (a *app) runCLIInstall(dir string, remove []string) {
	v := &a.agent
	if v.cliBusy || !a.cfg.AllowWrites || a.shotMode {
		return
	}
	ts := tailscaleTool(managedTailscale(a.mon.Snapshot()))
	v.cliBusy, v.cliErr = true, ""
	go func() {
		var (
			dst string
			err error
		)
		if remove != nil {
			for _, p := range remove {
				if isTwinnelLink(p) {
					err = errors.Join(err, os.Remove(p))
				}
			}
			removeTailscaleLinks()
		} else {
			dst, err = installCLI(dir)
			if err == nil && fileExists(ts.Target) {
				// The tailscale CLI a setup installed, beside it (never
				// over another tailscale; a failure here is not the CLI's).
				_, _ = ts.Link([]string{dir})
			}
		}
		mygo.RunOnMain(func() {
			v.cliBusy = false
			switch {
			case err != nil:
				v.cliErr = err.Error()
				if errors.Is(err, fs.ErrPermission) && remove == nil && v.target == 0 && len(cliTargets()) > 1 {
					v.target = 1 // a folder of the person's own: offered next
				}
			case remove != nil:
				a.toast = i18n.T.AgentCLIRemoved
			default:
				a.toast = i18n.F(i18n.T.AgentCLIInstalled, tildePath(dst))
			}
			a.invalidate()
		})
	}()
}

// agentSection draws the card.
func (a *app) agentSection(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.agent
	connected := s.Helper.Connected() && a.hd.settings != nil
	var ag rpc.AgentSettings
	if connected {
		ag = a.hd.settings.Agent
		if seq := a.hd.lastEvent.Seq; !v.actLoaded || seq != v.actSeq {
			a.loadAgentActivity(seq)
		}
	}
	canSet := connected && a.helperOpsEnabled(s)
	cardTip(c, T.AgentSection, T.TipAgent, nil, func() {
		sw := func(key, label, tip string, on bool, disabled bool, why string, set func(bool) rpc.AgentSettingsUpdate, indent bool) {
			row := ui.Row(c).Gap(12).AlignItems(ui.Center)
			if indent {
				row.PaddingStart(20)
			}
			row.Children(func() {
				switchLabel(c, label, tip, nil)
				val := on
				e := disabledWhy(c, disabled, why, func() ui.Element { return ui.Switch(c.Key(key), &val).Label(label) })
				if e.Changed() {
					a.setAgentSwitch(set(val))
				}
			})
		}
		why := ""
		if !connected {
			why = T.AgentNeedsHelper
		}
		sw("ag-mcp", T.AgentMCP, T.TipAgentMCP, ag.MCP, !canSet, why,
			func(b bool) rpc.AgentSettingsUpdate { return rpc.AgentSettingsUpdate{MCP: &b} }, false)
		sw("ag-operate", T.AgentOperate, T.TipAgentOperate, ag.Operate, !canSet || !ag.MCP, why,
			func(b bool) rpc.AgentSettingsUpdate { return rpc.AgentSettingsUpdate{Operate: &b} }, true)
		sw("ag-configure", T.AgentConfigure, T.TipAgentConfigure, ag.Configure, !canSet || !ag.MCP, why,
			func(b bool) rpc.AgentSettingsUpdate { return rpc.AgentSettingsUpdate{Configure: &b} }, true)

		a.agentCLIRow(c, s)
		a.agentConnectRow(c)
		if connected {
			a.agentActivityRows(c)
		}
	})
}

func (a *app) agentCLIRow(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	v := &a.agent
	if !plat.cliLink { // Windows: bin\ on the user's PATH instead of a link
		a.agentCLIPathRow(c, s)
		return
	}
	links := a.cliLinks()
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		settingsLabel(c, T.AgentCLI, T.TipAgentCLI)
		if len(links) > 0 {
			for _, l := range links {
				monoText(c, tildePath(l)).SingleLine().Selectable().Shrink(1)
			}
			ui.Spacer(c)
			if busyButton(c.Key("ag-cli-rm"), T.AgentCLIRemove, false, v.cliBusy, !s.WritesAllowed).Clicked() {
				a.runCLIInstall("", links)
			}
			return
		}
		targets := cliTargets()
		labels := make([]string, len(targets))
		for i, t := range targets {
			labels[i] = tildePath(t)
		}
		if v.target >= len(labels) {
			v.target = 0
		}
		sel := labels[v.target]
		if ui.Select(c.Key("ag-cli-target"), &sel, labels).Label(T.AgentCLI).Changed() {
			for i, l := range labels {
				if l == sel {
					v.target = i
				}
			}
		}
		ui.Spacer(c)
		if busyButton(c.Key("ag-cli-add"), T.AgentCLIInstall, false, v.cliBusy, !s.WritesAllowed).Clicked() {
			a.runCLIInstall(targets[v.target], nil)
		}
	})
	if v.cliErr != "" {
		failedRow(c.Key("ag-cli-failed"), T.AgentCLIFailed, v.cliErr, nil, false)
	}
}

func (a *app) agentConnectRow(c *ui.Context) {
	T := i18n.T
	v := &a.agent
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		settingsLabel(c, T.AgentConnect, "")
		segmented(c.Key("ag-client"), &v.client, "Claude Code", "Codex", "Cursor", T.AgentClientOther)
		ui.Spacer(c)
		snip := agentSnippet(v.client, a.agentCommand())
		if pushButton(c.Key("ag-copy"), T.AgentCopy).Clicked() {
			copyText(c, snip)
		}
	})
	ui.Column(c).PaddingStart(labelCol() + 36).Children(func() {
		codeBlock(c, agentSnippet(v.client, a.agentCommand()))
	})
}

func (a *app) agentActivityRows(c *ui.Context) {
	T := i18n.T
	t := c.Theme()
	v := &a.agent
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		settingsLabel(c, T.AgentActivity, "").MarginY(2)
		ui.Column(c).Gap(6).Grow(1).Shrink(1).Children(func() {
			switch {
			case v.actErr != "":
				failedRow(c.Key("ag-act-failed"), T.AgentLoadFailed, v.actErr, func() { a.loadAgentActivity(a.hd.lastEvent.Seq) }, v.actBusy)
				return
			case v.actLoaded && len(v.activity) == 0:
				ui.Text(c, T.AgentActivityNone).TextColor(t.TextMuted)
				return
			}
			for i, act := range v.activity {
				if i == agentShownActions {
					break
				}
				ui.Row(c.Key(fmt.Sprint("ag-act-", i))).Gap(8).AlignItems(ui.Center).Children(func() {
					num(mono(ui.Text(c, act.Time.Local().Format("01-02 15:04")))).FontSize(plat.smallSize).TextColor(t.TextMuted).Shrink(0)
					if !act.OK {
						ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(t.Warning).Shrink(0).Label(T.ErrorPrefix)
					}
					ui.Text(c, agentActionText(act)).SingleLine().Shrink(1).Tooltip(strings.TrimSpace(act.Args + "\n" + act.Error))
				})
			}
			if n := len(v.activity) - agentShownActions; n > 0 {
				l := ui.Text(c.Key("ag-act-more"), i18n.F(T.AgentActivityMore, n)).FontSize(plat.calloutSize).TextColor(t.Accent).
					Focusable().Role(ui.RoleButton)
				if l.Clicked() {
					a.showTwinnelLog()
				}
			}
		})
	})
}

// agentActionText is one action in words: who and what.
func agentActionText(act rpc.AgentAction) string {
	what := act.Tool
	if act.Args != "" {
		what += " " + act.Args
	}
	f := i18n.T.EvAgent
	if !act.OK {
		f = i18n.T.EvAgentFailed
	}
	return i18n.F(f, act.Client, what)
}
