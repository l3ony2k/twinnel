package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// Before the takeover the helper's config path is Twinnel's default, which
// nothing runs: the pages show the detected one, or nothing.
func TestLiveConfigPath(t *testing.T) {
	own := "/Users/example/mh/conf/config.yaml"
	helperDefault := "/Library/Application Support/twinnel/mihomo/config.yaml"
	snap := func(taken bool) status.Snapshot {
		st := &rpc.Status{Mihomo: rpc.MihomoStatus{ConfigPath: helperDefault}}
		if taken {
			st.ActiveSubscription = "adopted"
		}
		return status.Snapshot{Helper: status.Helper{State: status.HelperConnected, Status: st}}
	}
	a := &app{}
	if got := a.liveConfigPath(snap(false)); got != "" {
		t.Fatalf("not detected, before takeover: %q, want none", got)
	}
	a.live = liveSetup{loaded: true, Config: own}
	if got := a.liveConfigPath(snap(false)); got != own {
		t.Fatalf("before takeover: %q, want %q", got, own)
	}
	if got := a.liveConfigPath(snap(true)); got != helperDefault {
		t.Fatalf("after takeover: %q, want the helper's %q", got, helperDefault)
	}
}
