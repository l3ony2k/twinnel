package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// derpInfoShots (TWINNEL_SHOTS_FLOW=derpinfo) captures the overview, the
// health page, the Tailscale page and the flyout with a synthetic health
// report in which tailnet peers are relayed: TWINNEL_RELAYED_REPORT names
// the directory the health test TestRelayedPeersNeverWarn wrote it to
// (report-<lang>.json). Nothing is sent or written.
func (a *app) derpInfoShots(save func(*mygo.Window, string)) {
	b, err := os.ReadFile(filepath.Join(os.Getenv("TWINNEL_RELAYED_REPORT"), "report-"+i18n.Tag()+".json"))
	if err != nil {
		log.Print(err)
		return
	}
	var rep health.Report
	if err := json.Unmarshal(b, &rep); err != nil {
		log.Print(err)
		return
	}
	rep.Started = time.Now().Add(-time.Minute)
	mygo.RunOnMain(func() {
		a.shotSnap = func(s *status.Snapshot) {
			s.Health = status.Health{Report: &rep, Updated: rep.Started}
			s.Loaded = true
			status.Evaluate(s)
		}
		a.win.SetSize(a.winWidth(), 1250)
	})
	for _, p := range []string{pageOverview, pageHealth, pageTailscale} {
		mygo.RunOnMain(func() {
			old := a.page
			a.page = p
			a.pageChanged(old)
			a.win.Invalidate()
		})
		time.Sleep(1500 * time.Millisecond)
		save(a.win, "main-"+p)
	}
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() { panel = a.panel })
	if panel != nil {
		save(panel, "panel")
	}
}
