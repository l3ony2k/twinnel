package main

import (
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

func TestHelperDownText(t *testing.T) {
	old := groupGapProbe
	t.Cleanup(func() { groupGapProbe, gapAt, gapSocket = old, time.Time{}, "" })
	i18n.Set("zh-Hans")
	gap, probes := groupGap{}, 0
	groupGapProbe = func(string) groupGap { probes++; return gap }
	a := &app{}
	denied := status.Snapshot{Helper: status.Helper{State: status.HelperDenied, Socket: "/var/run/twinnel/helper.sock"}}

	gapAt = time.Time{}
	if got := a.helperDownText(denied); got != i18n.T.HelperDownRow {
		t.Errorf("no gap: %q", got)
	}
	gap, gapAt = groupGap{group: "wheel"}, time.Time{}
	if got := a.helperDownText(denied); got != "后台助手 · 需要加入 wheel 组" {
		t.Errorf("missing: %q", got)
	}
	gap, gapAt = groupGap{group: "wheel", listed: true}, time.Time{}
	if got := a.helperDownText(denied); got != i18n.T.HelperGroupRowRelogin {
		t.Errorf("relogin: %q", got)
	}
	// Memoized for a few seconds: one probe for many frames.
	n := probes
	for i := 0; i < 10; i++ {
		a.helperDownText(denied)
	}
	if probes != n {
		t.Errorf("probed %d times in one frame burst", probes-n)
	}
	// Only a refused helper is probed; never in shot mode.
	notRunning := status.Snapshot{Helper: status.Helper{State: status.HelperNotRunning, Socket: denied.Helper.Socket}}
	if got := a.helperDownText(notRunning); got != i18n.T.HelperDownRow {
		t.Errorf("not running: %q", got)
	}
	gapAt = time.Time{}
	shot := &app{shotMode: true}
	if g := shot.helperGroupGap(denied.Helper); g.group != "" || probes != n {
		t.Errorf("shot mode probed the machine: %+v", g)
	}
}
