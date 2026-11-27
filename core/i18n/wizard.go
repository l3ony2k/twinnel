package i18n

// Texts of the first-run wizard (cmd/app/wizard.go): six steps from a
// fresh install to a working proxy and tailnet. Embedded in Strings;
// fields are promoted (T.WizTitle).
//
// The texts that name the platform (the administrator prompt, where the
// tray icon lives, how Tailscale is installed) are not in Strings: they
// live in wizardOS, one set per system, so that a screenshot run can show
// the Windows or Linux wording on any host (WizardOSFor).

// WizardStrings holds the wizard's texts that read the same everywhere.
type WizardStrings struct {
	WizTitle     string
	WizStepNames []string // the progress bar's six short names, in order
	WizProgress  string   // %d: this step, %d: all steps
	WizLater     string   // leaves the wizard; 继续设置 brings it back
	WizBack      string
	WizNext      string
	WizSkip      string
	WizFinish    string

	// The way back after 稍后再说: a row on 总览 and the helper pages.
	WizResume, WizResumeHead string
	WizGateHead              string // a helper page while the helper is not installed
	WizNeedsHelper           string // a step that needs the helper before it can run

	// ① 欢迎
	WizWelcomeHead string
	WizWelcomeBody string // %s: 这台 Mac / 这台电脑
	WizPlanHead    string
	WizPlan        []string // one line per later step: what it does
	WizStart       string

	// ② 后台助手
	WizHelperHead, WizHelperBody                         string
	WizHelperInstall, WizHelperInstalling, WizHelperDone string
	WizHelperChanges                                     string // the disclosure over the install plan
	WizHelperFailed, WizHelperRetryHint                  string

	// ③ 代理内核
	WizMihomoHead, WizMihomoBody                           string
	WizMihomoFound                                         string // %s: the running mihomo's config file
	WizMihomoTakeOver, WizMihomoOwn                        string
	WizMihomoTakenOver                                     string
	WizMihomoDownload, WizMihomoDownloading, WizMihomoDone string // WizMihomoDone: %s version
	WizMihomoExternal                                      string // the helper runs an existing binary
	WizMihomoSubFirst                                      string // a failed download: add the subscription, download through it
	WizMihomoFailed                                        string
	TipWizTakeOver                                         string

	// ④ 订阅
	WizSubHead, WizSubBody        string
	WizSubURL, WizSubPlaceholder  string
	WizSubImport, TipWizImport    string
	WizSubImported                string // %s: the file's name
	WizSubClear                   string // drops the picked file
	WizSubStart, WizSubStarting   string
	WizSubReady                   string // %s nodes, %s rules
	WizNode, WizNodes             string
	WizSubCounting, WizSubNoNodes string
	WizSubProgressHead            string
	WizSubDownloadLater           string // mihomo waits for the subscription's nodes

	// ⑤ Tailscale
	WizTSHead, WizTSBody                       string
	WizTSFound                                 string // %s: version, or "" (WizTSFoundBare)
	WizTSFoundBare                             string
	WizTSInstall, WizTSInstalling, WizTSFailed string
	WizTSLogin, WizTSLoginHint                 string
	WizTSLoginWait, WizTSLoginAgain            string
	WizTSLoggedIn                              string // %s: the tailnet
	WizTSLoggedInBare                          string
	WizTSSkip                                  string

	// ⑥ 完成
	WizDoneHead, WizDoneIssues                           string // WizDoneIssues: %d checks
	WizDoneHelper, WizDoneProxy, WizDoneSub, WizDoneTS   string // the summary's row names
	WizDoneHealth                                        string
	WizDoneInstalled, WizDoneRunning, WizDoneNotSet      string // WizDoneRunning: %s version
	WizDoneSkipped, WizDoneHealthOK, WizDoneHealthIssues string // WizDoneHealthIssues: %d
	WizDoneHealthWait, WizDoneHealthOpen                 string
	WizDoneFix                                           string // jumps back to a step not done
	WizTrayHead                                          string
	WizNextHead                                          string
	WizNextSteps                                         []string
	WizDoneOpen                                          string
}

