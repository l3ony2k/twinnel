package main

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// Another program's mihomo on the controller (round-2 QA): status names
// it and does not show its version, mode or TUN as Twinnel's.
func TestStatusForeignController(t *testing.T) {
	owner := &foreign.Owner{Addr: "127.0.0.1:9090", Verdict: foreign.Foreign, PID: 4321,
		Exe: `C:\Tools\other\mihomo.exe`, Parent: `C:\Tools\other\launcher.exe`}
	s := status.Snapshot{Loaded: true,
		Helper: status.Helper{State: status.HelperConnected, Status: &rpc.Status{Mihomo: rpc.MihomoStatus{Owner: owner}}},
		Mihomo: status.Mihomo{Reachable: true, Version: "v9.9.9-other", Config: mihomo.Config{Mode: "global"}},
	}
	i18n.Set("en")
	defer i18n.Set("zh-Hans")
	status.Evaluate(&s)
	s = hideForeign(s)
	if s.Level != status.LevelError || len(s.Reasons) == 0 || s.Reasons[0].Code != status.ReasonCtlForeign {
		t.Fatalf("level %v reasons %+v", s.Level, s.Reasons)
	}
	txt := statusText(s)
	// The app's words (status.MihomoView: its state and facts), not the
	// CLI's own: the two cannot drift (Windows QA round 3: the CLI said
	// "another mihomo" twice).
	v := s.MihomoView()
	want := []string{v.Text(), "twinnel doctor --fix controller-owner"}
	for _, f := range v.Facts() {
		want = append(want, f.Label+": "+f.Value)
	}
	if len(v.Facts()) != 3 {
		t.Errorf("facts %+v", v.Facts())
	}
	for _, w := range want {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q in\n%s", w, txt)
		}
	}
	if n := strings.Count(strings.ToLower(txt), "another mihomo"); n != 2 { // the reason and the state
		t.Errorf("\"another mihomo\" %d times:\n%s", n, txt)
	}
	for _, bad := range []string{"v9.9.9-other", "global", "tun "} {
		if strings.Contains(txt, bad) {
			t.Errorf("shows the other mihomo's %q as Twinnel's:\n%s", bad, txt)
		}
	}
}

// The helper's DNS watchdog holding the system DNS back is a reason, as in
// the app.
func TestStatusDNSRestored(t *testing.T) {
	s := status.Snapshot{Loaded: true,
		Helper: status.Helper{State: status.HelperConnected, Status: &rpc.Status{DNSWatch: &rpc.DNSWatch{Phase: rpc.DNSWatchRestored}}},
		Mihomo: status.Mihomo{Reachable: true, Config: mihomo.Config{Tun: mihomo.Tun{Enable: true}}},
	}
	status.Evaluate(&s)
	found := false
	for _, r := range s.Reasons {
		found = found || r.Code == status.ReasonDNSRestored
	}
	if !found {
		t.Fatalf("reasons %+v", s.Reasons)
	}
}
