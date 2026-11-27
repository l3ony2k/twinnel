package health

// competitor-autostart: another proxy client registered to start by itself
// (a scheduled task, a Run key, a LaunchAgent, a login item, a systemd
// unit). A takeover that only stopped it loses at the next login, when it
// takes the default route and the controller port back.

import (
	"context"
	"fmt"
	"strings"

	"github.com/l3ony2k/twinnel/core/autostart"
)

// Reasons of competitor-autostart.
const (
	// ReasonAutostartOn: entries the helper can turn off (one click).
	ReasonAutostartOn = "competitor-autostart/enabled"
	// ReasonAutostartManual: entries turned off in the system's settings
	// (a macOS login item) or with a command (a Linux user unit).
	ReasonAutostartManual = "competitor-autostart/manual"
)

// FixDisableAutostart turns the entries in FixHint.Param (comma-separated
// autostart IDs) off through the helper (autostart.change).
const FixDisableAutostart = "disable-autostart"

// OpenLoginItems is System Settings → General → Login Items (macOS).
const OpenLoginItems = "os/login-items"

// CompetitorAutostart is the check.
type CompetitorAutostart struct{}

func (CompetitorAutostart) ID() string            { return "competitor-autostart" }
func (c CompetitorAutostart) Title() string       { return c.title(&zhTexts) }
func (CompetitorAutostart) title(L *texts) string { return autostartText(L).title }
func (CompetitorAutostart) Platforms() []string   { return AllPlatforms }

func (CompetitorAutostart) Run(ctx context.Context, env Env) Result {
	A := autostartText(tx(ctx))
	read := env.Settings().Autostarts
	if read == nil {
		return skipText(A.notHere)
	}
	es, err := read(ctx)
	res := Result{Status: StatusOK, Summary: A.none}
	if err != nil {
		if len(es) == 0 {
			return readSkip(ctx, A.unread, err)
		}
		res.Evidence = append(res.Evidence, fmt.Sprintf(A.partial, accessWords(ctx, err)))
	}
	var helperIDs, manual, clients []string
	seen := map[string]bool{}
	for _, e := range es {
		state := A.off
		if e.Enabled {
			state = A.on
		}
		line := fmt.Sprintf("%s: %s %s", e.Client, kindWord(A, e.Kind), e.Name)
		if e.Program != "" && e.Program != e.Name {
			line += " (" + e.Program + ")"
		}
		res.Evidence = append(res.Evidence, line+" — "+state)
		if !e.Enabled {
			continue
		}
		if !seen[e.Client] {
			seen[e.Client] = true
			clients = append(clients, e.Client)
		}
		switch e.Disable {
		case autostart.ByHelper:
			helperIDs = append(helperIDs, e.ID)
		case autostart.Manual:
			if e.Command != "" {
				manual = append(manual, e.Command)
			}
			res.because(ReasonAutostartManual)
		default:
			res.because(ReasonAutostartManual)
		}
	}
	if len(clients) == 0 {
		return res
	}
	res.Status = StatusWarn
	res.Summary = fmt.Sprintf(A.found, strings.Join(clients, ", "))
	res.Fix = &FixHint{Text: A.fix}
	if len(helperIDs) > 0 {
		res.because(ReasonAutostartOn)
		res.Fix.AutoFix, res.Fix.Param = FixDisableAutostart, strings.Join(helperIDs, ",")
	}
	if len(manual) > 0 {
		res.Fix.Command = strings.Join(manual, "\n")
	}
	return res
}

func kindWord(A *autostartTexts, k autostart.Kind) string {
	if w, ok := A.kinds[k]; ok {
		return w
	}
	return string(k)
}

type autostartTexts struct {
	title, notHere, unread, partial string
	none, found, fix, on, off       string
	denied, notFound, sep           string
	cfgDenied, cfgMissing           string
	kinds                           map[autostart.Kind]string
}

var zhAutostart = autostartTexts{
	title:      "其他代理软件自启动",
	notHere:    "这里不检查自启动项（由后台助手检查）",
	unread:     "读不到自启动项",
	partial:    "有的自启动位置读不到：%s",
	none:       "没有其他代理软件会自己启动",
	found:      "%s 会在登录或开机时自己启动，抢回网络",
	fix:        "关掉它们的自启动（可以撤销）",
	on:         "会自启动",
	off:        "已关闭",
	denied:     "没有权限（不是不存在）",
	notFound:   "不存在",
	sep:        "：",
	cfgDenied:  "没有权限读取它的配置文件（文件在，只是需要管理员）",
	cfgMissing: "也找不到它的配置文件",
	kinds: map[autostart.Kind]string{
		autostart.ScheduledTask: "计划任务", autostart.RunKey: "启动项（Run 键）",
		autostart.LaunchAgent: "LaunchAgent", autostart.LaunchDaemon: "LaunchDaemon", autostart.LoginItem: "登录项",
		autostart.SystemdSystem: "systemd 服务", autostart.SystemdUser: "systemd 用户服务", autostart.XDGAutostart: "桌面自启动",
	},
}