var zhWizard = WizardStrings{
	WizTitle:     "设置 Twinnel",
	WizStepNames: []string{"欢迎", "后台助手", "代理内核", "订阅", "Tailscale", "完成"},
	WizProgress:  "第 %d 步，共 %d 步",
	WizLater:     "稍后再说",
	WizBack:      "上一步",
	WizNext:      "下一步",
	WizSkip:      "跳过",
	WizFinish:    "完成",

	WizResume:      "继续设置",
	WizResumeHead:  "设置还没完成",
	WizGateHead:    "这一页要先装好后台助手",
	WizNeedsHelper: "这一步要先安装后台助手",

	WizWelcomeHead: "欢迎使用 Twinnel",
	WizWelcomeBody: "Twinnel 让代理（mihomo）和私有网络（Tailscale）在%s上同时运行、互不干扰。设置好以后，两者的状态和开关都在一个地方。",
	WizPlanHead:    "接下来会做这些",
	WizPlan: []string{
		"安装后台助手，只需要授权一次",
		"准备代理内核 mihomo：接管已有的，或下载官方版本",
		"添加订阅：粘贴链接，或导入配置文件",
		"安装并登录 Tailscale，不需要可以跳过",
	},
	WizStart: "开始设置",

	WizHelperHead:       "安装后台助手",
	WizHelperBody:       "后台助手是一个常驻的系统服务。修改网络设置、下载内核、启停 mihomo 都由它完成，所以只需要授权这一次，以后 Twinnel 不再要管理员权限。",
	WizHelperInstall:    "安装",
	WizHelperInstalling: "正在安装…",
	WizHelperDone:       "后台助手已安装，正在运行",
	WizHelperChanges:    "会做哪些改动",
	WizHelperFailed:     "后台助手没有装好",
	WizHelperRetryHint:  "如果关掉了授权窗口，点「重试」再来一次。",

	WizMihomoHead:        "代理内核 mihomo",
	WizMihomoBody:        "mihomo 是真正转发代理流量的程序。Twinnel 从 mihomo 官方的 GitHub 发布页下载它，校验后安装，以后在「内核」页更新。",
	WizMihomoFound:       "这里已经有一个 mihomo 在运行，配置文件是 %s。",
	WizMihomoTakeOver:    "接管现有的 mihomo",
	WizMihomoOwn:         "不接管，下载新的",
	WizMihomoTakenOver:   "已接管现有的 mihomo",
	WizMihomoDownload:    "下载 mihomo",
	WizMihomoDownloading: "正在下载并校验…",
	WizMihomoDone:        "mihomo %s 已安装",
	WizMihomoExternal:    "使用这里已有的 mihomo",
	WizMihomoSubFirst:    "先添加订阅，用它的节点下载",
	WizMihomoFailed:      "mihomo 没有下载下来",
	TipWizTakeOver:       "Twinnel 读取现在的配置，拆成订阅和覆写，确认合成结果和原来一致后才替换。节点和规则保持不变，随时可以撤销。",

	WizSubHead:          "订阅",
	WizSubBody:          "订阅是代理服务商给你的一个链接，里面有节点和分流规则。粘贴链接；如果你在用 Clash Verge 或 Clash Party，也可以直接导入它的配置文件。",
	WizSubURL:           "订阅链接",
	WizSubPlaceholder:   "https://",
	WizSubImport:        "导入配置文件…",
	TipWizImport:        "可以导入 Twinnel 的备份（.zip），或 Clash Verge、Clash Party 的配置和覆写文件（.yaml）。文件只交给后台助手保存，不会上传。",
	WizSubImported:      "已读取 %s",
	WizSubClear:         "移除",
	WizSubStart:         "保存并启动代理",
	WizSubStarting:      "正在启动代理…",
	WizSubReady:         "代理已启动：%s、%s",
	WizNode:             "%d 个节点",
	WizNodes:            "%d 个节点",
	WizSubCounting:      "代理已启动，正在读取节点…",
	WizSubNoNodes:       "订阅里没有节点。检查链接是否完整，或联系服务商。",
	WizSubProgressHead:  "进度",
	WizSubDownloadLater: "mihomo 会通过订阅的节点下载",

	WizTSHead:         "Tailscale（可选）",
	WizTSBody:         "Tailscale 把你的电脑、手机和服务器连成一个私有网络，在任何地方都能互相访问。Twinnel 会让它和代理同时工作。用不到可以跳过，以后在「Tailscale 设备」页再装。",
	WizTSFound:        "已安装 Tailscale %s",
	WizTSFoundBare:    "已安装 Tailscale",
	WizTSInstall:      "安装 Tailscale",
	WizTSInstalling:   "正在下载并安装…",
	WizTSFailed:       "Tailscale 没有装好",
	WizTSLogin:        "登录 Tailscale",
	WizTSLoginHint:    "会在浏览器里打开 Tailscale 的登录页，登录后回到这里。",
	WizTSLoginWait:    "在浏览器里完成登录…",
	WizTSLoginAgain:   "重新打开登录页",
	WizTSLoggedIn:     "已登录 %s",
	WizTSLoggedInBare: "已登录",
	WizTSSkip:         "跳过",

	WizDoneHead:         "设置完成",
	WizDoneIssues:       "设置完成，有 %d 项需要注意",
	WizDoneHelper:       "后台助手",
	WizDoneProxy:        "代理",
	WizDoneSub:          "订阅",
	WizDoneTS:           "Tailscale",
	WizDoneHealth:       "健康检查",
	WizDoneInstalled:    "已安装",
	WizDoneRunning:      "运行中 · %s",
	WizDoneNotSet:       "还没设置",
	WizDoneSkipped:      "已跳过",
	WizDoneHealthOK:     "全部通过",
	WizDoneHealthIssues: "%d 项需要注意",
	WizDoneHealthWait:   "正在检查…",
	WizDoneHealthOpen:   "查看",
	WizDoneFix:          "去设置",
	WizTrayHead:         "以后在哪里找到 Twinnel",
	WizNextHead:         "接下来",
	WizNextSteps: []string{
		"在「策略组」页选节点、测延迟",
		"在「Tailscale 设备」页查看你的设备",
		"网络不对劲时，打开「健康检查」页看哪里出了问题",
	},
	WizDoneOpen: "打开总览",
}

