package i18n

// Texts of the main window's sidebar container: the title bar's toggle,
// the 显示 menu's item with the same name, the divider, and the short nav
// label of 订阅与覆写 (cmd/app sidebar.go).

// SidebarStrings holds them.
type SidebarStrings struct {
	// SidebarToggle names the title bar button and the 显示 menu item
	// (⌃⌘S) that cycle the sidebar: expanded, icons only, hidden.
	SidebarToggle string
	// SidebarResize names the divider for assistive technology.
	SidebarResize string
	// NavConfig is 订阅与覆写's sidebar row; the page title stays
	// PageConfig.
	NavConfig string
}

var zhSidebar = SidebarStrings{
	SidebarToggle: "切换侧边栏",
	SidebarResize: "调整侧边栏宽度",
	NavConfig:     "订阅与覆写",
}

var enSidebar = SidebarStrings{
	SidebarToggle: "Toggle Sidebar",
	SidebarResize: "Resize Sidebar",
	NavConfig:     "Subs & Overrides",
}
