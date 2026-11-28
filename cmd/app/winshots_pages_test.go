package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// TestWindowsPageStateShots renders the states of the pages ui-review
// section 6 names (内核 → 组件, 历史, 设置 → 本地 Agent) on the Windows path
// and on macOS for comparison, from synthetic state set in memory (no
// helper, no network): TWINNEL_WINSHOTS=<dir>, TWINNEL_WINSHOTS_SCALE
// (1.5), TWINNEL_WINSHOTS_GOOS (windows,darwin).
func TestWindowsPageStateShots(t *testing.T) {
	dir := os.Getenv("TWINNEL_WINSHOTS")
	if dir == "" {
		t.Skip("TWINNEL_WINSHOTS not set")
	}
	scale, err := strconv.ParseFloat(cmpOr(os.Getenv("TWINNEL_WINSHOTS_SCALE"), "1.5"), 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldT := plat, i18n.T
	defer func() { plat, i18n.T = old, oldT; resetFlags() }()
	i18n.Set("zh")
	var digests shotDigests
	for _, goos := range []string{"windows", "darwin"} {
		plat = platformFor(goos)
		resetFlags()
		for _, st := range pageStates() {
			tr := renderPageState(t, goos, st, float32(scale))
			var buf bytes.Buffer
			if err := png.Encode(&buf, tr.Image()); err != nil {
				t.Fatal(err)
			}
			name := map[string]string{"windows": "win", "darwin": "mac"}[goos] + "-" + st.name
			if err := os.WriteFile(filepath.Join(dir, name+".png"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			digests.add(name, buf.Bytes())
		}
	}
	if err := digests.err(); err != nil {
		t.Error(err)
	}
}

// renderPageState draws st on goos's path (plat set by the caller),
// dark, at 1100 × 900 DIPs and scale, from
// synthetic state (a connected helper that allows writes).
func renderPageState(t *testing.T, goos string, st pageState, scale float32) *ui.Tester {
	t.Helper()
	a, _ := layoutApp(t)
	a.probes = &shotProbes{goos: goos, machine: shotMachine(goos)}
	a.theme = "dark"
	snap := a.shotSnap
	a.shotSnap = func(s *status.Snapshot) {
		snap(s)
		s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
		s.WritesAllowed, s.Mihomo.Reachable, s.Loaded = true, true, true
	}
	a.hd.loaded, a.hd.connected = true, true
	a.hd.settings = &rpc.Settings{}
	st.set(a)
	a.page = st.page
	tr := ui.NewTesterWith(func(c *ui.Context) { a.mainView(c) }, 1100, 900, ui.TesterOptions{Dark: true})
	tr.SetTitleBar(titleBarFor(goos))
	tr.SetScale(scale)
	for range 4 {
		tr.Frame()
	}
	return tr
}

type pageState struct {
	name, page string
	set        func(a *app)
}

// pageStates are synthetic states of the pages: components not
// installed, downloading at a known percentage, failed; a chosen history
// entry with its diff; agent activity.
func pageStates() []pageState {
	day := time.Date(2026, 11, 22, 9, 0, 0, 0, time.Local)
	const dir = `/Users/example/.config/mihomo`
	missing := func() []rpc.AccessoryFile {
		var fs []rpc.AccessoryFile
		for _, n := range []string{"GeoIP.dat", "GeoSite.dat", "geoip.metadb"} {
			fs = append(fs, rpc.AccessoryFile{Name: n, File: dir + "/" + n, UpdateAvailable: true, RemoteSize: 9_000_000, Verifiable: true})
		}
		return fs
	}
	installed := func() []rpc.AccessoryFile {
		var fs []rpc.AccessoryFile
		for _, n := range []string{"GeoIP.dat", "GeoSite.dat", "geoip.metadb"} {
			fs = append(fs, rpc.AccessoryFile{Name: n, File: dir + "/" + n, Installed: true, Size: 9_000_000, ModTime: day, Version: "latest",
				Source: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/" + n, RemoteTag: "latest", UpdateAvailable: n == "GeoSite.dat", RemoteTime: day.Add(48 * time.Hour)})
		}
		return fs
	}
	uiMissing := &rpc.AccessoryCheck{Dir: dir, Files: []rpc.AccessoryFile{{Name: "ui", File: dir + "/ui", UpdateAvailable: true, RemoteTag: "v1.91.0", RemoteSize: 2_400_000}}}
	geo := func(fs []rpc.AccessoryFile) *rpc.AccessoryCheck {
		return &rpc.AccessoryCheck{Dir: dir, Files: fs, Checked: true, CheckedAt: day}
	}
	comp := func(name string, f func(a *app)) pageState {
		return pageState{name: "components-" + name, page: pageKernel, set: func(a *app) {
			a.shotSection = "components"
			a.hd.kern.geo.loading, a.hd.kern.ui.loading = true, true // hold off the status reads
			f(a)
		}}
	}
	return []pageState{
		comp("not-installed", func(a *app) {
			a.hd.kern.geo.check, a.hd.kern.ui.check = geo(missing()), uiMissing
		}),
		comp("newer", func(a *app) {
			a.hd.kern.geo.check, a.hd.kern.ui.check = geo(installed()), uiMissing
		}),
		comp("downloading", func(a *app) {
			a.hd.kern.geo.check, a.hd.kern.ui.check = geo(installed()), uiMissing
			a.hd.kern.geo.prog = &rpc.AccessoryProgress{Kind: "geo", Stage: rpc.AccStageDownload, Name: "geosite", Done: 4_000_000, Total: 10_000_000}
			a.hd.busy = geoKind().opUpdate()
			a.beginOp(a.hd.busy)
			a.hd.span.start = time.Now().Add(-time.Second)
		}),
		comp("failed", func(a *app) {
			raw := "geosite: download https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat: dial tcp 203.0.113.4:443: i/o timeout"
			a.hd.kern.geo.check, a.hd.kern.ui.check = geo(missing()), uiMissing
			a.hd.kern.geo.failErr, a.hd.kern.geo.failWhy = raw, failReason(rpc.Errorf(rpc.CodeFailed, "%s", raw))
		}),
		{name: "history", page: pageHistory, set: func(a *app) {
			for i, r := range []string{"切换到 TUN", "订阅更新", "启用覆写「广告拦截」", "接管前的配置"} {
				a.hd.history = append(a.hd.history, rpc.HistoryEntry{ID: "h" + strconv.Itoa(i), Time: day.Add(-time.Duration(i) * 7 * time.Hour),
					Hash: "9f2c1e0a7b3d", Size: 41_000 + i*120, Kind: "applied", Reason: r, BeforeTwinnel: i == 3})
			}
			a.hd.hist.sel = "h1"
			a.hd.hist.diffID = "h1"
			old := "mode: rule\ntun:\n  enable: false\n  stack: system\ndns:\n  enable: true\n"
			a.hd.hist.diff = newDiff(old, "mode: rule\ntun:\n  enable: true\n  stack: gvisor\ndns:\n  enable: true\n")
		}},
		{name: "settings-agent", page: pageSettings, set: func(a *app) {
			a.agent.actLoaded = true
			a.agent.actBusy = true // hold off agent.activity
			a.hd.layers = []rpc.Layer{{ID: "tun-stack", Name: "TUN stack gvisor"}}
			for i, x := range [][2]string{{"apply", "preview_hash=78834c0a reason=tun"}, {"layer_put", "content=<20 bytes> id=tun-stack"},
				{"select_node", "group=Proxy node=Example 1"}, {"set_mode", "mode=global"}} {
				a.agent.activity = append(a.agent.activity, rpc.AgentAction{Time: day.Add(time.Duration(i) * time.Minute), Client: "claude-code 2.1", Tool: x[0], Args: x[1], OK: i != 2})
			}
		}},
	}
}
