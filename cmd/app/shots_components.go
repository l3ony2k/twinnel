package main

import (
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// componentShots (TWINNEL_SHOTS_FLOW=components) captures every state of
// the GEO 数据库 and 控制面板 rows of 内核 → 组件 (components.go): not
// installed, installed and not checked, up to date, newer, downloading,
// verifying, reloading, done, reload pending, failed with the other
// routes, partial, auto-update failed, dashboard not served. Each state is
// set in memory (the views and a busy label for the running ones), so
// nothing is downloaded and the helper's own status reads are held off.
// No component text names the system, so one pass serves macOS, Windows
// and Linux; TWINNEL_LANG=en shoots the English wording.
func (a *app) componentShots(save func(*mygo.Window, string)) {
	day := time.Now().Add(-6 * 24 * time.Hour)
	checked := time.Now().Add(-25 * time.Minute)
	const dir = "/Users/example/.config/mihomo"
	src := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"
	file := func(name, rel string, size int64) rpc.AccessoryFile {
		return rpc.AccessoryFile{Name: name, File: dir + "/" + rel, URL: src + rel, Installed: true, Size: size, ModTime: day,
			Version: "latest", Source: src + rel, Verifiable: true, RemoteTag: "latest", RemoteSize: size, RemoteTime: day}
	}
	geoInstalled := func() []rpc.AccessoryFile {
		return []rpc.AccessoryFile{file("geoip", "GeoIP.dat", 18_900_000), file("geosite", "GeoSite.dat", 8_200_000),
			file("mmdb", "geoip.metadb", 9_100_000), file("asn", "ASN.mmdb", 7_800_000)}
	}
	geoMissing := func() []rpc.AccessoryFile {
		fs := geoInstalled()
		for i := range fs {
			fs[i] = rpc.AccessoryFile{Name: fs[i].Name, File: fs[i].File, URL: fs[i].URL, UpdateAvailable: true, RemoteSize: fs[i].RemoteSize, Verifiable: true}
		}
		return fs
	}
	// geoOnlyMMDB: a config with GEOIP rules only (Windows QA round 1):
	// the .dat files and the ASN database are absent and unused.
	geoOnlyMMDB := func() []rpc.AccessoryFile {
		fs := geoInstalled()
		for _, i := range []int{0, 1, 3} {
			fs[i] = rpc.AccessoryFile{Name: fs[i].Name, File: fs[i].File, URL: fs[i].URL, Unused: true}
		}
		return fs
	}
	geoNoneUsed := func() []rpc.AccessoryFile {
		fs := geoOnlyMMDB()
		fs[2] = rpc.AccessoryFile{Name: fs[2].Name, File: fs[2].File, URL: fs[2].URL, Unused: true}
		return fs
	}
	geoNewer := func() []rpc.AccessoryFile {
		fs := geoInstalled()
		for i := range fs[:2] {
			fs[i].UpdateAvailable, fs[i].RemoteTime = true, time.Now().Add(-3*time.Hour)
		}
		return fs
	}
	uiFile := func(installed bool, version, remote string) rpc.AccessoryFile {
		f := rpc.AccessoryFile{Name: "ui", File: dir + "/ui", URL: "https://github.com/Zephyruso/zashboard/releases/latest/download/dist.zip",
			RemoteTag: remote, RemoteSize: 2_400_000, Verifiable: true}
		if installed {
			f.Installed, f.ModTime, f.Version, f.Source = true, day, version, f.URL
		}
		f.UpdateAvailable = !installed || (remote != "" && remote != version)
		return f
	}
	type state struct {
		name string
		geo  accessoryView
		ui   accessoryView
		busy string // a running download: its op label
	}
	gk, uk := geoKind(), uiKind()
	geoCk := func(files []rpc.AccessoryFile, at time.Time) *rpc.AccessoryCheck {
		return &rpc.AccessoryCheck{Dir: dir, Files: files, CheckedAt: at, Checked: !at.IsZero(), AutoUpdate: rpc.GeoAutoOff}
	}
	uiCk := func(f rpc.AccessoryFile, at time.Time, served bool) *rpc.AccessoryCheck {
		return &rpc.AccessoryCheck{Dir: dir, Files: []rpc.AccessoryFile{f}, CheckedAt: at, Checked: !at.IsZero(), Configured: served, Path: "/ui/"}
	}
	states := []state{
		{name: "not-installed",
			geo: accessoryView{check: geoCk(geoMissing(), time.Time{})},
			ui:  accessoryView{check: uiCk(uiFile(false, "", ""), time.Time{}, false)}},
		{name: "not-installed-sized",
			geo: accessoryView{check: geoCk(geoMissing(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(false, "", "v1.91.0"), checked, false)}},
		{name: "installed-unchecked",
			geo: accessoryView{check: geoCk(geoInstalled(), time.Time{})},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.90.0", ""), time.Time{}, true)}},
		{name: "up-to-date",
			geo: accessoryView{check: geoCk(geoInstalled(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "geo-only-mmdb",
			geo: accessoryView{check: geoCk(geoOnlyMMDB(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "geo-none-used",
			geo: accessoryView{check: geoCk(geoNoneUsed(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "newer",
			geo: accessoryView{check: geoCk(geoNewer(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.90.0", "v1.91.0"), checked, true)}},
		{name: "downloading", busy: gk.opUpdate(),
			geo: accessoryView{check: geoCk(geoNewer(), checked), prog: &rpc.AccessoryProgress{Kind: "geo", Stage: rpc.AccStageDownload, Name: "geosite", Done: 11_000_000, Total: 27_100_000}},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "verifying", busy: gk.opUpdate(),
			geo: accessoryView{check: geoCk(geoNewer(), checked), prog: &rpc.AccessoryProgress{Kind: "geo", Stage: rpc.AccStageVerify}},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "reloading", busy: gk.opUpdate(),
			geo: accessoryView{check: geoCk(geoNewer(), checked), prog: &rpc.AccessoryProgress{Kind: "geo", Stage: rpc.AccStageReload}},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "ui-configuring", busy: uk.opUpdate(),
			geo: accessoryView{check: geoCk(geoInstalled(), checked)},
			ui:  accessoryView{check: uiCk(uiFile(false, "", "v1.91.0"), checked, false), prog: &rpc.AccessoryProgress{Kind: "ui", Stage: rpc.AccStageReload}}},
		{name: "done",
			geo: accessoryView{check: geoCk(geoInstalled(), time.Now()), res: &rpc.AccessoryUpdateResult{Updated: []string{"geoip", "geosite"}, Reloaded: true}},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), time.Now(), true), res: &rpc.AccessoryUpdateResult{Updated: []string{"ui"}, Configured: true, Files: []rpc.AccessoryFile{uiFile(true, "v1.91.0", "v1.91.0")}}}},
		{name: "reload-pending",
			geo: accessoryView{check: geoCk(geoInstalled(), time.Now()), res: &rpc.AccessoryUpdateResult{Updated: []string{"geoip", "geosite"}, ReloadNeeded: true,
				ReloadError: "mihomo runs a config Twinnel does not manage yet; restart mihomo to use the new files"}},
			ui: accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
		{name: "failed",
			geo: accessoryView{check: geoCk(geoMissing(), checked), failWhy: failReason(rpc.Errorf(rpc.CodeFailed, "%s", "geosite: download https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat: dial tcp 140.82.112.4:443: i/o timeout")), failErr: "geosite: download https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat: dial tcp 140.82.112.4:443: i/o timeout"},
			ui:  accessoryView{check: uiCk(uiFile(false, "", "v1.91.0"), checked, false), failWhy: failReason(rpc.Errorf(rpc.CodeFailed, "GitHub API rate limit reached (60 requests an hour without a token); try again until 15:40")), failErr: "GitHub API rate limit reached (60 requests an hour without a token); try again until 15:40", proxyOpen: true}},
		{name: "partial-auto-failed",
			geo: func() accessoryView {
				fs := geoInstalled()
				fs[3] = rpc.AccessoryFile{Name: "asn", File: dir + "/ASN.mmdb", UpdateAvailable: true, RemoteSize: 7_800_000}
				fs[1].Source, fs[1].Version = "", ""
				ck := geoCk(fs, checked)
				ck.AutoUpdate, ck.LastAuto, ck.AutoError = rpc.GeoAutoWeekly, time.Now().Add(-26*time.Hour), "geosite: dial tcp: i/o timeout"
				return accessoryView{check: ck}
			}(),
			ui: accessoryView{check: uiCk(uiFile(true, "v1.91.0", ""), time.Time{}, false)}},
		{name: "check-failed",
			geo: accessoryView{check: geoCk(geoInstalled(), checked), checkErr: helperError(rpc.Errorf(rpc.CodeFailed, "Get \"https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/tags/latest\": dial tcp: lookup api.github.com: no such host"))},
			ui:  accessoryView{check: uiCk(uiFile(true, "v1.91.0", "v1.91.0"), checked, true)}},
	}
	connected := status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
	set := func(st state) {
		mygo.RunOnMain(func() {
			a.shotSnap = func(s *status.Snapshot) {
				s.Helper = connected
				s.WritesAllowed = true // the actions as a user sees them; nothing runs
				s.Mihomo.Reachable = true
				s.Loaded = true
			}
			a.shotSection = "components"
			g, u := st.geo, st.ui
			g.loading, u.loading = true, true // hold off geo.status / ui.status
			a.hd.kern.geo, a.hd.kern.ui = g, u
			a.hd.busy = st.busy
			a.hd.spanOp = ""
			if st.busy != "" {
				a.beginOp(st.busy)
				a.hd.span.start = time.Now().Add(-time.Second) // spinner already shown
			}
			old := a.page
			a.page = pageKernel
			if old != pageKernel {
				a.pageChanged(old)
			}
			a.invalidate()
		})
		time.Sleep(700 * time.Millisecond)
	}
	for _, st := range states {
		set(st)
		save(a.win, "components-"+st.name)
	}
	mygo.RunOnMain(func() {
		a.hd.busy = ""
		a.shotSection = ""
		a.hd.kern.geo.loading, a.hd.kern.ui.loading = false, false
	})
}
