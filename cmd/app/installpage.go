package main

import (
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// The 安装后台助手 page: 关于 → 入门's first step opens it. It has no
// sidebar row of its own: the sidebar keeps 关于 selected (System
// Settings does the same for a pane's sub-pages), and the toolbar's ‹,
// ⌘[ or a click on 关于 go back.

// subPages maps a page without a sidebar row to the row it belongs to.
var subPages = map[string]string{pageInstall: pageAbout}

// navParent is the sidebar row that shows page: its own, or its parent's.
func navParent(page string) string {
	if p, ok := subPages[page]; ok {
		return p
	}
	return page
}

// pageBack is where ‹ and ⌘[ go from page; "" on a page with a row.
func pageBack(page string) string { return subPages[page] }

// subPageHeader is pageHeader for a sub-page: a ‹ before the title that
// goes back to its parent.
func (a *app) subPageHeader(c *ui.Context, title string) {
	back := pageBack(a.page)
	pageBar = toolbarSpec{title: title, back: func() { a.showPage(back) }}
}

// installPage shows the install guide (onboarding.go) while the helper is
// not installed, its state while it is installed but not reachable
// (helperGate), and a short 已安装 once it is connected.
func (a *app) installPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	a.subPageHeader(c, T.StepHelper)
	if !a.helperGate(c, s) {
		return
	}
	t := c.Theme()
	groupBox(c, t).Padding(16, groupPadX).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconOK).FontSize(iconLarge).TextColor(t.Success)
			ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, T.HelperInstalledTitle).FontSize(plat.leadSize).FontWeight(600).SingleLine()
				if st := s.Helper.Status; st != nil && st.HelperVersion != "" {
					num(small(c, st.HelperVersion)).SingleLine().Selectable()
				}
			})
			if pushButton(c.Key("helper-settings"), T.HelperSection).Clicked() {
				a.showPage(pageSettings)
			}
		})
	})
}
