package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// shots saves PNGs of every page of the main window and of the panel into
// dir, then quits: TWINNEL_SHOTS=<dir>. A development aid for checking the
// layout without screen-recording rights; MyGo renders the native content
// itself (Window.CapturePage).
func (a *app) shots(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	save := func(w *mygo.Window, name string) {
		png, err := w.CapturePage()
		if err != nil {
			log.Printf("capture %s: %v", name, err)
			return
		}
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, png, 0o644); err != nil {
			log.Print(err)
			return
		}
		log.Printf("saved %s", path)
	}
	go func() {
		time.Sleep(6 * time.Second) // let the monitor load, traffic accumulate and the health checks finish
		if f, ok := a.shotFlows()[os.Getenv("TWINNEL_SHOTS_FLOW")]; ok {
			f(save)
			mygo.App.Quit()
			return
		}
		for _, p := range []string{pageOverview, pageProxies, pageTailscale, pageHealth, pageLogs, pageSettings, pageAbout} {
			mygo.RunOnMain(func() {
				old := a.page
				a.page = p
				a.pageChanged(old)
				a.win.Invalidate()
			})
			if p == pageLogs {
				time.Sleep(3 * time.Second)
			} else {
				time.Sleep(700 * time.Millisecond)
			}
			save(a.win, "main-"+p)
			if p == pageProxies { // the toolbar over scrolled content
				mygo.RunOnMain(func() {
					a.win.SetSize(a.winWidth(), 560) // short, so the page scrolls
					if st := a.pageScroll[p]; st != nil {
						st.Y = 240
					}
					a.win.Invalidate()
				})
				time.Sleep(500 * time.Millisecond)
				save(a.win, "main-proxies-scrolled")
				mygo.RunOnMain(func() {
					a.win.SetSize(a.winWidth(), a.winHeight())
					if st := a.pageScroll[p]; st != nil {
						st.Y = 0
					}
				})
			}
		}
		if os.Getenv("TWINNEL_SHOTS_QUICK") == "" { // quick: the main pages and the panel only
			a.helperShots(dir, save)
		}
		for _, v := range []status.TrayIcon{status.TrayOK, status.TrayWarn, status.TrayError, status.TrayUnknown,
			status.TrayTSDown, status.TrayMode, status.TrayNoHelper} {
			if err := os.WriteFile(filepath.Join(dir, "tray-"+string(v)+".png"), trayIcon(v), 0o644); err != nil {
				log.Print(err)
			}
		}
		mygo.RunOnMain(a.togglePanel)
		time.Sleep(1500 * time.Millisecond)
		var panel *mygo.Window
		mygo.RunOnMain(func() { panel = a.panel })
		if panel != nil {
			save(panel, "panel")
		}
		mygo.App.Quit()
	}()
}

// shotSaver saves a window's capture under a name.
type shotSaver = func(w *mygo.Window, name string)

// shotFlows are the passes TWINNEL_SHOTS_FLOW picks; any other value (or
// none) runs the default pass in shots.
func (a *app) shotFlows() map[string]func(save shotSaver) {
	return map[string]func(save shotSaver){
		"sidebar":         a.sidebarShots,
		"sidecollapse":    a.sidebarCollapseShots, // shots_sidebarcollapse.go
		"controls":        a.controlShots,         // shots_controls.go
		"final":           a.finalShots,           // shots_final.go
		"toolbar":         a.toolbarShots,         // shots_toolbar.go
		"install":         a.installShots,         // shots_about.go
		"helpermgmt":      a.helperMgmtShots,      // shots_helpermgmt.go
		"about":           a.aboutShots,           // shots_about.go
		"delay":           a.delayShots,           // shots_delay.go
		"egress":          a.egressShots,          // shots_egress.go
		"exit":            a.exitShots,            // shots_exit.go
		"configpolish":    a.configPolishShots,    // shots_configpolish.go
		"configui":        a.configUIShots,        // shots_configui.go
		"configpages":     a.configPagesShots,     // shots_configpages.go
		"importcfg":       a.importShots,          // shots_importcfg.go
		"takeoverexplain": a.takeoverExplainShots, // shots_takeoverexplain.go
		"lifecycle":       a.lifecycleShots,       // shots_lifecycle.go
		"status":          a.statusShots,          // shots_status.go
		"overview":        a.overviewShots,        // shots_overview.go
		"center":          a.centerShots,          // shots_center.go
		"trayspeed":       a.trayspeedShots,       // shots_trayspeed.go
		"dock":            a.dockShots,            // shots_dock.go
		"infotips":        a.infoTipShots,         // shots_infotips.go
		"derpinfo":        a.derpInfoShots,        // shots_derpinfo.go
		"flyout":          a.flyoutShots,          // shots_flyout.go
		"tslogin":         a.tsLoginShots,         // shots_tslogin.go
		"foundations":     a.foundationsShots,     // shots_foundations.go
		"surfaces":        a.surfacesShots,        // shots_surfaces.go
		"agent":           a.agentShots,           // shots_agent.go
		"readme":          a.readmeShots,          // shots_readme.go
		"wizard":          a.wizardShots,          // shots_wizard.go
		"healthfix":       a.healthFixShots,       // shots_healthfix.go
		"dnswatch":        a.dnsWatchShots,        // shots_dnswatch.go
		"takeover":        a.takeoverE2E,
	}
}

