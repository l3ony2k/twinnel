package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/uninstall"
	"github.com/l3ony2k/duet/core/version"
)

func hardenedPlan(purge bool) *uninstall.Plan {
	p := &uninstall.Plan{Known: true, Restore: &uninstall.Restore{FromLabel: "com.l3ony2k.duet.mihomo", ToLabel: "com.mihomo.proxy"}}
	p.Remove = []uninstall.Item{{Kind: uninstall.HelperService}, {Kind: uninstall.HelperBinary}, {Kind: uninstall.HelperSocket}, {Kind: uninstall.DuetMihomo}}
	data := []uninstall.Item{{Kind: uninstall.StateDir, Subscriptions: 1, Layers: 3}, {Kind: uninstall.Secrets}}
	p.Keep = []uninstall.Item{{Kind: uninstall.OriginalMihomo}}
	if purge {
		p.Remove = append(p.Remove, data...)
	} else {
		p.Keep = append(p.Keep, data...)
	}
	p.Keep = append(p.Keep, uninstall.Item{Kind: uninstall.Tailscale}, uninstall.Item{Kind: uninstall.App})
	return p
}

func TestPlanText(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // the real ~/Library/Application Support/duet/backups must not leak in
	got := planText(hardenedPlan(false), false)
	want := "移除：后台助手、duet 的 mihomo 服务（切回 com.mihomo.proxy，断网几秒）\n" +
		"保留：mihomo 原来的服务、数据（1 个订阅、3 个覆写、历史、日志）、钥匙串项目、Tailscale、duet.app"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got = planText(hardenedPlan(true), true)
	if !strings.HasPrefix(got, "移除：后台助手、duet 的 mihomo 服务（切回 com.mihomo.proxy，断网几秒）、数据（1 个订阅、3 个覆写、历史、日志）、钥匙串项目、登录时打开、duet.app（移到废纸篓）\n保留：mihomo 原来的服务、Tailscale") {
		t.Errorf("remove all:\n%s", got)
	}
	if strings.Contains(got, "保留：mihomo 原来的服务、Tailscale、duet.app") {
		t.Error("remove all keeps the app")
	}
}

func TestUninstallFlags(t *testing.T) {
	if got := uninstallFlags(hardenedPlan(true), true); !slices.Equal(got, []string{"helper", "uninstall", "--yes", "--purge", "--restore-mihomo"}) {
		t.Errorf("got %v", got)
	}
	if got := uninstallFlags(&uninstall.Plan{}, false); !slices.Equal(got, []string{"helper", "uninstall", "--yes"}) {
		t.Errorf("got %v", got)
	}
}

func TestRefusalText(t *testing.T) {
	if got := refusalText(&uninstall.Refusal{Code: uninstall.OwnMihomo, Message: "x"}); !strings.Contains(got, "「内核」") {
		t.Error(got)
	}
	if got := refusalText(&uninstall.Refusal{Code: uninstall.Conflict, Message: "boom"}); got != "无法卸载：boom" {
		t.Error(got)
	}
}

func TestCLIFailure(t *testing.T) {
	out := "# duet helper upgrade will run these steps as root:\n1. x\nduet: helper upgrade did not complete (cancelled)\n"
	if got := cliFailure(out, errors.New("exit status 1")); got != "helper upgrade did not complete (cancelled)" {
		t.Errorf("got %q", got)
	}
	if got := cliFailure("", errors.New("exit status 2")); got == "" {
		t.Error("empty")
	}
}

func TestHelperMatch(t *testing.T) {
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	snap := func(v string) status.Snapshot {
		return status.Snapshot{Helper: status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: v}}}
	}
	version.Version = "0.1.0-dev"
	if helperOutdated(snap("0.0.1")) {
		t.Error("development app shows a version state")
	}
	version.Version = "0.3.0"
	if !helperOutdated(snap("0.2.0")) || helperOutdated(snap("0.3.0")) || helperOutdated(snap("0.4.0")) {
		t.Error("outdated")
	}
	if helperOutdated(status.Snapshot{Helper: status.Helper{State: status.HelperNotRunning}}) {
		t.Error("not connected")
	}
}

func TestCanUpdateHelper(t *testing.T) {
	a := &app{}
	for action, want := range map[string]bool{"upgrade": true, "restart": true, "none": false, "downgrade": false, "not-installed": false, "": false} {
		a.hm.check = &upgradeCheck{Action: action}
		if a.canUpdateHelper() != want {
			t.Errorf("%q: %v", action, !want)
		}
	}
}
