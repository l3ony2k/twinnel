package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/version"
)

// helperMgmtShots (TWINNEL_SHOTS_FLOW=helpermgmt) captures 设置 → 后台助手 in
// each state, and the version row in the sidebar footer and on the
// overview. The helper state, the app's version and the upgrade check are
// set directly: no CLI runs and nothing is written.
func (a *app) helperMgmtShots(save func(*mygo.Window, string)) {
	oldVersion := version.Version
	defer func() { version.Version = oldVersion }()
	connected := func(v string) status.Helper {
		return status.Helper{State: status.HelperConnected, Socket: "/var/run/twinnel/helper.sock",
			Status: &rpc.Status{HelperVersion: v, PID: 4242}}
	}
	mygo.RunOnMain(func() { a.win.SetSize(a.winWidth(), 1250) })
	for _, st := range []struct {
		name, page, app string
		h               status.Helper
		hm              helperMgmtView
	}{
		{"settings-helper-current", pageSettings, "0.2.0", connected("0.2.0"),
			helperMgmtView{check: &upgradeCheck{Action: "none"}, checkPID: 4242}},
		{"settings-helper-outdated", pageSettings, "0.2.0", connected("0.1.0"),
			helperMgmtView{check: &upgradeCheck{Action: "upgrade", Bundled: "0.2.0", Installed: "0.1.0"}, checkPID: 4242}},
		{"settings-helper-updating", pageSettings, "0.2.0", connected("0.1.0"),
			helperMgmtView{check: &upgradeCheck{Action: "upgrade"}, checkPID: 4242, running: i18n.T.HelperUpdate}},
		{"settings-helper-refused", pageSettings, "0.2.0", connected("0.2.0"),
			helperMgmtView{check: &upgradeCheck{Action: "none"}, checkPID: 4242, errMsg: i18n.T.UninstallOwnMihomo}},
		{"settings-helper-not-running", pageSettings, "0.2.0", status.Helper{State: status.HelperNotRunning, Socket: "/var/run/twinnel/helper.sock"},
			helperMgmtView{check: &upgradeCheck{Action: "none"}}},
		{"settings-helper-not-installed", pageSettings, "0.2.0", status.Helper{State: status.HelperNotInstalled},
			helperMgmtView{check: &upgradeCheck{Action: "not-installed"}}},
		{"overview-helper-outdated", pageOverview, "0.2.0", connected("0.1.0"),
			helperMgmtView{check: &upgradeCheck{Action: "upgrade"}, checkPID: 4242}},
	} {
		apply := func() {
			version.Version = st.app
			a.hm = st.hm
			a.mon.SetHelper(st.h)
			a.win.Invalidate()
		}
		mygo.RunOnMain(func() {
			old := a.page
			a.page = st.page
			a.pageChanged(old)
			apply()
		})
		time.Sleep(900 * time.Millisecond)
		mygo.RunOnMain(apply) // the helper connection may have set its own state meanwhile
		if st.hm.running != "" {
			mygo.RunOnMain(func() { a.beginOp(st.hm.running) })
			time.Sleep(500 * time.Millisecond) // past busyDelay: the spinner shows
			mygo.RunOnMain(apply)
		}
		time.Sleep(300 * time.Millisecond)
		save(a.win, st.name)
		if st.hm.running != "" {
			mygo.RunOnMain(a.endOp)
		}
	}
}
