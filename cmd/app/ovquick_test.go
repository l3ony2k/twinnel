package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// overviewTester draws the whole window on 总览 at width on goos's
// platform, in lang.
func overviewTester(t *testing.T, goos, lang string, width int) (*app, *ui.Tester) {
	t.Helper()
	t.Cleanup(resetFlags) // after usePlat's cleanup (LIFO): from the platform restored
	usePlat(t, goos)
	resetFlags()
	i18n.Set(lang)
	t.Cleanup(func() { i18n.Set("zh") })
	a, _ := layoutApp(t)
	a.hc = nil
	tr := ui.NewTester(func(c *ui.Context) { a.mainView(c) }, width, 900)
	tr.SetTitleBar(titleBarFor(goos))
	for range 3 {
		tr.Frame()
	}
	return a, tr
}

// N3 (Windows QA round 5): at the window's minimum width (760) the
// CommandBar stays one line inside the pane: the commands that do not
// fit fold, last first, into "…", above its own items.
func TestOverviewCommandBarFolds(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		a, tr := overviewTester(t, "windows", lang, 760)
		for range 3 {
			tr.Frame()
		}
		T := i18n.T
		first, ok1 := tr.Access(T.TestAllDelay)
		more, ok2 := tr.Access(T.OvMoreActions)
		if !ok1 || !ok2 {
			t.Fatalf("%s: commands missing (%v %v)", lang, ok1, ok2)
		}
		if d := more.Bounds.Y - first.Bounds.Y; d > 1 || d < -1 {
			t.Errorf("%s: … wrapped (y %.0f vs %.0f)", lang, more.Bounds.Y, first.Bounds.Y)
		}
		if right := more.Bounds.X + more.Bounds.W; right > a.paneBox.X+a.paneBox.W {
			t.Errorf("%s: … ends at %.0f, past the pane %+v", lang, right, a.paneBox)
		}
		if _, ok := tr.AccessByRole(ui.RoleMenuButton, T.Recovery); ok {
			t.Errorf("%s: 应急恢复 still a button at 760", lang)
		}
		if err := tr.Click(T.OvMoreActions); err != nil {
			t.Fatal(err)
		}
		menu := tr.Menu()
		tr.CloseMenu()
		if len(menu) < 3 || menu[len(menu)-2] != T.ReloadConfig || menu[len(menu)-1] != T.RestartMihomo || !slices.Contains(menu, T.Recovery) {
			t.Errorf("%s: overflow menu %q", lang, menu)
		}
	}
}

// 4.1: on Windows the quick actions are a CommandBar: they fit one line
// at the window's default width (965) in every language, 重载配置 and
// 重启 mihomo under "…", and "…" as tall as its neighbours.
func TestOverviewCommandBar(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		_, tr := overviewTester(t, "windows", lang, 965)
		T := i18n.T
		first, ok := tr.Access(T.TestAllDelay)
		if !ok {
			t.Fatalf("%s: no %q", lang, T.TestAllDelay)
		}
		more, ok := tr.Access(T.OvMoreActions)
		if !ok {
			t.Fatalf("%s: no overflow", lang)
		}
		for _, name := range []string{T.OvCopyProxyEnv, T.OvDashboard, T.Recovery, T.OvMoreActions} {
			r, ok := tr.Access(name)
			if !ok {
				t.Errorf("%s: no %q", lang, name)
				continue
			}
			if d := r.Bounds.Y - first.Bounds.Y; d > 1 || d < -1 {
				t.Errorf("%s: %q wrapped to another line (y %.0f vs %.0f)", lang, name, r.Bounds.Y, first.Bounds.Y)
			}
		}
		if d := more.Bounds.H - first.Bounds.H; d > 1 || d < -1 {
			t.Errorf("%s: … is %.1f tall, its neighbours %.1f", lang, more.Bounds.H, first.Bounds.H)
		}
		for _, name := range []string{T.ReloadConfig, T.RestartMihomo} {
			if r, ok := tr.AccessByRole(ui.RoleButton, name); ok {
				t.Errorf("%s: %q still a button at %+v", lang, name, r.Bounds)
			}
		}
		if err := tr.Click(T.OvMoreActions); err != nil {
			t.Fatal(err)
		}
		menu := tr.Menu()
		tr.CloseMenu()
		if len(menu) != 2 || menu[0] != T.ReloadConfig || menu[1] != T.RestartMihomo {
			t.Errorf("%s: overflow menu %q", lang, menu)
		}
	}
	// macOS keeps its row of buttons.
	_, tr := overviewTester(t, "darwin", "zh", 1280)
	if _, ok := tr.AccessByRole(ui.RoleButton, i18n.T.ReloadConfig); !ok {
		t.Error("macOS: 重载配置 is no longer a button")
	}
}

