package i18n

// AutostartStrings are the takeover page's card of other proxy apps that
// start by themselves (core/autostart): what will come back after the
// takeover, and turning it off through the helper.
type AutostartStrings struct {
	AsHead, AsTip          string // the card's title and its ⓘ
	AsOn, AsOff            string // an entry's state
	AsDisable, AsDisabling string // the card's button (helper entries)
	AsUndo                 string // after a disable
	AsDisabled             string // %d entries were turned off
	AsLoginItems           string // a guided entry's link (macOS login items / Windows startup apps)
	AsCopyCmd              string // a manual entry's link: copies its command
	AsKinds                map[string]string
}

var zhAutostart = AutostartStrings{
	AsHead: "会自己启动的其他代理软件",
	AsTip: "这些软件登记了开机或登录时自动启动。接管只会停掉正在运行的，下次登录它们会再起来，抢走默认路由和 9090 端口。" +
		"停用只是关掉它们的自启动，不删除，随时可以撤销。",
	AsOn:         "会自启动",
	AsOff:        "已停用",
	AsDisable:    "停用自启动",
	AsDisabling:  "正在停用…",
	AsUndo:       "撤销",
	AsDisabled:   "已停用 %d 项自启动",
	AsLoginItems: "打开登录项设置",
	AsCopyCmd:    "复制命令",
	AsKinds: map[string]string{
		"scheduled-task": "计划任务", "run-key": "启动项", "launch-agent": "LaunchAgent", "launch-daemon": "系统守护进程",
		"login-item": "登录项", "systemd-system": "systemd 服务", "systemd-user": "systemd 用户服务", "xdg-autostart": "桌面自启动",
	},
}

var enAutostart = AutostartStrings{
	AsHead: "Other Proxy Apps That Start by Themselves",
	AsTip: "These apps are registered to start at boot or login. A takeover only stops what runs now; at the next login they come back and take the default route and port 9090. " +
		"Turning them off disables the autostart entries without deleting them, and you can undo it.",
	AsOn:         "Starts by itself",
	AsOff:        "Turned off",
	AsDisable:    "Turn Off Autostart",
	AsDisabling:  "Turning off…",
	AsUndo:       "Undo",
	AsDisabled:   "Turned off %d autostart entr{y|ies}",
	AsLoginItems: "Open Login Items",
	AsCopyCmd:    "Copy Command",
	AsKinds: map[string]string{
		"scheduled-task": "Scheduled task", "run-key": "Startup entry", "launch-agent": "LaunchAgent", "launch-daemon": "System daemon",
		"login-item": "Login item", "systemd-system": "systemd unit", "systemd-user": "systemd user unit", "xdg-autostart": "Desktop autostart",
	},
}
