package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// surfacesShots (TWINNEL_SHOTS_FLOW=surfaces) renders the shared surfaces
// that the default pass rarely shows: the toasts (success with 查看, a
// failed operation with ✕), the segmented control, enabled and disabled
// main actions, the failed row, the diff block, the loading skeleton and
// the unreachable state with its details open and closed. Synthetic data
// only; hidden windows, captured from memory.
func (a *app) surfacesShots(save func(*mygo.Window, string)) {
	T := i18n.T
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	// Toasts over 总览: a success that links to the log, a failure that
	// stays with ✕, an error from a direct c.Toast (typeless, prefixed).
	on(func() {
		old := a.page
		a.page = pageOverview
		a.pageChanged(old)
		a.toast, a.toastOp = T.Saved, true
		a.toastErr = i18n.F(T.OpFailedTitle, T.Apply)
	})
	time.Sleep(1200 * time.Millisecond)
	save(a.win, "toasts")

	var win *mygo.Window
	mygo.RunOnMain(func() {
		// The skeleton shows once the box has waited busyDelay.
		a.loadingSeen = map[string][2]time.Time{"surfaces": {time.Now().Add(-time.Second), time.Now()}}
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "surfaces", Hidden: true, Width: a.winWidth(), Height: 1100,
			Content: ui.View(a.surfacesGallery),
		})
	})
	time.Sleep(1500 * time.Millisecond)
	save(win, "surfaces-gallery")
	mygo.RunOnMain(func() { win.Close() })
}

// surfaceDiff is a synthetic diff for the gallery.
const surfaceDiff = ` rules:
-  - DOMAIN-SUFFIX,example.org,PROXY
+  - DOMAIN-SUFFIX,example.org,DIRECT
+  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve
   - MATCH,PROXY
`

func (a *app) surfacesGallery(c *ui.Context) {
	a.applyTheme(c)
	c.DisableAnimations()
	t := c.Theme()
	T := i18n.T
	c.Root().Background(t.Background)
	ui.Column(c).Padding(24, plat.pagePadX).Gap(16).Fill().Children(func() {
		card(c, T.Mode, func() {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				mode := 0
				kvControl(segmented(c.Key("seg"), &mode, T.ModeName("rule"), T.ModeName("global"), T.ModeName("direct")))
				layout := 1
				segmented(c.Key("seg2"), &layout, T.TraySpeedOneLine, T.TraySpeedTwo)
				segmented(c.Key("seg3"), &layout, T.TraySpeedOneLine, T.TraySpeedTwo).Disabled(true)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				primaryButton(c.Key("p-on"), T.Save, false)
				primaryButton(c.Key("p-off"), T.Save, true)
				ui.PrimaryButton(c.Key("p-old"), T.Save).Disabled(true) // MyGo's own, for comparison
				pushButton(c.Key("push"), T.Retry)
				toolIcon(c.Key("tool"), iconRefresh, T.Retry, false)
			})
			failedRow(c.Key("failed"), T.Unreachable, "Get \"https://api.github.com/repos/MetaCubeX/mihomo/releases\": dial tcp: i/o timeout", func() {}, false)
		})
		card(c, T.Preview, func() {
			diffBlock(c, diffLines(surfaceDiff), true)
		})
		a.loadingBox(c, "surfaces", 120)
		ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Basis(0).Grow(1).Children(func() {
				a.unreachableState(c, svcMihomo, "dial tcp 127.0.0.1:19897: connect: connection refused")
			})
			ui.Column(c).Basis(0).Grow(1).Children(func() {
				a.unreachableState(c.Key("ts"), svcTailscale, "dial unix /var/run/tailscaled.socket: connect: no such file or directory")
			})
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			countBadge(c, 5, toneNeutral, false)
			countBadge(c, 2, toneAttention, false)
			countBadge(c, 1, toneAlert, false)
		})
	})
}
