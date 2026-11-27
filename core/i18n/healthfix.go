package i18n

// HealthFixStrings are the health page's fix actions (health-fixes): each
// failing check's reasons, in plain words, with 修复 (one click, after a
// preview), 去调整 (the place in the app) or 为什么无法自动修复.
type HealthFixStrings struct {
	FixDo         string // a one-click fix's button
	FixGo         string // a guided fix's button
	FixWhyNot     string // a manual fix's disclosure
	FixRerun      string // a fix that is only checking again
	FixOpenSystem string // a manual fix's button that opens the system setting
	FixSheetTitle string // the preview's title: the check's title
	FixWillDo     string // the preview's list heading
	FixUndoable   string // the preview: the fix can be put back
	FixNoUndo     string // the preview: it cannot
	FixApply      string // the preview's primary button
	FixRunning    string // the button while the fix runs
	FixUndo       string // the row's button after a fix
	FixDone       string // toast: the check passes now
	FixStill      string // toast: the fix ran, the check still fails
	FixUndone     string // toast
	FixFailed     string // toast title: the fix failed (%s: the check)
	FixDetails    string // the check's own summary, under the reasons
	FixLoading    string // the preview is being prepared
	// HealthSplit is the health checks' one number when both failures
	// and warnings are in it: %d all, %d failed, %d warnings.
	HealthSplit string
	// HealthFound is HealthSplit before takeover (what was found on this
	// computer, not something that broke).
	HealthFound string
	HealthLine  string // %s: HealthCount, on a line that does not say 健康检查 already
}

var zhHealthFix = HealthFixStrings{
	FixDo:         "修复",
	FixGo:         "去调整",
	FixWhyNot:     "为什么无法自动修复",
	FixRerun:      "重新检查",
	FixOpenSystem: "打开设置",
	FixSheetTitle: "修复：%s",
	FixWillDo:     "将会",
	FixUndoable:   "修复后可以撤销。",
	FixNoUndo:     "修复后不能撤销。",
	FixApply:      "修复",
	FixRunning:    "正在修复…",
	FixUndo:       "撤销修复",
	FixDone:       "已修复：%s",
	FixStill:      "已执行，%s 仍未通过",
	FixUndone:     "已撤销",
	FixFailed:     "修复 %s 失败",
	FixDetails:    "检查结果",
	FixLoading:    "正在准备…",
	HealthSplit:   "%d 项需要处理：%d 项失败、%d 项警告",
	HealthFound:   "发现 %d 个问题：%d 项失败、%d 项警告",
	HealthLine:    "健康检查：%s",
}

var enHealthFix = HealthFixStrings{
	FixDo:         "Fix",
	FixGo:         "Adjust",
	FixWhyNot:     "Why Twinnel can't fix this",
	FixRerun:      "Check Again",
	FixOpenSystem: "Open Settings",
	FixSheetTitle: "Fix: %s",
	FixWillDo:     "This will",
	FixUndoable:   "You can undo it afterwards.",
	FixNoUndo:     "This can't be undone.",
	FixApply:      "Fix",
	FixRunning:    "Fixing…",
	FixUndo:       "Undo Fix",
	FixDone:       "Fixed: %s",
	FixStill:      "Done, but %s still fails",
	FixUndone:     "Undone",
	FixFailed:     "Couldn't fix %s",
	FixDetails:    "Check result",
	FixLoading:    "Preparing…",
	HealthSplit:   "%d need{s|} attention: %d failed, %d warning{s}",
	HealthFound:   "%d issue{s} found: %d failed, %d warning{s}",
	HealthLine:    "Health: %s",
}

// HealthCount words the health checks' one number (fail + warn), split
// when both are in it, so every place that shows the number says the
// same: the health page, the overview, the flyout and the banner. found:
// before takeover (what was found here, not something that broke).
func (s *Strings) HealthCount(fail, warn int, found bool) string {
	switch {
	case fail > 0 && warn > 0 && found:
		return F(s.HealthFound, fail+warn, fail, warn)
	case fail > 0 && warn > 0:
		return F(s.HealthSplit, fail+warn, fail, warn)
	case found:
		return F(s.OvHealthIssues, fail+warn)
	case fail > 0:
		return F(s.HealthFailN, fail)
	}
	return F(s.HealthWarnN, warn)
}
