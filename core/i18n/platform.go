package i18n

import (
	"maps"
	"runtime"
)

// Texts that depend on the desktop Twinnel runs on. The tables in the
// other files are written for macOS (Finder, Keychain, LaunchDaemon, the
// menu bar, ⌘); on another system, platformLangs swaps each language's
// table for a copy with that system's words (windowsZh, windowsEn), so
// For, Set and Tag work unchanged. A system without a patch keeps the
// macOS wording.

// PlatformStrings are texts only another system shows. Their own struct,
// embedded in Strings.
type PlatformStrings struct {
	// The install plan's sc.exe steps (Windows; cmd/app/installplan.go).
	PlanSCCreate, PlanSCConfig, PlanSCFailure, PlanSCDescription, PlanSCStart, PlanSCDelete string
}

var zhPlatform = PlatformStrings{
	PlanSCCreate:      "注册 Windows 服务",
	PlanSCConfig:      "设为开机自动启动，以 SYSTEM 身份运行",
	PlanSCFailure:     "服务意外退出时自动重启",
	PlanSCDescription: "写入服务说明",
	PlanSCStart:       "启动后台助手服务",
	PlanSCDelete:      "删除服务",
}

var enPlatform = PlatformStrings{
	PlanSCCreate:      "Register the Windows service",
	PlanSCConfig:      "Start at boot, as SYSTEM",
	PlanSCFailure:     "Restart the service if it exits unexpectedly",
	PlanSCDescription: "Set the service's description",
	PlanSCStart:       "Start the helper service",
	PlanSCDelete:      "Delete the service",
}

// platformPatches are the per-system rewrites of each language, by GOOS
// and language tag.
var platformPatches = map[string]map[string]func(*Strings){
	"windows": {"zh-Hans": windowsZh, "en": windowsEn},
}

// platformLangs returns ls with each language's table replaced by a
// patched copy for goos, when goos has a patch for it.
func platformLangs(goos string, ls []Lang) []Lang {
	p := platformPatches[goos]
	if p == nil {
		return ls
	}
	out := make([]Lang, len(ls))
	for i, l := range ls {
		out[i] = l
		if f := p[l.Tag]; f != nil {
			s := *l.S
			// The maps are shared with the original: a patch replaces
			// them before changing an entry (cloneMaps).
			f(&s)
			out[i].S = &s
		}
	}
	return out
}

var hostGOOS = runtime.GOOS

// windowsZh: Windows words in Chinese (Microsoft's terms: 文件资源管理器,
// 任务栏, 通知区域, 回收站, 设置 › 应用 › 启动).
func windowsZh(s *Strings) {
	s.InfoExitNode = "让这台电脑的流量从 Tailscale 里的另一台设备出去。mihomo 的 TUN 开着时，两者都要接管默认路由，会互相冲突。"
	s.ServiceInstallTitle = "为「%s」安装 Windows 服务？"
	s.OnboardPlanHint = "点击安装后 Windows 会请求一次管理员授权"
	s.MenuBar = "任务栏"
	s.LayerReorderHint = "拖动可以调整顺序（Alt+Shift+↑ / Alt+Shift+↓）"
	s.ItemSecrets = "加密保存的凭据"
	s.ConfigEmptyHint = "添加订阅链接或文件，或者接管这台电脑上已有的 mihomo 配置"
	s.TkEffStoreSub = "加密保存 · 首次手动更新时下载"
	s.ShowInFinder = "在文件资源管理器中显示"
	s.InvHelperService = "后台助手服务（Windows 服务）"
	s.InvSecrets = "加密保存的凭据"
	s.LeaveRemoveAllWhat = "删除 Twinnel 的数据和加密保存的凭据"
	s.TipPrimaryGroup = "Twinnel 把这个策略组当作出口：总览和托盘面板显示它选的节点和延迟。"
	s.TipOpenAtLogin = "登录后 Twinnel 出现在任务栏的通知区域。mihomo 和 Tailscale 由系统服务运行，不需要 Twinnel 开着。"
	s.TipHelper = "以 SYSTEM 身份运行的 Windows 服务。改配置、更新订阅和内核、启停 mihomo 服务都由它完成，Twinnel 本身不需要管理员权限。"
	s.ItemApp = "Twinnel 应用"
	s.ItemAppTrash = "Twinnel 程序文件夹（退出后删除）"
	s.OpenAtLoginApprove = "在 设置 › 应用 › 启动 中允许 Twinnel"
	s.SetupTitle = "设置这台电脑"
	s.StepSetUpMac = "设置这台电脑"
	s.SetupDone = "这台电脑已设置好"
	s.TipLeaveRemoveAll = "卸载后台助手，退出后删除 Twinnel 程序文件夹。mihomo 和 Tailscale 继续运行。不会撤销接管：要换回原来的配置，先撤销接管。"
}

