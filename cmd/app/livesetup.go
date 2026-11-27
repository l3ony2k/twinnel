package main

import (
	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/status"
)

// liveSetup is the mihomo that runs on this machine before duet takes it
// over: its service, binary and config file, read the way a takeover plan
// reads them (the launchd job's -d/-f). Before the takeover the helper's
// own settings point at duet's defaults, which nothing runs; the pages
// show this instead (接管 → 现有安装, 关于, 内核).
type liveSetup struct {
	loaded  bool // a detection finished (found or not)
	loading bool
	Label   string // the service label, e.g. com.example.mihomo
	Plist   string
	Binary  string
	Config  string // "" when nothing was found
}

// found: a config file was detected.
func (l liveSetup) found() bool { return l.Config != "" }

// detectLiveSetup reads the running mihomo's setup in the background
// (read-only: launchd plists, plutil). Main thread. Shot runs never read
// the machine; they set a.live themselves.
func (a *app) detectLiveSetup() {
	if a.shotMode || a.live.loading {
		return
	}
	a.live.loading = true
	go func() {
		l := a.probes.liveSetup()
		mygo.RunOnMain(func() {
			l.loaded = true
			a.live = l
			a.invalidate()
		})
	}()
}

// liveConfigPath is the config file the running mihomo uses: the helper's
// once duet took over, else the detected one; "" when unknown.
func (a *app) liveConfigPath(s status.Snapshot) string {
	if s.TakenOver() {
		if st := s.Helper.Status; st != nil && st.Mihomo.ConfigPath != "" {
			return st.Mihomo.ConfigPath
		}
	}
	return a.live.Config
}