var enWizard = WizardStrings{
	WizTitle:     "Set Up Twinnel",
	WizStepNames: []string{"Welcome", "Helper", "Proxy Core", "Subscription", "Tailscale", "Done"},
	WizProgress:  "Step %d of %d",
	WizLater:     "Not Now",
	WizBack:      "Back",
	WizNext:      "Next",
	WizSkip:      "Skip",
	WizFinish:    "Finish",

	WizResume:      "Continue Setup",
	WizResumeHead:  "Setup isn't finished",
	WizGateHead:    "This page needs the helper",
	WizNeedsHelper: "This step needs the helper first",

	WizWelcomeHead: "Welcome to Twinnel",
	WizWelcomeBody: "Twinnel runs a proxy (mihomo) and a private network (Tailscale) side by side on %s, without either breaking the other. Once it's set up, both are managed in one place.",
	WizPlanHead:    "What happens next",
	WizPlan: []string{
		"Install the helper; you approve it once",
		"Get the mihomo proxy core: take over the one you have, or download the official one",
		"Add your subscription: paste its link, or import a config file",
		"Install and log in to Tailscale; skip it if you don't need it",
	},
	WizStart: "Get Started",

	WizHelperHead:       "Install the Helper",
	WizHelperBody:       "The helper is a small system service. It changes network settings, downloads the proxy core and starts or stops mihomo, so you approve it once and Twinnel never asks for administrator rights again.",
	WizHelperInstall:    "Install",
	WizHelperInstalling: "Installing…",
	WizHelperDone:       "The helper is installed and running",
	WizHelperChanges:    "What it changes",
	WizHelperFailed:     "The helper wasn't installed",
	WizHelperRetryHint:  "If you closed the approval window, click Retry to try again.",

	WizMihomoHead:        "The mihomo Proxy Core",
	WizMihomoBody:        "mihomo is the program that actually carries proxy traffic. Twinnel downloads it from mihomo's official GitHub releases, verifies it and installs it; you can update it later on the Core page.",
	WizMihomoFound:       "A mihomo is already running here, with the config file %s.",
	WizMihomoTakeOver:    "Take Over This mihomo",
	WizMihomoOwn:         "Download a New One Instead",
	WizMihomoTakenOver:   "Took over the existing mihomo",
	WizMihomoDownload:    "Download mihomo",
	WizMihomoDownloading: "Downloading and verifying…",
	WizMihomoDone:        "mihomo %s is installed",
	WizMihomoExternal:    "Using the mihomo already here",
	WizMihomoSubFirst:    "Add the Subscription First, Download Through It",
	WizMihomoFailed:      "mihomo didn't download",
	TipWizTakeOver:       "Twinnel reads your current config, splits it into a subscription and overrides, and only switches once the result matches. Your nodes and rules stay the same, and you can undo it at any time.",

	WizSubHead:          "Subscription",
	WizSubBody:          "A subscription is a link from your proxy provider with the nodes and the routing rules. Paste it; if you use Clash Verge or Clash Party, you can import its config file instead.",
	WizSubURL:           "Subscription link",
	WizSubPlaceholder:   "https://",
	WizSubImport:        "Import a Config File…",
	TipWizImport:        "Imports a Twinnel backup (.zip), or a Clash Verge or Clash Party config or override file (.yaml). The file goes to the helper only; nothing is uploaded.",
	WizSubImported:      "Read %s",
	WizSubClear:         "Remove",
	WizSubStart:         "Save and Start the Proxy",
	WizSubStarting:      "Starting the proxy…",
	WizSubReady:         "The proxy is running: %s, %s",
	WizNode:             "%d node",
	WizNodes:            "%d node{s}",
	WizSubCounting:      "The proxy is running; reading the nodes…",
	WizSubNoNodes:       "The subscription has no nodes. Check that the link is complete, or ask your provider.",
	WizSubProgressHead:  "Progress",
	WizSubDownloadLater: "mihomo downloads through the subscription's nodes",

	WizTSHead:         "Tailscale (Optional)",
	WizTSBody:         "Tailscale joins your computers, phones and servers into one private network you can reach from anywhere. Twinnel keeps it working alongside the proxy. Skip it if you don't need it; you can install it later on the Tailscale Devices page.",
	WizTSFound:        "Tailscale %s is installed",
	WizTSFoundBare:    "Tailscale is installed",
	WizTSInstall:      "Install Tailscale",
	WizTSInstalling:   "Downloading and installing…",
	WizTSFailed:       "Tailscale wasn't installed",
	WizTSLogin:        "Log In to Tailscale",
	WizTSLoginHint:    "Opens Tailscale's login page in your browser; come back here when you're done.",
	WizTSLoginWait:    "Finish logging in in your browser…",
	WizTSLoginAgain:   "Open the Login Page Again",
	WizTSLoggedIn:     "Logged in to %s",
	WizTSLoggedInBare: "Logged in",
	WizTSSkip:         "Skip",

	WizDoneHead:         "You're All Set",
	WizDoneIssues:       "Set up, with %d thing{s} to look at",
	WizDoneHelper:       "Helper",
	WizDoneProxy:        "Proxy",
	WizDoneSub:          "Subscription",
	WizDoneTS:           "Tailscale",
	WizDoneHealth:       "Health checks",
	WizDoneInstalled:    "Installed",
	WizDoneRunning:      "Running · %s",
	WizDoneNotSet:       "Not set up",
	WizDoneSkipped:      "Skipped",
	WizDoneHealthOK:     "All passed",
	WizDoneHealthIssues: "%d to look at",
	WizDoneHealthWait:   "Checking…",
	WizDoneHealthOpen:   "View",
	WizDoneFix:          "Set Up",
	WizTrayHead:         "Where to find Twinnel",
	WizNextHead:         "Next",
	WizNextSteps: []string{
		"Pick a node and test delays on the Proxy Groups page",
		"See your devices on the Tailscale Devices page",
		"If the network acts up, the Health page shows what's wrong",
	},
	WizDoneOpen: "Open Overview",
}

