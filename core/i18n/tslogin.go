package i18n

// Texts of the Tailscale state panel (cmd/app tslogin.go): one per
// state of the local node, each with its action, and the login flow
// (browser link, QR code, timeout). Texts that name a system thing come
// in a macOS, Windows and Linux variant (…Mac, …Win, …Linux); the app
// picks one by the platform it runs on, so shots can show all three.

// TSLoginStrings holds them.
type TSLoginStrings struct {
	TSLWhat string // ⓘ body: what Tailscale is

	TSLNotInstalledTitle string
	TSLNotInstalledBody  string
	TSLInstall           string // button: install (the wizard / setup)
	TSLDownload          string // link: the official download page

	TSLDownTitle     string
	TSLDownBodyMac   string // the open-source tailscaled (LaunchDaemon)
	TSLDownBodyApp   string // the macOS app
	TSLDownBodyWin   string // the official client's service
	TSLDownBodyLinux string // systemd
	TSLStartService  string // button: start the service through the helper
	TSLOpenApp       string // button: open the macOS app

	TSLStartingTitle string // the node is connecting (no action)

	TSLStoppedTitle string
	TSLStoppedBody  string
	TSLConnect      string // button

	TSLNeedsLoginTitle string
	TSLNeedsLoginBody  string
	TSLLogIn           string // button

	TSLLoggedOutTitle string
	TSLLoggedOutBody  string

	TSLExpiredTitle string
	TSLExpiredBody  string
	TSLRelogin      string // button: log in again (also renews a key that expires soon)

	TSLMachineAuthTitle string
	TSLMachineAuthBody  string
	TSLAdminConsole     string // button: open the admin console's machines page

	TSLWaitingTitle string // the login runs: finish it in the browser
	TSLFetchingURL  string // before tailscaled hands out the link
	TSLOpenAgain    string // button: open the link again
	TSLCopyLink     string // button
	TSLScanQR       string // caption under the QR code
	TSLLinkLabel    string // accessibility label of the link

	TSLTimedOutTitle string
	TSLTimedOutBody  string

	TSLFailedTitle  string
	TSLLoggedIn     string // toast: %s is the tailnet ("" → TSLLoggedInBare)
	TSLLoggedInBare string
}

var zhTSLogin = TSLoginStrings{
	TSLWhat: "Tailscale 把你的电脑、手机和服务器连成一个只有你能进的私有网络（tailnet），在哪儿都能互相访问。Twinnel 让它和 mihomo 一起工作。",

	TSLNotInstalledTitle: "还没有安装 Tailscale",
	TSLNotInstalledBody:  "先安装，再用你的 Tailscale 账号登录。",
	TSLInstall:           "安装 Tailscale",
	TSLDownload:          "从官网下载",

	TSLDownTitle:     "Tailscale 没有运行",
	TSLDownBodyMac:   "tailscaled 服务停了。",
	TSLDownBodyApp:   "Tailscale 应用没有打开。",
	TSLDownBodyWin:   "Windows 服务「Tailscale」没有运行。",
	TSLDownBodyLinux: "systemd 服务 tailscaled 没有运行。",
	TSLStartService:  "启动 Tailscale",
	TSLOpenApp:       "打开 Tailscale",

	TSLStartingTitle: "正在连接 Tailscale…",

	TSLStoppedTitle: "Tailscale 已断开",
	TSLStoppedBody:  "这台设备现在不在你的 tailnet 里。",
	TSLConnect:      "连接",

	TSLNeedsLoginTitle: "登录 Tailscale",
	TSLNeedsLoginBody:  "用你的 Tailscale 账号登录，这台设备就会加入你的 tailnet。",
	TSLLogIn:           "登录 Tailscale",

	TSLLoggedOutTitle: "已退出 Tailscale 账号",
	TSLLoggedOutBody:  "重新登录后，这台设备回到你的 tailnet。",

	TSLExpiredTitle: "这台设备的登录已过期",
	TSLExpiredBody:  "重新登录后恢复连接。",
	TSLRelogin:      "重新登录",

	TSLMachineAuthTitle: "等待管理员批准",
	TSLMachineAuthBody:  "已经登录。tailnet 的管理员在管理后台批准这台设备后，它会自动连接。",
	TSLAdminConsole:     "打开管理后台",

	TSLWaitingTitle: "在浏览器里完成登录…",
	TSLFetchingURL:  "正在获取登录链接…",
	TSLOpenAgain:    "再次打开",
	TSLCopyLink:     "复制链接",
	TSLScanQR:       "或用手机扫码登录",
	TSLLinkLabel:    "Tailscale 登录链接",

	TSLTimedOutTitle: "登录超时",
	TSLTimedOutBody:  "这个登录链接已失效。",

	TSLFailedTitle:  "没能开始登录",
	TSLLoggedIn:     "已登录 Tailscale · %s",
	TSLLoggedInBare: "已登录 Tailscale",
}

var enTSLogin = TSLoginStrings{
	TSLWhat: "Tailscale links your computers, phones and servers into a private network only you can reach (a tailnet), wherever they are. Twinnel makes it work alongside mihomo.",

	TSLNotInstalledTitle: "Tailscale Isn't Installed",
	TSLNotInstalledBody:  "Install it, then log in with your Tailscale account.",
	TSLInstall:           "Install Tailscale",
	TSLDownload:          "Download from tailscale.com",

	TSLDownTitle:     "Tailscale Isn't Running",
	TSLDownBodyMac:   "The tailscaled service has stopped.",
	TSLDownBodyApp:   "The Tailscale app isn't open.",
	TSLDownBodyWin:   "The Tailscale Windows service isn't running.",
	TSLDownBodyLinux: "The tailscaled systemd service isn't running.",
	TSLStartService:  "Start Tailscale",
	TSLOpenApp:       "Open Tailscale",

	TSLStartingTitle: "Connecting to Tailscale…",

	TSLStoppedTitle: "Tailscale Is Disconnected",
	TSLStoppedBody:  "This device isn't on your tailnet right now.",
	TSLConnect:      "Connect",

	TSLNeedsLoginTitle: "Log In to Tailscale",
	TSLNeedsLoginBody:  "Log in with your Tailscale account to add this device to your tailnet.",
	TSLLogIn:           "Log In to Tailscale",

	TSLLoggedOutTitle: "Logged Out of Tailscale",
	TSLLoggedOutBody:  "Log in again to put this device back on your tailnet.",

	TSLExpiredTitle: "This Device's Login Has Expired",
	TSLExpiredBody:  "Log in again to reconnect.",
	TSLRelogin:      "Log In Again",

	TSLMachineAuthTitle: "Waiting for Admin Approval",
	TSLMachineAuthBody:  "You're logged in. Once an admin of the tailnet approves this device in the admin console, it connects by itself.",
	TSLAdminConsole:     "Open Admin Console",

	TSLWaitingTitle: "Finish Logging In in Your Browser…",
	TSLFetchingURL:  "Getting the login link…",
	TSLOpenAgain:    "Open Again",
	TSLCopyLink:     "Copy Link",
	TSLScanQR:       "Or scan with your phone",
	TSLLinkLabel:    "Tailscale login link",

	TSLTimedOutTitle: "Login Timed Out",
	TSLTimedOutBody:  "This login link has expired.",

	TSLFailedTitle:  "Couldn't Start the Login",
	TSLLoggedIn:     "Logged in to Tailscale · %s",
	TSLLoggedInBare: "Logged in to Tailscale",
}
