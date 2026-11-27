package i18n

// Texts of the remaining pages reworked by the round-3 page pass (内核,
// 安装, 关于, 健康检查, the menu bar panel). Their own struct, embedded
// in Strings, so parallel work on i18n.go does not collide.

// PagesStrings holds them.
type PagesStrings struct {
	PgComponents  string // 内核: the list of GEO 数据库, 控制面板, Tailscale
	PgCheckFailed string // %s: what was checked ("mihomo"): a failed update check
}

var zhPages = PagesStrings{
	PgComponents:  "组件",
	PgCheckFailed: "没能检查 %s 的更新",
}

var enPages = PagesStrings{
	PgComponents:  "Components",
	PgCheckFailed: "Couldn't check %s for updates",
}