// WizardOS are the wizard's texts that name the platform.
type WizardOS struct {
	Machine      string // 这台 Mac, this PC: WizWelcomeBody's %s
	AdminPrompt  string // what happens when the helper installs
	TSInstallHow string // how Tailscale gets installed
	TrayWhere    string // where the tray / menu bar icon lives
}

// wizardOS is WizardOS per language tag, then per GOOS.
var wizardOS = map[string]map[string]WizardOS{
	"zh-Hans": {
		"darwin": {
			Machine:      "这台 Mac",
			AdminPrompt:  "点「安装」后，macOS 会弹窗请你输入这台 Mac 的登录密码。",
			TSInstallHow: "装的是开源版 tailscaled，作为系统服务运行，不占用系统的 VPN 通道。",
			TrayWhere:    "Twinnel 住在屏幕右上角的菜单栏里。点它的图标看状态、换节点；关掉这个窗口，它照样在后台工作。",
		},
		"windows": {
			Machine:      "这台电脑",
			AdminPrompt:  "点「安装」后，Windows 会弹出「用户账户控制」窗口，选「是」。",
			TSInstallHow: "从 Tailscale 官网下载官方安装包并安装，安装时可能再弹一次「用户账户控制」。",
			TrayWhere:    "Twinnel 在任务栏右下角的通知区域。看不到图标时，点「^」展开隐藏的图标，可以把它拖到任务栏上常驻。关掉这个窗口，它照样在后台工作。",
		},
		"linux": {
			Machine:      "这台电脑",
			AdminPrompt:  "点「安装」后，系统会弹出身份验证窗口，输入你的登录密码。",
			TSInstallHow: "下载 Tailscale 官方的 tailscaled，安装成系统服务。",
			TrayWhere:    "Twinnel 在系统托盘里（GNOME 要先装 AppIndicator 扩展才显示托盘图标）。关掉这个窗口，它照样在后台工作。",
		},
	},
	"en": {
		"darwin": {
			Machine:      "this Mac",
			AdminPrompt:  "When you click Install, macOS asks for this Mac's login password.",
			TSInstallHow: "Installs the open-source tailscaled as a system service, so it doesn't take the system's VPN slot.",
			TrayWhere:    "Twinnel lives in the menu bar at the top right of the screen. Click its icon to see the status and switch nodes; it keeps working after you close this window.",
		},
		"windows": {
			Machine:      "this PC",
			AdminPrompt:  "When you click Install, Windows shows a User Account Control prompt; choose Yes.",
			TSInstallHow: "Downloads Tailscale's official installer and runs it; Windows may show User Account Control once more.",
			TrayWhere:    "Twinnel is in the notification area at the right end of the taskbar. If you don't see it, click ^ to show hidden icons; you can drag it onto the taskbar to keep it there. It keeps working after you close this window.",
		},
		"linux": {
			Machine:      "this computer",
			AdminPrompt:  "When you click Install, your system asks you to authenticate with your login password.",
			TSInstallHow: "Downloads Tailscale's official tailscaled and installs it as a system service.",
			TrayWhere:    "Twinnel is in the system tray (on GNOME, install the AppIndicator extension to see tray icons). It keeps working after you close this window.",
		},
	},
}

// WizardOSFor returns the wizard's platform texts for goos ("darwin",
// "windows", "linux") in the current language; another system gets
// Linux's.
func WizardOSFor(goos string) WizardOS {
	m := wizardOS[Tag()]
	if m == nil {
		m = wizardOS[langs[0].Tag]
	}
	if w, ok := m[goos]; ok {
		return w
	}
	return m["linux"]
}
