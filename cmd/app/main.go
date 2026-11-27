// Command app is duet's menu bar app: a tray icon with the live speed, a
// flyout panel and a main window, all in MyGo's native UI. It only renders
// and wires; the logic lives in core/....
//
// Environment: DUET_CONTROLLER (default 127.0.0.1:9090), DUET_MIHOMO_SECRET,
// DUET_READ_ONLY=1 to keep every control that changes things off,
// DUET_SOCKET (the helper's socket, default /var/run/duet/helper.sock),
// DUET_CLI (the duet command line tool, for the helper install plan),
// DUET_LANG=en|zh-Hans to show one UI language for this run without saving
// it (screenshots; it overrides 设置 → 语言).
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/tailscale"
)

// app holds everything. Fields below "main thread" are read and written
// only on the main thread (views, tray callbacks, RunOnMain).
type app struct {
	cfgPath string
	cfg     status.Config
	install tailscale.Install

	mh        *mihomo.Client
	ts        *tailscale.Client
	mon       *status.Monitor
	healthSrc status.HealthSource // nil on platforms without checks
	health    *helperHealth       // the same source; nil with healthSrc

	// healthVisible is true while the main window shows the health page:
	// the checks then rerun every HealthRefresh.
	healthVisible atomic.Bool
	notes         *notifier

	hc *status.HelperConn // the root helper (lives across monitor restarts)

	stop  context.CancelFunc
	unsub func()

	// main thread
	tray      *mygo.Tray
	trayLook  trayLook // what the status item shows (trayspeed.go)
	trayShown bool     // trayLook has been set once
	panel     *mygo.Window
	win       *mygo.Window
	page      string
	actionErr string
	settings  settingsForm
	logs      logsView
	events    eventLog      // the helper's events, for 日志 → duet (eventlog.go)
	probes    machineProbes // reads of this computer (probes.go); fixtures in shot mode
	shotMode  bool          // DUET_SHOTS: the panel draws its own background
	// panelShotH is the content height shot mode's panel was last sized
	// to (fitShotPanel).
	panelShotH int
	// shotSnap overrides the snapshot of the flyout and the main window in
	// shot mode (shots_flyout.go, shots_infotips.go).
	shotSnap func(*status.Snapshot)
	// shotSection limits a long page to one section in shot mode (MyGo
	// captures only the top of a page); "" draws everything.
	shotSection string
	theme       string // DUET_THEME=light|dark forces a theme (development, shots)
	peers       ui.ListState
	groupsOpen  map[string]bool
	tileCols    map[string]int             // node tile columns per group, from the last layout
	shotNoPeers bool                       // shots: the devices card as if the tailnet had no other device
	tileFocus   tileRef                    // the node tile ↑/↓ moved the focus to, until it is built
	pageScroll  map[string]*ui.ScrollState // each page's scroll place; the toolbar's edge shows once scrolled
	healthOpen  map[string]bool            // check ID → expanded, once the user toggled it
	exitPick    string                     // exit node select value (peer name or ExitNodeNone)
	hd          helperData                 // what the helper pages show
	about       aboutView                  // the 关于 page (aboutpage.go)
	live        liveSetup                  // the running mihomo before takeover (livesetup.go)
	hm          helperMgmtView             // 设置 → 后台助手 (helpermgmt.go)
	creds       credsState                 // controller credentials from the helper
	toast       string                     // shown once by the next frame of the main window
	toastOp     bool                       // toast is an operation's outcome: it links to 日志 → duet (H7)
	toastErr    string                     // a failed operation's outcome, shown until dismissed (H7)
	pend        pendingState               // optimistic control values and their errors (pending.go)
	retry       busySpan                   // the unreachable states' 重试 press (states.go)
	loadingSeen map[string][2]time.Time    // loadingBox: first and last frame per key
	langShown   bool                       // applyLang ran once
	trayTip     string                     // the tray's tooltip as last set
	egress      egressState                // the on-demand egress test (statusui.go)
	egressProbe func(context.Context) *health.CheckReport
	evidenceAll map[string]bool // health check ID → all evidence lines shown
	side        sidebarState    // the sidebar's mode and width (sidebar.go)
	shotNav     struct {        // shot mode: force the sidebar's key/hover looks
		active *bool
		hover  string
	}
}