// 4.3: a card's plain key–value lines are 32 tall on Windows (a line
// every 40 with the rows' gap), rows holding a control keep the row
// height; macOS keeps its rhythm.
func TestOverviewLineHeights(t *testing.T) {
	for goos, pitch := range map[string]float32{"windows": 32 + gapRow, "darwin": 24 + gapRow} {
		_, tr := overviewTester(t, goos, "zh", 1280)
		T := i18n.T
		// mihomo's card: 进程 then 运行时长, both plain lines.
		var ys []float32
		for _, e := range tr.AccessAll() {
			if e.Name == T.OvUptime || e.Name == T.OvProcess {
				ys = append(ys, e.Bounds.Y)
			}
		}
		if len(ys) < 2 {
			t.Fatalf("%s: lines %v", goos, ys)
		}
		if d := ys[1] - ys[0]; d < pitch-1 || d > pitch+1 {
			t.Errorf("%s: plain lines every %.1f, want %v", goos, d, pitch)
		}
		if got := ovLineH(); goos == "windows" && got >= plat.rowMinHeight {
			t.Errorf("windows: plain line %v not under the control row %v", got, plat.rowMinHeight)
		}
	}
}

// 4.4: an empty 最近活动 centers its line (and a muted icon) in the card,
// not a bare line at its top left.
func TestOverviewRecentEmpty(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		a, tr := overviewTester(t, goos, "zh", 1280)
		a.events.evs = nil
		tr.SetSize(1280, 1600) // the whole page in the window
		tr.Frame()
		tr.Frame()
		T := i18n.T
		box, ok := tr.AccessByRole(ui.RoleStatus, T.EventsEmpty)
		if !ok {
			t.Fatalf("%s: no empty state", goos)
		}
		line, _ := tr.Find(T.EventsEmpty)
		head, _ := tr.Find(T.OvRecent)
		// Centered across the card (its heading starts at the card's left).
		cardMid := box.Bounds.X + box.Bounds.W/2
		if d := line.X + line.W/2 - cardMid; d > 2 || d < -2 {
			t.Errorf("%s: line centered at %.0f, the card at %.0f", goos, line.X+line.W/2, cardMid)
		}
		if line.X-head.X < 40 {
			t.Errorf("%s: line at the card's left (%.0f vs heading %.0f)", goos, line.X, head.X)
		}
		if line.Y-box.Bounds.Y < 20 {
			t.Errorf("%s: line at the card's top", goos)
		}
	}
}

// 4.2/7.2: the 出口 tile is a labelled group, not a button: the buttons
// in it (检测出口 IP) are themselves for the keyboard and assistive
// technology; on Windows they are as tall as every other button.
func TestExitTileButtons(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		a, tr := overviewTester(t, goos, "zh", 1280)
		a.egress = egressState{}
		tr.Frame()
		tr.Frame()
		T := i18n.T
		for _, e := range tr.AccessAll() {
			if e.Role == ui.RoleButton && len(e.Name) >= len(T.OvTileExit) && e.Name[:len(T.OvTileExit)] == T.OvTileExit {
				t.Errorf("%s: the exit tile is a button: %q", goos, e.Name)
			}
		}
		check, ok := tr.AccessByRole(ui.RoleButton, T.OvEgressCheck)
		if !ok {
			t.Fatalf("%s: %q not exposed as a button", goos, T.OvEgressCheck)
		}
		if !check.Focusable {
			t.Errorf("%s: %q not focusable", goos, T.OvEgressCheck)
		}
		std, _ := tr.AccessByRole(ui.RoleButton, T.TestAllDelay)
		if goos == "windows" {
			if d := check.Bounds.H - std.Bounds.H; d > 1 || d < -1 {
				t.Errorf("windows: %q %.1f tall, buttons %.1f", T.OvEgressCheck, check.Bounds.H, std.Bounds.H)
			}
		} else if check.Bounds.H != 22 {
			t.Errorf("macOS: %q %.1f tall, want the small 22", T.OvEgressCheck, check.Bounds.H)
		}
	}
}

// 4.5/7.3: the traffic chart (a Tab stop once it has samples) is named
// 流量 with the rates now, and no Tab stop of the overview is unnamed.
func TestTrafficChartNamed(t *testing.T) {
	_, tr := overviewTester(t, "windows", "zh", 1280)
	T := i18n.T
	want := T.Traffic + " · " + T.Download + " "
	found := false
	for _, e := range tr.AccessAll() {
		if e.Focusable && e.Name == "" {
			t.Errorf("an unnamed Tab stop: %v at %+v", e.Role, e.Bounds)
		}
		if strings.HasPrefix(e.Name, want) && strings.Contains(e.Name, "KB/s") {
			found = true
		}
	}
	if !found {
		t.Errorf("no chart named %q…", want)
	}
}
