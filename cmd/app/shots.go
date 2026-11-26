package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/status"
)

// shots saves PNGs of every page of the main window and of the panel into
// dir, then quits: DUET_SHOTS=<dir>. A development aid for checking the
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
		for _, p := range []string{pageOverview, pageProxies, pageTailscale, pageHealth, pageLogs, pageSettings} {
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
		}
		for _, l := range []status.Level{status.LevelOK, status.LevelWarn, status.LevelError} {
			if err := os.WriteFile(filepath.Join(dir, "tray-"+l.String()+".png"), trayIcon(l), 0o644); err != nil {
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