func main() {
	a, err := newApp(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	a.notes = newNotifier()
	a.theme = os.Getenv("DUET_THEME")
	initLang()
	mygo.App.SetName(i18n.T.AppName)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The flyout and the main window come and go; the tray keeps the app.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// newApp builds the app's state from the environment, before any window:
// the shot guard, the machine probes, the config and the Tailscale client.
// A shot run (DUET_SHOTS) that could reach the owner's machine stops here.
func newApp(getenv func(string) string) (*app, error) {
	if err := shotGuard(getenv); err != nil {
		return nil, err
	}
	probes, err := probesFor(getenv)
	if err != nil {
		return nil, err
	}
	a := &app{page: pageOverview, groupsOpen: map[string]bool{}, tileCols: map[string]int{}, pageScroll: map[string]*ui.ScrollState{}, healthOpen: map[string]bool{}}
	i18n.Namer = a.eventName // helper messages name subscriptions and overrides (R-unslop-21)
	a.probes = probes
	// Shot mode never takes the owner's focus: no tray, no notifications,
	// windows created hidden and captured from memory (Window.CapturePage
	// renders native content itself; it needs no pixels on screen).
	a.shotMode = getenv("DUET_SHOTS") != ""
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	a.cfgPath = filepath.Join(dir, "duet", "app.json")
	a.cfg, err = status.LoadConfig(a.cfgPath, getenv)
	if err != nil {
		log.Printf("config %s: %v (using defaults)", a.cfgPath, err)
		a.cfg, _ = status.LoadConfig("", getenv)
	}
	// Shots are read-only unless a run asks for writes against fakes
	// (DUET_READ_ONLY=0): an agent's screenshot run never changes the
	// owner's network.
	if a.shotMode && getenv(status.EnvReadOnly) != "0" {
		a.cfg.AllowWrites = false
	}
	if !a.cfg.AllowWrites {
		log.Printf("read-only (%s)", status.EnvReadOnly)
	}
	a.install = probes.tailscaleInstall()
	a.ts = tailscale.New(probes.tailscaleAPI(), a.cfg.AllowWrites)
	if f := getenv(tailscale.StatusFileEnv); f != "" { // development: shots use a synthetic tailnet
		log.Printf("tailscale: reading %s instead of tailscaled (%s)", f, tailscale.StatusFileEnv)
	}
	return a, nil
}

func (a *app) ready() {
	a.applyLang()
	mygo.App.OnLocaleChanged(func(string) { a.applyLang() }) // the system's languages changed
	if a.shotMode {
		a.startHelper()
		a.start()
		a.openMain()
		a.shots(os.Getenv("DUET_SHOTS"))
		return
	}
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID:              "duet",
		Icon:            trayIcon(status.TrayUnknown),
		IconIsTemplate:  true,
		MonospacedTitle: true,
		ToolTip:         i18n.T.AppName,
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(a.appMenu())
	}
	if runtime.GOOS == "linux" {
		a.tray.SetMenu(a.trayMenu())
	} else {
		a.tray.OnClick(a.togglePanel)
		a.tray.OnRightClick(func() {
			if a.panel != nil {
				a.panel.Close()
			}
			a.tray.SetMenu(a.trayMenu())
			defer a.tray.SetMenu(nil)
			a.tray.PopUpMenu()
		})
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.stop != nil {
			a.stop()
		}
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	a.startHelper()
	a.start()
	if dir := os.Getenv("DUET_TAKEOVER_FIXTURE"); dir != "" && os.Getenv("DUET_SHOTS") == "" { // development: a recorded plan (shots load it later)
		if err := a.loadTakeoverFixture(dir); err != nil {
			log.Printf("takeover fixture: %v", err)
		}
	}
	if os.Getenv("DUET_OPEN") != "" { // for development
		a.openMain()
		if os.Getenv("DUET_OPEN") == "panel" {
			a.togglePanel()
		}
	}
}

// start (re)builds the mihomo client and the monitor from a.cfg. Main thread.
func (a *app) start() {
	if a.stop != nil {
		a.unsub()
		a.stop()
	}
	cc := controllerConfig(a.cfg, a.creds.cur) // the helper's credentials win over cfg
	mh, err := mihomo.New(mihomo.Options{Address: cc.Controller, Secret: cc.Secret, AllowWrites: a.cfg.AllowWrites})
	if err != nil {
		log.Printf("mihomo: %v; using the default controller", err)
		mh, _ = mihomo.New(mihomo.Options{Secret: cc.Secret, AllowWrites: a.cfg.AllowWrites})
	}
	a.mh = mh
	hc := a.hc
	if a.shotMode {
		hc = nil // the scratch helper's checks read the real machine; shots use the fixture
	}
	a.health = wrapHealth(hc, newHealthSource(a.probes, cc))
	a.healthSrc = nil
	if a.health != nil {
		a.healthSrc = a.health
	}
	a.mon = status.NewMonitor(status.Options{
		Mihomo:        mh,
		Health:        a.healthSrc,
		Tailscale:     a.ts,
		Install:       a.install,
		PrimaryGroup:  a.cfg.PrimaryGroup,
		TestURL:       a.cfg.TestURL,
		WritesAllowed: a.cfg.AllowWrites,

		ExitNodeAccepted: a.cfg.ExitNodeAccepted,
	})
	a.egressProbe = newEgressProbe(a.probes, cc)
	if a.hc != nil {
		a.mon.SetHelper(a.hc.State())
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.stop = cancel
	mon := a.mon
	a.unsub = mon.Subscribe(func() { a.changed(mon) })
	mon.Run(ctx)
	if a.win != nil && a.page == pageLogs {
		mon.SetLogLevel(a.logs.levelName())
	}
	go a.healthTicker(ctx, mon)
}

// healthTicker reruns the health checks every HealthRefresh while the
// health page shows.
func (a *app) healthTicker(ctx context.Context, mon *status.Monitor) {
	t := time.NewTicker(HealthRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a.healthVisible.Load() {
				mon.RunHealth(ctx)
			}
		}
	}
}

// syncHealthVisible records whether the health page shows. Main thread.
func (a *app) syncHealthVisible() {
	a.healthVisible.Store(a.win != nil && a.page == pageHealth)
}

// changed runs on a monitor goroutine after every change.
func (a *app) changed(mon *status.Monitor) {
	s := mon.Snapshot()
	mygo.RunOnMain(func() {
		if mon != a.mon {
			return // replaced by start
		}
		a.syncTray(s)
		if tip := status.TrayToolTip(s); a.tray != nil && tip != a.trayTip {
			a.trayTip = tip
			a.tray.SetToolTip(tip)
		}
		a.syncExitNodeAcceptance(s)
		if a.panel != nil {
			a.panel.Invalidate()
		}
		if a.win != nil {
			a.win.Invalidate()
		}
		if !a.cfg.NotificationsOff && !a.shotMode {
			a.notes.check(s, a.openFor)
		}
	})
}

// openFor opens the main window at the page that explains an alert.
func (a *app) openFor(key string) {
	old := a.page
	switch {
	case strings.HasPrefix(key, "health/"):
		a.page = pageHealth
	case strings.HasPrefix(key, "ts-"):
		a.page = pageTailscale
	default:
		a.page = pageOverview
	}
	a.openMain()
	a.pageChanged(old)
}

// do runs a write action off the main thread, shows its error, refreshes.
func (a *app) do(fn func(ctx context.Context) error) {
	mon := a.mon
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := fn(ctx)
		mygo.RunOnMain(func() {
			a.actionErr = ""
			if err != nil {
				a.actionErr = helperError(err)
			}
			a.invalidate()
		})
		mon.Refresh()
	}()
}

func (a *app) invalidate() {
	if a.panel != nil {
		a.panel.Invalidate()
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}

// panelWidth is the flyout's width (it grows to 380 for long names).
const panelWidth = 340

func (a *app) togglePanel() {
	if a.panel != nil {
		a.panel.Close()
		return
	}
	if a.shotMode { // a hidden window of the flyout's width, captured from memory
		a.panel = mygo.NewWindow(mygo.WindowOptions{
			Title: i18n.T.AppName, Hidden: true, Width: panelWidth, Height: 620, // fitShotPanel then fits it to the content
			Content: ui.View(a.panelView),
		})
		a.panel.OnClosed(func() { a.panel, a.panelShotH = nil, 0 })
		return
	}
	a.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:       a.tray,
		Placement:  mygo.PlacementBottom,
		Gap:        4,
		Width:      panelWidth,
		Height:     420,
		FitContent: true,
		MinWidth:   panelWidth,
		MaxWidth:   380,
		MaxHeight:  720,
		Focusable:  true,
		Shadow:     true,
		Popover:    !a.shotMode,
		Content:    ui.View(a.panelView),
	})
	a.panel.OnClosed(func() { a.panel = nil })
}