var enAutostart = autostartTexts{
	title:      "Other proxy apps starting by themselves",
	notHere:    "Autostarts are checked by the helper, not here",
	unread:     "Could not read the autostart entries",
	partial:    "Some autostart places could not be read: %s",
	none:       "No other proxy app starts by itself",
	found:      "%s will start again at login or boot and take the network back",
	fix:        "Turn off their autostart (you can undo it)",
	on:         "starts by itself",
	off:        "turned off",
	denied:     "access denied (not missing)",
	notFound:   "not found",
	sep:        ": ",
	cfgDenied:  "its config file is there but this process may not read it (administrators only)",
	cfgMissing: "and its config file was not found",
	kinds: map[autostart.Kind]string{
		autostart.ScheduledTask: "scheduled task", autostart.RunKey: "Run key",
		autostart.LaunchAgent: "LaunchAgent", autostart.LaunchDaemon: "LaunchDaemon", autostart.LoginItem: "login item",
		autostart.SystemdSystem: "systemd unit", autostart.SystemdUser: "systemd user unit", autostart.XDGAutostart: "desktop autostart",
	},
}

func autostartText(L *texts) *autostartTexts {
	if L == textTables["en"] {
		return &enAutostart
	}
	return &zhAutostart
}

func init() {
	fixCatalog[ReasonAutostartOn] = FixSpec{Kind: FixAuto, Action: FixDisableAutostart}
	fixCatalog[ReasonAutostartManual] = FixSpec{Kind: FixManual, Open: OpenLoginItems}
	fixCatalog[ReasonAutostartManual+"@"+Linux] = FixSpec{Kind: FixManual}
	fixCatalog[ReasonAutostartManual+"@"+Windows] = FixSpec{Kind: FixManual, Open: OpenLoginItems}

	zhFixTexts[ReasonAutostartOn] = FixText{"另一个代理软件登记了自启动，登录或开机后会再起来，抢走默认路由和 9090 端口。",
		"由后台助手关掉这些自启动项：只是停用，不删除，可以撤销。软件本身不动。"}
	zhFixTexts[ReasonAutostartOn+"@"+Windows] = FixText{"另一个代理软件登记了计划任务或启动项，登录后会再起来，抢走默认路由和 9090 端口。",
		"由后台助手停用这些计划任务和启动项（以系统身份，不需要再确认管理员）：只是停用，不删除，可以撤销。"}
	zhFixTexts[ReasonAutostartManual] = FixText{"另一个代理软件把自己加进了登录项，登录后会再起来。",
		"在「系统设置 → 通用 → 登录项与扩展」里关掉它，或在那个软件自己的设置里关掉「开机启动」。"}
	zhFixTexts[ReasonAutostartManual+"@"+Linux] = FixText{"另一个代理软件登记了用户级自启动，登录后会再起来。",
		"用你自己的账户运行下面的命令关掉它（后台助手不改你的用户服务）。"}
	zhFixTexts[ReasonAutostartManual+"@"+Windows] = FixText{"另一个代理软件登记了自启动，登录后会再起来。",
		"在「任务管理器 → 启动应用」里禁用它，或在那个软件自己的设置里关掉开机启动。"}

	enFixTexts[ReasonAutostartOn] = FixText{"Another proxy app is registered to start by itself; after a login or restart it comes back and takes the default route and port 9090.",
		"The helper turns these autostart entries off: disabled, not deleted, and you can undo it. The app itself is left alone."}
	enFixTexts[ReasonAutostartOn+"@"+Windows] = FixText{"Another proxy app has a scheduled task or a Run key; after you sign in it comes back and takes the default route and port 9090.",
		"The helper disables these scheduled tasks and startup entries (it runs as SYSTEM, so no extra administrator prompt): disabled, not deleted, and you can undo it."}
	enFixTexts[ReasonAutostartManual] = FixText{"Another proxy app added itself to your login items and comes back when you log in.",
		"Turn it off in System Settings → General → Login Items & Extensions, or in that app's own \"launch at login\" setting."}
	enFixTexts[ReasonAutostartManual+"@"+Linux] = FixText{"Another proxy app is registered to start in your user session.",
		"Run the command below as yourself to turn it off (the helper does not change your user services)."}
	enFixTexts[ReasonAutostartManual+"@"+Windows] = FixText{"Another proxy app is registered to start when you sign in.",
		"Disable it in Task Manager → Startup apps, or in that app's own \"start with Windows\" setting."}
}