// windowsEn: Windows words in English.
func windowsEn(s *Strings) {
	s.SelfIP = "This PC's IP"
	s.SelfName = "This PC's name"
	s.InfoTun = "TUN lets mihomo carry all of this PC's traffic, including apps that ignore the system proxy. Off, only apps that use the system proxy go through mihomo."
	s.InfoExitNode = "Sends this PC's traffic out through another device on your tailnet. While mihomo's TUN is on, both want the default route and get in each other's way."
	s.ServiceInstallTitle = "Install a Windows service for “%s”?"
	s.OnboardPlanHint = "Windows asks for administrator approval once"
	s.MenuBar = "Taskbar"
	s.LayerReorderHint = "Drag to reorder (Alt+Shift+↑ / Alt+Shift+↓)"
	s.ConfigEmptyHint = "Add a subscription link or file, or take over the mihomo config already on this PC"
	cloneMaps(s)
	s.HistReasons["setup"] = "Set up this PC"
	s.SetupTitle = "Set Up This PC"
	s.StepSetUpMac = "Set up this PC"
	s.SetupDone = "This PC is set up"
	s.MsgTexts["refuses-exposed"] = "%[1]s listens on %[2]s beyond this PC without a secret; Twinnel won't write that"
	s.TkEffStoreSub = "Saved encrypted · downloaded on the first manual update"
	s.ShowInFinder = "Show in File Explorer"
	s.LeaveInstalled = "What Twinnel Installed on This PC"
	s.InvHelperService = "Helper service (Windows service)"
	s.InvSecrets = "Encrypted credentials"
	s.ItemSecrets = "encrypted credentials"
	s.LeaveRemoveAllWhat = "Deletes Twinnel's data and encrypted credentials"
	s.TipPrimaryGroup = "Twinnel treats this group as the exit: the overview and the tray panel show the node it chose and its latency."
	s.TipOpenAtLogin = "Twinnel appears in the taskbar's notification area after you sign in. mihomo and Tailscale run as system services and don't need Twinnel open."
	s.TipHelper = "A Windows service that runs as SYSTEM. It changes the config, updates subscriptions and the core, and starts or stops mihomo's service, so Twinnel itself never needs administrator rights."
	s.OvThisDevice = "This PC"
	s.TipAgent = "Lets AI agents on this PC (Claude Code, Codex, Cursor and others) see and operate Twinnel through MCP. Every change shows in Logs → Twinnel with the agent's name."
	s.ItemApp = "the Twinnel app"
	s.ItemAppTrash = "the Twinnel program folder (deleted after it quits)"
	s.OpenAtLoginApprove = "Allow Twinnel in Settings › Apps › Startup"
	s.TipLeaveRemoveAll = "Uninstalls the helper and deletes the Twinnel program folder after it quits. mihomo and Tailscale keep running. It doesn't undo the takeover: to get your original config back, undo the takeover first."
}

// cloneMaps gives s maps of its own before a patch changes an entry.
func cloneMaps(s *Strings) {
	s.MsgTexts = maps.Clone(s.MsgTexts)
	s.HistReasons = maps.Clone(s.HistReasons)
}