func (a *app) openMain() {
	if a.panel != nil {
		a.panel.Close()
	}
	if a.win != nil {
		if a.shotMode {
			return
		}
		a.win.Show()
		a.win.Focus()
		mygo.App.Focus()
		a.syncHealthVisible()
		return
	}
	a.settings.load(a.cfg)
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         i18n.T.AppName,
		TitleBarStyle: plat.titleBar,
		Vibrancy:      a.sidebarMaterial(),
		Width:         a.winWidth(),
		Height:        a.winHeight(),
		MinWidth:      760,
		MinHeight:     480,
		StateKey:      a.stateKey(),
		Hidden:        a.shotMode, // never shown: captured from memory
		Content:       ui.View(a.mainView),
	})
	a.win.OnClosed(func() {
		a.win = nil
		a.mon.SetLogLevel("") // logs stream only while the window shows
		a.syncHealthVisible()
	})
	if a.page == pageLogs {
		a.mon.SetLogLevel(a.logs.levelName())
	}
	if a.page == pageHealth {
		a.runHealth()
	}
	a.syncHealthVisible()
	if !a.shotMode {
		mygo.App.Focus()
	}
}

// winWidth is 980, or DUET_SHOTS_WIDTH in shot mode (narrow/wide passes).
func (a *app) winWidth() int {
	if a.shotMode {
		if w, err := strconv.Atoi(os.Getenv("DUET_SHOTS_WIDTH")); err == nil && w >= 760 {
			return w
		}
	}
	return 980
}