// helperShots walks the helper pages, driving the app's own handlers
// against whatever helper TWINNEL_SOCKET names (a dev helper with fakes).
// With TWINNEL_READ_ONLY=0 and TWINNEL_SHOTS_WRITE=1 it also writes: a new
// layer, apply, rollback.
func (a *app) helperShots(dir string, save func(*mygo.Window, string)) {
	on := func(fn func()) { mygo.RunOnMain(func() { fn(); a.invalidate() }) }
	wait := func(d time.Duration) {
		// Let a helper operation finish.
		deadline := time.Now().Add(20 * time.Second)
		time.Sleep(d)
		for time.Now().Before(deadline) {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" || a.hd.loading })
			time.Sleep(200 * time.Millisecond)
			if !busy {
				break
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	page := func(p string) {
		on(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
		})
		wait(800 * time.Millisecond)
	}
	var snap status.Snapshot
	mygo.RunOnMain(func() { snap = a.mon.Snapshot() })
	writes := snap.WritesAllowed && os.Getenv("TWINNEL_SHOTS_WRITE") == "1"

	page(pageConfig)
	for i := 0; i < 50; i++ { // the first-run view loads the install plan
		done := true
		mygo.RunOnMain(func() { done = snap.Helper.Connected() || a.hd.onb.loaded })
		if done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	save(a.win, "main-config")
	if !snap.Helper.Connected() {
		// The first-run / not-running view is the page; then its plan open.
		on(func() { a.hd.onb.stepsOpen, a.hd.onb.fileOpen = true, true })
		wait(600 * time.Millisecond)
		save(a.win, "onboarding-steps")
		return
	}
	on(func() {
		for _, l := range a.hd.layers {
			if l.ID == coexistLayerID {
				a.selectLayer(l)
			}
		}
	})
	wait(time.Second)
	save(a.win, "config-managed-layer")
	on(func() {
		for _, l := range a.hd.layers {
			if !l.Managed && l.Stage != stageBase {
				a.selectLayer(l) // a stored layer, its editor inline under its row
				break
			}
		}
	})
	wait(time.Second)
	save(a.win, "config-user-layer")
	if writes {
		on(func() {
			a.newLayer()
			v := &a.hd.cfg
			v.edID, v.edName = "app3-demo", "app3 demo"
			v.ed.Buffer.Set("# app3 dev run (synthetic)\n+rules:\n  - DOMAIN-SUFFIX,app3.example,DIRECT\nhosts:\n  +.lan: 192.0.2.10\n  '<+.home>': 192.0.2.11\n")
		})
		wait(500 * time.Millisecond)
		save(a.win, "config-new-layer")
		on(a.saveLayer)
		wait(time.Second)
		save(a.win, "config-save-error") // the helper refuses +.lan: it needs a list
		// The +.lan key fails compose (it needs a list): fix it, as a user would.
		on(func() {
			a.hd.cfg.ed.Buffer.Set("# app3 dev run (synthetic)\n+rules:\n  - DOMAIN-SUFFIX,app3.example,DIRECT\nhosts:\n  '<+.lan>': 192.0.2.10\n")
			a.saveLayer()
		})
		wait(time.Second)
		on(func() { a.moveLayerByID("app3-demo", -1) })
		wait(time.Second)
	}
	on(func() { a.hd.cfg.sel, a.hd.cfg.isNew, a.hd.cfg.ed = "", false, nil }) // room for the diff
	on(func() { a.runPreview(a.mon.Snapshot()) })
	wait(2 * time.Second)
	save(a.win, "config-preview")
	if writes {
		on(func() { a.hd.cfg.reason = "app3 shots: demo layer"; a.runApply() })
		wait(2 * time.Second)
		save(a.win, "config-applied")
	}

	page(pageHistory)
	var hist []string
	mygo.RunOnMain(func() {
		for _, e := range a.hd.history {
			hist = append(hist, e.ID)
		}
	})
	if len(hist) > 0 {
		oldest := hist[len(hist)-1] // history.list is newest first
		on(func() { a.hd.hist.sel = oldest; a.historyDiff(a.mon.Snapshot(), oldest) })
		wait(time.Second)
		save(a.win, "history-diff")
		if writes && len(hist) > 1 {
			on(func() {
				for _, e := range a.hd.history {
					if e.ID == oldest {
						a.historyRollbackNow(e)
					}
				}
			})
			wait(2 * time.Second)
			save(a.win, "history-rollback")
		}
	}

	page(pageKernel)
	on(a.kernelCheck)
	wait(3 * time.Second)
	for i := 0; i < 50; i++ { // tailscale.info reads pkgs.tailscale.com
		done := false
		mygo.RunOnMain(func() { done = !a.hd.kern.tsLoading })
		if done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	save(a.win, "main-kernel")
	a.kernelShots(save, wait, on, writes)
	a.takeoverShots(save, wait, on, page)
}

// kernelShots: the 服务, GEO and UI cards, one at a time (shotSection).
// With writes: a service restart (a dry run on a helper that is not root),
// geo and UI updates (downloads into the dev config dir), then 应用以生效.
func (a *app) kernelShots(save func(*mygo.Window, string), wait func(time.Duration), on func(func()), writes bool) {
	long := func() { // downloads can take a while
		for i := 0; i < 600; i++ {
			busy := true
			mygo.RunOnMain(func() { busy = a.hd.busy != "" })
			if !busy {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		wait(500 * time.Millisecond)
	}
	on(func() { a.shotSection = "services"; a.loadServices() })
	wait(1500 * time.Millisecond)
	save(a.win, "kernel-services")
	if writes {
		on(func() { a.serviceOp(rpc.MethodServiceRestart, "mihomo", i18n.T.ServiceRestart) })
		wait(2 * time.Second)
		save(a.win, "kernel-service-restart-dryrun")
	}
	for _, k := range []accessoryKind{geoKind(), uiKind()} {
		on(func() { a.shotSection = k.check; a.accessoryCheck(k) })
		long()
		save(a.win, "kernel-"+k.check)
		if writes {
			on(func() { a.accessoryUpdate(k, false) })
			long()
			save(a.win, "kernel-"+k.update)
			if k.check == rpc.MethodGeoCheck {
				var r *rpc.AccessoryUpdateResult
				mygo.RunOnMain(func() { r = a.hd.kern.geo.res })
				if r != nil && r.ReloadNeeded {
					on(func() { a.applyForce(i18n.T.AccessoryApplyReason, func() { r.ReloadNeeded = false }) })
					long()
					save(a.win, "kernel-geo-applied")
				}
			}
		}
	}
	on(func() { a.shotSection = "" })
}

// takeoverShots: the 接管 page against the helper (which on main says it
// is too old), then the wizard from TWINNEL_TAKEOVER_FIXTURE in sections.
func (a *app) takeoverShots(save func(*mygo.Window, string), wait func(time.Duration), on func(func()), page func(string)) {
	page(pageTakeover)
	wait(time.Second)
	save(a.win, "takeover-helper")
	planOK := false
	mygo.RunOnMain(func() { planOK = a.hd.connected && !a.hd.take.unsupported })
	if planOK {
		on(func() { a.takeoverMakePlan("") })
		wait(5 * time.Second)
		save(a.win, "takeover-helper-plan")
	}
	dir := os.Getenv("TWINNEL_TAKEOVER_FIXTURE")
	if dir == "" {
		return
	}
	var adopt *rpc.AdoptResult
	on(func() {
		if err := a.loadTakeoverFixture(dir); err != nil {
			log.Print(err)
		}
		adopt, a.hd.take.adopt = a.hd.take.adopt, nil
		a.shotSection = "plan"
	})
	wait(500 * time.Millisecond)
	save(a.win, "takeover-plan")
	on(func() { a.shotSection = "verdict" })
	wait(500 * time.Millisecond)
	save(a.win, "takeover-verdict")
	on(func() { a.hd.take.showText = true })
	wait(500 * time.Millisecond)
	save(a.win, "takeover-report")
	if adopt != nil {
		on(func() { a.hd.take.adopt, a.hd.take.showText, a.shotSection = adopt, false, "result" })
		wait(500 * time.Millisecond)
		save(a.win, "takeover-result")
	}
	on(func() { a.shotSection = "" })
}

// section reports whether a page section draws: always, except in shot
// mode when shotSection names another one.
func (a *app) section(name string) bool {
	return a.shotSection == "" || a.shotSection == name
}

// sidebarShots captures the source list's looks: selection in a key and a
// non-key window, hover, and the count badges on a selected row
// (TWINNEL_SHOTS_FLOW=sidebar). The key state is forced (shotNav), since a
// capture does not change which window is key.
func (a *app) sidebarShots(save func(*mygo.Window, string)) {
	yes, no := true, false
	shot := func(name, page, hover string, active *bool) {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = page
			a.pageChanged(old)
			a.shotNav.active, a.shotNav.hover = active, hover
			a.win.Invalidate()
		})
		time.Sleep(700 * time.Millisecond)
		save(a.win, "sidebar-"+name)
	}
	shot("key-overview-hover-proxies", pageOverview, pageProxies, &yes)
	shot("notkey-overview-hover-proxies", pageOverview, pageProxies, &no)
	shot("key-tailscale", pageTailscale, "", &yes)
	shot("key-health", pageHealth, pageTailscale, &yes)
	shot("notkey-health", pageHealth, "", &no)
	shot("key-settings", pageSettings, pageKernel, &yes)
}
