package i18n

import "strings"

// The tables are written for macOS: Finder, the Keychain, LaunchDaemons,
// "this Mac". On other desktops a few texts name what is there instead.
// desktopWording rewrites those texts in a language's table; the
// platform's file (desktop_linux.go) applies it to every table once, at
// init, before any code reads T. The texts are kept here, with the
// tables, so that a translator sees them next to the macOS ones.

// linuxWording is the Linux text of each field that names a macOS thing,
// per language tag. Fields not listed keep the macOS table's text (they
// are not shown on Linux, or name nothing platform-specific).
var linuxWording = map[string]func(s *Strings){
	"zh-Hans": func(s *Strings) {
		s.ShowInFinder = "在文件管理器中显示"
		s.InvHelperService = "后台助手服务（systemd）"
		s.InvSecrets = "密钥文件"
		s.ItemSecrets = "密钥文件"
		s.LeaveRemoveAllWhat = "删除 Twinnel 的数据和密钥文件"
		s.TkEffStoreSub = "存入后台助手的密钥文件 · 首次手动更新时下载"
		s.LayerReorderHint = "拖动可以调整顺序（Ctrl+Alt+↑ / Ctrl+Alt+↓）"
		s.NotificationsUnsupportedTip = "桌面没有通知服务"
		s.ServiceInstallTitle = "为「%s」安装 systemd 服务？"
		s.OnboardPlanHint = "点击安装后会要求验证一次身份"
		s.ItemApp = "Twinnel"
		s.ItemAppTrash = "Twinnel 程序（用安装它的方式卸载）"
		s.TipLeaveRemoveAll = "卸载后台助手，删除 Twinnel 的设置。程序本身用安装它的方式卸载。mihomo 和 Tailscale 继续运行。不会撤销接管：要换回原来的配置，先撤销接管。"
		s.TipOpenAtLogin = "登录后 Twinnel 出现在系统托盘。mihomo 和 Tailscale 由系统服务运行，不需要 Twinnel 开着。"
		s.TipPrimaryGroup = "Twinnel 把这个策略组当作出口：总览和托盘面板显示它选的节点和延迟。"
		s.TipAgentCLI = "在终端里用 Twinnel 命令，Agent 的 MCP 配置也指向它。安装是建一个指向 Twinnel 命令行工具的链接，随时可以移除。"
		s.InfoExitNode = "让这台电脑的流量从 Tailscale 里的另一台设备出去。mihomo 的 TUN 开着时，两者都要接管默认路由，会互相冲突。"
		s.ConfigEmptyHint = "添加订阅链接或文件，或者接管这台电脑上已有的 mihomo 配置"
		s.MenuBar = "系统托盘"
		s.TraySpeedSwitch = "在系统托盘显示网速"
		s.HelperDeniedHint = "只有后台助手所用组（sudo 或 wheel）里的用户可以管理网络配置"
		s.SetupTitle = "设置这台电脑"
		s.StepSetUpMac = "设置这台电脑"
		s.SetupDone = "这台电脑已设置好"
	},
	"en": func(s *Strings) {
		s.ShowInFinder = "Show in File Manager"
		s.InvHelperService = "Helper service (systemd)"
		s.InvSecrets = "Secrets file"
		s.ItemSecrets = "secrets file"
		s.LeaveRemoveAllWhat = "Deletes Twinnel's data and secrets file"
		s.TkEffStoreSub = "Saved to the helper's secrets file · downloaded on the first manual update"
		s.LayerReorderHint = "Drag to reorder (Ctrl+Alt+↑ / Ctrl+Alt+↓)"
		s.NotificationsUnsupportedTip = "The desktop has no notification service"
		s.ServiceInstallTitle = "Install a systemd service for “%s”?"
		s.OnboardPlanHint = "You'll authenticate once"
		s.ItemApp = "Twinnel"
		s.ItemAppTrash = "the Twinnel app (uninstall it the way you installed it)"
		s.TipLeaveRemoveAll = "Uninstalls the helper and deletes Twinnel's settings. Uninstall the app itself the way you installed it. mihomo and Tailscale keep running. It doesn't undo the takeover: to get your original config back, undo the takeover first."
		s.TipOpenAtLogin = "Twinnel appears in the system tray after you log in. mihomo and Tailscale run as system services and don't need Twinnel open."
		s.TipPrimaryGroup = "Twinnel treats this group as the exit: the overview and the tray panel show the node it chose and its latency."
		s.TipAgentCLI = "Use Twinnel in a terminal; the agents' MCP settings point to it too. Installing makes a link to Twinnel's command line tool that you can remove at any time."
		s.TipAgent = "Lets AI agents on this computer (Claude Code, Codex, Cursor and others) see and operate Twinnel through MCP. Every change shows in Logs → Twinnel with the agent's name."
		s.SelfIP = "This computer's IP"
		s.SelfName = "This computer's name"
		s.InfoTun = "TUN lets mihomo carry all of this computer's traffic, including apps that ignore the system proxy. Off, only apps that use the system proxy go through mihomo."
		s.InfoExitNode = "Sends this computer's traffic out through another device on your tailnet. While mihomo's TUN is on, both want the default route and get in each other's way."
		s.OvThisDevice = "This computer"
		s.ConfigEmptyHint = "Add a subscription link or file, or take over the mihomo config already on this computer"
		s.LeaveInstalled = "What Twinnel Installed on This Computer"
		s.MenuBar = "System tray"
		s.TraySpeedSwitch = "Show network speed in the system tray"
		s.HelperDeniedHint = "Only users in the helper's group (sudo or wheel) can manage the network setup"
		s.SetupTitle = "Set Up This Computer"
		s.StepSetUpMac = "Set up this computer"
		s.SetupDone = "This computer is set up"
		cloneMaps(s)
		s.HistReasons["setup"] = "Set up this computer"
		if t, ok := s.MsgTexts["refuses-exposed"]; ok {
			s.MsgTexts["refuses-exposed"] = strings.ReplaceAll(t, "this Mac", "this computer")
		}
	},
}

// written are the tables as written, for macOS, copied before any init
// rewords them (package variables are set before init runs). Every
// system's table is made from these, so what one system shows never
// depends on the system the code runs on.
var written = func() map[string]Strings {
	m := map[string]Strings{}
	for _, l := range langs {
		c := *l.S
		cloneMaps(&c)
		m[l.Tag] = c
	}
	return m
}()

// WordedFor returns a copy of the table of language tag (an exact tag of
// Langs) as goos shows it: the table as written, with goos's wording
// (linuxWording, windowsWording) on top. nil for an unknown tag.
func WordedFor(tag, goos string) *Strings {
	w, ok := written[tag]
	if !ok {
		return nil
	}
	c := w
	cloneMaps(&c)
	if fn := platformWording[goos][tag]; fn != nil {
		fn(&c)
	}
	return &c
}

// platformWording is each non-macOS system's wording, per language tag.
var platformWording = map[string]map[string]func(*Strings){Linux: linuxWording, Windows: windowsWording}

// applyDesktop words every language's table for goos, the system this
// runs on (desktop_linux.go, desktop_windows.go), at init.
func applyDesktop(goos string) {
	for _, l := range langs {
		*l.S = *WordedFor(l.Tag, goos)
	}
}
