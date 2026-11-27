//go:build darwin

package main

import (
	"context"
	"os"
	"time"

	"github.com/l3ony2k/duet/core/takeover"
	"github.com/l3ony2k/duet/platform/darwin/takeoverprobe"
)

// probeLiveSetup finds the mihomo launchd job (daemons, and agents of this
// user) and its files, as takeover planning does. No service and no
// ~/.config/mihomo/config.yaml: nothing found.
func probeLiveSetup() liveSetup {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home, _ := os.UserHomeDir()
	svcs, _ := takeoverprobe.New(home).Services(ctx)
	svc := takeover.MihomoService(svcs)
	bin, _, cfg := takeover.MihomoFiles(svc, home)
	l := liveSetup{Binary: bin, Config: cfg}
	if svc != nil {
		l.Label, l.Plist = svc.Label, svc.Path
		return l
	}
	if _, err := os.Stat(cfg); err != nil {
		return liveSetup{}
	}
	return l
}
