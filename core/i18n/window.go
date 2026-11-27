package i18n

// Texts of the main window's own parts added late (node tiles, peer rows,
// the 显示 menu). Their own struct, embedded in Strings as the other
// per-area sets are; fields are promoted (T.NodeUse).

// WindowStrings holds them.
type WindowStrings struct {
	// Context menus of a policy group's head and its node tiles.
	GroupExpand   string
	GroupCollapse string
	NodeUse       string // pick this node for the group
	CopyNodeName  string

	// The app menu's 显示 (View) menu, which lists the pages with ⌘1…⌘9.
	ViewMenu string

	// A diff's folded unchanged lines (cmd/app diffSeparator).
	DiffExpandUp, DiffExpandDown, DiffExpandAll string
	DiffUnchanged                               string // %d: lines
}

var zhWindow = WindowStrings{
	GroupExpand:   "展开",
	GroupCollapse: "收起",
	NodeUse:       "使用这个节点",
	CopyNodeName:  "复制节点名",
	ViewMenu:      "显示",

	DiffExpandUp:   "向上展开",
	DiffExpandDown: "向下展开",
	DiffExpandAll:  "全部展开",
	DiffUnchanged:  "%d 行未改动",
}
