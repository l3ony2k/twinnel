package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/update"
)

// aboutShots (DUET_SHOTS_FLOW=about) captures the 关于 page whole (a tall
// window) and the header in each 检查更新 state, set directly: no request
// goes to GitHub.
func (a *app) aboutShots(save func(*mygo.Window, string)) {
	set := func(fn func(v *aboutView)) {
		mygo.RunOnMain(func() {
			if a.page != pageAbout {
				old := a.page
				a.page = pageAbout
				a.pageChanged(old)
			}
			fn(&a.about)
			a.win.Invalidate()
		})
		time.Sleep(900 * time.Millisecond)
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 1500) })
	set(func(v *aboutView) {
		v.checked, v.res = true, update.Result{State: update.NoReleases}
		v.faqOpen = map[string]bool{}
	})
	save(a.win, "about")
	set(func(v *aboutView) {
		for _, q := range []string{"为什么 duet 不占用系统 VPN 通道？", "「只读」是什么意思？"} {
			v.faqOpen[q] = true
		}
	})
	save(a.win, "about-faq-open")
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), a.winHeight()) })
	for _, st := range []struct {
		name string
		fn   func(v *aboutView)
	}{
		{"about-checking", func(v *aboutView) { v.checking, v.checked = true, false }},
		{"about-latest", func(v *aboutView) {
			v.checking, v.checked, v.err, v.res = false, true, "", update.Result{State: update.UpToDate, Latest: "0.1.0"}
		}},
		{"about-available", func(v *aboutView) {
			v.checking, v.checked, v.err = false, true, ""
			v.res = update.Result{State: update.Available, Latest: "0.2.0", URL: "https://github.com/l3ony2k/duet/releases/tag/v0.2.0"}
		}},
		{"about-failed", func(v *aboutView) { v.checking, v.checked, v.err = false, true, "没能连上 GitHub" }},
	} {
		set(st.fn)
		if st.name == "about-checking" {
			time.Sleep(400 * time.Millisecond) // past busyDelay: the spinner shows
		}
		save(a.win, st.name)
	}
	set(func(v *aboutView) { *v = aboutView{cliVersion: v.cliVersion, cliLoaded: true} })
}

// installShots (DUET_SHOTS_FLOW=install) captures 关于 → 安装: the page
// in each helper state, set on the monitor just before the capture (the
// helper connection may overwrite it later; the shot is taken first).
func (a *app) installShots(save func(*mygo.Window, string)) {
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 1100) })
	for _, st := range []struct {
		name string
		h    status.Helper
	}{
		{"install-not-installed", status.Helper{State: status.HelperNotInstalled}},
		{"install-not-running", status.Helper{State: status.HelperNotRunning, Socket: "/var/run/duet/duet.sock"}},
		{"install-installed", status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}},
	} {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = pageInstall
			a.pageChanged(old)
			a.mon.SetHelper(st.h)
			a.win.Invalidate()
		})
		time.Sleep(1500 * time.Millisecond) // the install plan's dry run
		mygo.RunOnMain(func() { a.mon.SetHelper(st.h); a.win.Invalidate() })
		time.Sleep(300 * time.Millisecond)
		save(a.win, st.name)
	}
}