func (a *app) winHeight() int {
	if a.shotMode {
		return 1500 // the shots capture the visible part only
	}
	return 660
}

// applyTheme sets the app's theme (platform.go) for the system's
// appearance, or DUET_THEME's when set. Main thread.
func (a *app) applyTheme(c *ui.Context) {
	dark := c.Theme().Dark
	switch a.theme {
	case "dark":
		dark = true
	case "light":
		dark = false
	}
	c.SetTheme(systemTheme(dark, c.Preferences()))
}

// flushToast shows the pending outcome message as a toast: a success
// that needs no answer goes away by itself (errors stay on the page).
func (a *app) flushToast(c *ui.Context) {
	if a.toast != "" {
		if a.toastOp {
			c.ToastAction(a.toast, i18n.T.EvShow, a.showDuetLog)
		} else {
			c.Toast(a.toast)
		}
		a.toast, a.toastOp = "", false
	}
	if a.toastErr != "" {
		c.AddToast(ui.Toast{ID: "op-failed", Title: a.toastErr, Type: "error", Timeout: -1, Action: i18n.T.EvShow, OnAction: a.showDuetLog})
		a.toastErr = ""
	}
}

// stateKey keeps the window's place and size across launches, except in
// shot mode, which wants its own size.
func (a *app) stateKey() string {
	if a.shotMode {
		return ""
	}
	return "main"
}

// sidebarMaterial is the window's material, none in shot mode: a capture
// cannot show what the system draws behind the window, so the sidebar
// draws an opaque stand-in.
func (a *app) sidebarMaterial() mygo.Vibrancy {
	if a.shotMode {
		return mygo.VibrancyNone
	}
	return plat.sidebarMat
}

// appMenu is the macOS menu bar's menus: the standard ones, and 显示
// (View) listing the pages with their ⌘1…⌘9 (the window takes the same
// keys itself, pageKeys, on every platform). As an accessory app duet
// shows no menu bar of its own, but the menu's key equivalents work while
// it is active, and the menus show if it ever runs as a regular app.
func (a *app) appMenu() *mygo.Menu {
	var pages []*mygo.MenuItem
	for i, sec := range navSections(0, 0, toneNeutral) {
		if i > 0 {
			pages = append(pages, mygo.Separator())
		}
		for _, it := range sec.items {
			item := &mygo.MenuItem{Label: it.label}
			switch {
			case it.digit > 0:
				item.Accelerator = "CmdOrCtrl+" + strconv.Itoa(it.digit)
			case it.id == pageSettings:
				item.Accelerator = "CmdOrCtrl+,"
			}
			id := it.id
			item.Click = func(*mygo.MenuItem, *mygo.Window) {
				a.openMain()
				a.showPage(id)
			}
			pages = append(pages, item)
		}
	}
	pages = append(pages, mygo.Separator(), &mygo.MenuItem{Label: i18n.T.SidebarToggle, Accelerator: "Ctrl+Cmd+S",
		Click: func(*mygo.MenuItem, *mygo.Window) { a.openMain(); a.toggleSidebar() }})
	pages = append(pages, &mygo.MenuItem{Role: mygo.RoleToggleFullScreen})
	// The app menu as the default one, but 关于 duet opens the 关于 page,
	// not the standard about panel.
	about := &mygo.MenuItem{Label: i18n.T.AboutApp, Click: func(*mygo.MenuItem, *mygo.Window) {
		a.openMain()
		a.showPage(pageAbout)
	}}
	appMenu := []*mygo.MenuItem{
		about,
		mygo.Separator(),
		{Role: mygo.RoleServices},
		mygo.Separator(),
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: i18n.T.AppName, Submenu: appMenu},
		{Role: mygo.RoleFileMenu},
		{Role: mygo.RoleEditMenu},
		{Label: i18n.T.ViewMenu, Submenu: pages},
		{Role: mygo.RoleWindowMenu},
	})
}
