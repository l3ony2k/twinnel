package health

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/binsane"
)

// Synthetic binaries: a path containing "damaged" is zeroed, "locked" is
// unreadable, anything else intact. No real file is read.
func init() {
	checkBinary = func(p, _ string, _ bool) binsane.Result {
		switch {
		case strings.Contains(p, "damaged"):
			return binsane.Result{Path: p, Problem: binsane.Zeroed}
		case strings.Contains(p, "locked"):
			return binsane.Result{Path: p, Problem: binsane.Denied}
		}
		return binsane.Result{Path: p}
	}
	reasonCases[ReasonBinMihomo] = reasonCase{BinaryIntegrity{}, func() Env {
		e := healthyEnv()
		e.settings.Binaries = []BinaryRef{{Name: "mihomo", Path: "/synthetic/damaged/mihomo"}}
		return e
	}}
	reasonCases[ReasonBinTailscale] = reasonCase{BinaryIntegrity{}, func() Env {
		e := healthyEnv()
		e.settings.Binaries = append(e.settings.Binaries, BinaryRef{Name: "tailscale", Path: `C:\synthetic\damaged\tailscale.exe`, Signed: true})
		return e
	}}
	reasonCases[ReasonAutostartOn] = reasonCase{CompetitorAutostart{}, func() Env {
		return autostartEnv(autostart.Entry{ID: `scheduled-task:\mihomo-party`, Kind: autostart.ScheduledTask, Name: `\mihomo-party`, Client: "Clash Party", Enabled: true, Disable: autostart.ByHelper})
	}}
	reasonCases[ReasonAutostartManual] = reasonCase{CompetitorAutostart{}, func() Env {
		return autostartEnv(autostart.Entry{ID: "login-item:ClashX Meta", Kind: autostart.LoginItem, Name: "ClashX Meta", Client: "ClashX", Enabled: true, Disable: autostart.Guided, Open: autostart.LoginItemsSettings})
	}}
}

func autostartEnv(es ...autostart.Entry) *fakeEnv {
	e := healthyEnv()
	e.settings.Autostarts = func(context.Context) ([]autostart.Entry, error) { return es, nil }
	return e
}

func TestCompetitorAutostart(t *testing.T) {
	// A disabled entry is listed, not a problem.
	r := want(t, CompetitorAutostart{}, autostartEnv(autostart.Entry{Kind: autostart.ScheduledTask, Name: `\mihomo-party-run`, Client: "Clash Party", Disable: autostart.ByHelper}), StatusOK, `\mihomo-party-run`)
	if len(r.Reasons) != 0 {
		t.Fatal(r.Reasons)
	}
	// Two entries the helper can turn off and a manual one: one auto fix
	// with both IDs, the command for the other.
	r = want(t, CompetitorAutostart{}, autostartEnv(
		autostart.Entry{ID: "a", Kind: autostart.ScheduledTask, Name: `\mihomo-party`, Client: "Clash Party", Enabled: true, Disable: autostart.ByHelper},
		autostart.Entry{ID: "b", Kind: autostart.RunKey, Name: "Clash Verge", Client: "Clash Verge", Enabled: true, Disable: autostart.ByHelper},
		autostart.Entry{ID: "c", Kind: autostart.SystemdUser, Name: "mihomo.service", Client: "mihomo", Enabled: true, Disable: autostart.Manual, Command: "systemctl --user disable --now mihomo.service"},
	), StatusWarn, "Clash Party, Clash Verge, mihomo")
	if r.Fix == nil || r.Fix.AutoFix != FixDisableAutostart || r.Fix.Param != "a,b" || !strings.Contains(r.Fix.Command, "systemctl --user") || len(r.Reasons) != 2 {
		t.Fatalf("%+v %v", r.Fix, r.Reasons)
	}
	// Not readable: says denied, not "none found".
	e := healthyEnv()
	e.settings.Autostarts = func(context.Context) ([]autostart.Entry, error) {
		return nil, &fs.PathError{Op: "open", Path: `C:\Windows\System32\Tasks`, Err: fs.ErrPermission}
	}
	want(t, CompetitorAutostart{}, e, StatusSkip, "没有权限")
	e.settings.Autostarts = nil
	want(t, CompetitorAutostart{}, e, StatusSkip, "")
}

func TestBinaryIntegrity(t *testing.T) {
	e := healthyEnv()
	e.settings.Binaries = []BinaryRef{{Name: "mihomo", Path: "/synthetic/locked/mihomo"}}
	want(t, BinaryIntegrity{}, e, StatusSkip, "没有权限")
	e.settings.Binaries = append(e.settings.Binaries, BinaryRef{Name: "tailscale", Path: "/synthetic/tailscale"})
	want(t, BinaryIntegrity{}, e, StatusOK, "没有权限读取（不是不存在")
	e.settings.Binaries = append(e.settings.Binaries, BinaryRef{Name: "mihomo", Path: "/synthetic/damaged/mihomo"})
	r := want(t, BinaryIntegrity{}, e, StatusFail, "内容全是 0")
	if r.Fix.AutoFix != FixRepairMihomo {
		t.Fatal(r.Fix)
	}
}
