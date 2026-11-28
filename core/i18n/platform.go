package i18n

import "maps"

// Windows texts: the steps only a Windows install plan has
// (PlatformStrings), and the Windows wording of the texts the tables
// write for macOS (windowsWording; desktop.go applies it, from
// desktop_windows.go).

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

// windowsWording is the Windows text of each field that names a macOS
// thing, per language tag (applied as linuxWording is, desktop.go):
// Microsoft's terms (文件资源管理器, 任务栏, 通知区域, 设置 › 应用 › 启动,
// "this PC").
var windowsWording = map[string]func(*Strings){"zh-Hans": windowsZh, "en": windowsEn}

// windowsZh: Windows words in Chinese.
func windowsZh(s *Strings) {
	s.HelperDeniedHint = "只有安装后台助手的用户，或以管理员身份运行的程序，可以管理网络配置"
	s.TipAgentCLI = "在终端里用 twinnel 命令，Agent 的 MCP 配置也指向它。安装是把 Twinnel 的 bin 文件夹加入你的 PATH，之后新打开的终端才能找到它；随时可以移除。"
	s.InfoExitNode = "让这台电脑的流量从 Tailscale 里的另一台设备出去。mihomo 的 TUN 开着时，两者都要接管默认路由，会互相冲突。"
	s.ServiceInstallTitle = "为「%s」安装 Windows 服务？"
	s.ServiceLabel = "服务名"
	s.ServiceDefinition = "注册表项"
	s.OnboardPlanHint = "点击安装后 Windows 会请求一次管理员授权"
	s.OnboardIntro = "修改网络配置需要一个以 SYSTEM 身份运行的后台助手："
	s.ServiceDryRunHint = "后台助手没有以管理员权限运行，步骤没有执行"
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
	s.AsLoginItems = "打开启动应用设置"
	s.TipLeaveRemoveAll = "卸载后台助手，退出后删除 Twinnel 程序文件夹。mihomo 和 Tailscale 继续运行。不会撤销接管：要换回原来的配置，先撤销接管。"
}

// windowsEn: Windows words in English.
func windowsEn(s *Strings) {
	s.HelperDeniedHint = "Only the user who installed the helper, or a program run as administrator, can manage the network setup"
	s.TipAgentCLI = "Use twinnel in a terminal; the agents' MCP settings point to it too. Installing adds Twinnel's bin folder to your PATH, so terminals you open afterwards find it. You can remove it at any time."
	s.SelfIP = "This PC's IP"
	s.SelfName = "This PC's name"
	s.InfoTun = "TUN lets mihomo carry all of this PC's traffic, including apps that ignore the system proxy. Off, only apps that use the system proxy go through mihomo."
	s.InfoExitNode = "Sends this PC's traffic out through another device on your tailnet. While mihomo's TUN is on, both want the default route and get in each other's way."
	s.ServiceInstallTitle = "Install a Windows service for “%s”?"
	s.ServiceLabel = "Service name"
	s.ServiceDefinition = "Registry key"
	s.OnboardPlanHint = "Windows asks for administrator approval once"
	s.OnboardIntro = "Changing the network setup needs a helper that runs as SYSTEM:"
	s.ServiceDryRunHint = "The helper isn't running as administrator; no steps were run"
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
	s.AsLoginItems = "Open Startup Apps"
	s.TipLeaveRemoveAll = "Uninstalls the helper and deletes the Twinnel program folder after it quits. mihomo and Tailscale keep running. It doesn't undo the takeover: to get your original config back, undo the takeover first."
}

// cloneMaps gives s maps of its own before a patch changes an entry.
func cloneMaps(s *Strings) {
	s.MsgTexts = maps.Clone(s.MsgTexts)
	s.HistReasons = maps.Clone(s.HistReasons)
}
