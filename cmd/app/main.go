// Command app is duet's menu bar app: a tray icon with the live speed, a
// flyout panel and a main window, all in MyGo's native UI. It only renders
// and wires; the logic lives in core/....
//
// Environment: DUET_CONTROLLER (default 127.0.0.1:9090), DUET_MIHOMO_SECRET,
// DUET_ALLOW_WRITES=1 to enable the controls that change things.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

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

	mh  *mihomo.Client
	ts  *tailscale.Client
	mon *status.Monitor

	stop  context.CancelFunc
	unsub func()

	// main thread
	tray       *mygo.Tray
	trayTitle  string
	trayLevel  status.Level
	panel      *mygo.Window
	win        *mygo.Window
	page       string
	actionErr  string
	settings   settingsForm
	logs       logsView
	shotMode   bool // DUET_SHOTS: the panel draws its own background
	peers      ui.ListState
	groupsOpen map[string]bool
}

func main() {
	a := &app{page: pageOverview, groupsOpen: map[string]bool{}}
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	a.cfgPath = filepath.Join(dir, "duet", "app.json")
	a.cfg, err = status.LoadConfig(a.cfgPath, os.Getenv)
	if err != nil {
		log.Printf("config %s: %v (using defaults)", a.cfgPath, err)
		a.cfg, _ = status.LoadConfig("", os.Getenv)
	}
	a.install = tailscale.DetectInstall(runtime.GOOS, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
	a.ts = tailscale.New(tailscale.LocalAPI(nil), a.cfg.AllowWrites)

	mygo.App.SetName(i18n.T.AppName)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The flyout and the main window come and go; the tray keeps the app.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) ready() {
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID:              "duet",
		Icon:            trayIcon(status.LevelUnknown),
		IconIsTemplate:  true,
		MonospacedTitle: true,
		ToolTip:         i18n.T.AppName,
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: i18n.T.OpenMainWindow, Click: func(*mygo.MenuItem, *mygo.Window) { a.openMain() }},
		mygo.Separator(),
		{Label: i18n.T.Quit, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	if runtime.GOOS == "linux" {
		a.tray.SetMenu(menu)
	} else {
		a.tray.OnClick(a.togglePanel)
		a.tray.OnRightClick(func() {
			if a.panel != nil {
				a.panel.Close()
			}
			a.tray.SetMenu(menu)
			defer a.tray.SetMenu(nil)
			a.tray.PopUpMenu()
		})
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.stop != nil {
			a.stop()
		}
		a.tray.Destroy()
	})
	a.start()
	if dir := os.Getenv("DUET_SHOTS"); dir != "" {
		a.shotMode = true
		a.openMain()
		a.shots(dir)
		return
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
	mh, err := mihomo.New(mihomo.Options{Address: a.cfg.Controller, Secret: a.cfg.Secret, AllowWrites: a.cfg.AllowWrites})
	if err != nil {
		log.Printf("mihomo: %v; using the default controller", err)
		mh, _ = mihomo.New(mihomo.Options{Secret: a.cfg.Secret, AllowWrites: a.cfg.AllowWrites})
	}
	a.mh = mh
	a.mon = status.NewMonitor(status.Options{
		Mihomo:        mh,
		Tailscale:     a.ts,
		Install:       a.install,
		PrimaryGroup:  a.cfg.PrimaryGroup,
		TestURL:       a.cfg.TestURL,
		WritesAllowed: a.cfg.AllowWrites,
	})
	ctx, cancel := context.WithCancel(context.Background())
	a.stop = cancel
	mon := a.mon
	a.unsub = mon.Subscribe(func() { a.changed(mon) })
	mon.Run(ctx)
	if a.win != nil && a.page == pageLogs {
		mon.SetLogLevel(a.logs.levelName())
	}
}

// changed runs on a monitor goroutine after every change.
func (a *app) changed(mon *status.Monitor) {
	s := mon.Snapshot()
	title := ""
	if s.Mihomo.Reachable {
		title = status.TrayTitle(s.Mihomo.Traffic)
	}
	mygo.RunOnMain(func() {
		if mon != a.mon {
			return // replaced by start
		}
		if title != a.trayTitle {
			a.trayTitle = title
			a.tray.SetTitle(title)
		}
		if s.Level != a.trayLevel {
			a.trayLevel = s.Level
			a.tray.SetIcon(trayIcon(s.Level), true)
			a.tray.SetToolTip(i18n.T.AppName + " · " + s.Level.Text())
		}
		if a.panel != nil {
			a.panel.Invalidate()
		}
		if a.win != nil {
			a.win.Invalidate()
		}
	})
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
				a.actionErr = i18n.T.ErrorPrefix + status.ShortError(err)
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

func (a *app) togglePanel() {
	if a.panel != nil {
		a.panel.Close()
		return
	}
	a.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:       a.tray,
		Placement:  mygo.PlacementBottom,
		Gap:        4,
		Width:      340,
		Height:     420,
		FitContent: true,
		MinWidth:   340,
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
		a.win.Show()
		a.win.Focus()
		mygo.App.Focus()
		return
	}
	a.settings.load(a.cfg)
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     i18n.T.AppName,
		Width:     980,
		Height:    660,
		MinWidth:  760,
		MinHeight: 480,
		StateKey:  "main",
		Content:   ui.View(a.mainView),
	})
	a.win.OnClosed(func() {
		a.win = nil
		a.mon.SetLogLevel("") // logs stream only while the window shows
	})
	if a.page == pageLogs {
		a.mon.SetLogLevel(a.logs.levelName())
	}
	mygo.App.Focus()
}
