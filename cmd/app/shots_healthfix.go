package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// healthFixShots (TWINNEL_SHOTS_FLOW=healthfix) shows the health fixes on
// a synthetic report, worded for macOS, Windows and Linux in turn: the
// failing rows with their reasons and actions, a manual fix opened (为什么
// 无法自动修复), the 修复 preview, the row after the fix with 撤销修复, a
// guided fix's 去调整 landing on 节点, and the flyout's 修复 row. The
// report and the helper are in memory (shotSnap, fx.shotHelper): nothing
// is read from or written to the machine or a helper.
func (a *app) healthFixShots(save func(*mygo.Window, string)) {
	fake := &shotFixHelper{}
	var platform string
	fixed := false
	mygo.RunOnMain(func() {
		a.fx.shotHelper = fake
		a.shotSnap = func(s *status.Snapshot) {
			s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0"}}
			s.WritesAllowed = true
			s.Health = status.Health{Report: shotFixReport(platform, fixed), Updated: time.Now().Add(-time.Minute)}
			s.Loaded = true
			status.Evaluate(s)
		}
	})
	step := func(name string, wait time.Duration, f func()) {
		mygo.RunOnMain(func() {
			f()
			a.win.Invalidate()
		})
		time.Sleep(wait)
		save(a.win, name)
	}
	for _, p := range health.AllPlatforms {
		platform, fixed = p, false
		step("healthfix-"+p+"-problems", time.Second, func() {
			a.toast, a.toastErr = "", ""
			a.healthOpen = map[string]bool{}
			a.fx = fixState{shotHelper: fake}
			a.openPage(pageHealth)
		})
		step("healthfix-"+p+"-why", 600*time.Millisecond, func() {
			a.fx.why = map[string]bool{"vpn-conflict|" + health.ReasonVPNOther: true}
		})
		// The preview of the system DNS fix (manual on Linux: no sheet).
		if p != health.Linux {
			step("healthfix-"+p+"-preview", 1200*time.Millisecond, func() {
				a.fx.why = nil
				if st := a.pageScroll[pageHealth]; st != nil {
					st.Y = 0
				}
				s := a.mon.Snapshot()
				a.shotSnap(&s)
				for _, cr := range s.Health.Report.Checks {
					if cr.ID == "system-dns" {
						a.previewFix(cr, health.ReasonDNSNotMihomo, s)
					}
				}
			})
			step("healthfix-"+p+"-fixed", 1200*time.Millisecond, func() {
				s := a.mon.Snapshot()
				a.shotSnap(&s)
				fixed = true
				a.runFix(s)
				a.healthOpen["system-dns"] = true
			})
		}
	}
	platform, fixed = health.Darwin, false
	step("healthfix-guided", 1200*time.Millisecond, func() {
		a.fx = fixState{shotHelper: fake}
		a.toast = ""
		a.goFix(rowFixes(health.CheckReport{Result: health.Result{Reasons: []string{health.ReasonLeakNoGroup}}}, health.Darwin)[0], "")
	})
	mygo.RunOnMain(a.togglePanel)
	time.Sleep(1500 * time.Millisecond)
	var panel *mygo.Window
	mygo.RunOnMain(func() {
		panel = a.panel
		if panel != nil {
			panel.CapturePage() // lay out and fit once
		}
	})
	time.Sleep(600 * time.Millisecond)
	if panel != nil {
		save(panel, "healthfix-flyout")
	}
}

// shotFixReport is a synthetic report: three problems with a one-click,
// a guided and a manual fix, and the rest passing. fixed: system DNS was
// fixed.
func shotFixReport(platform string, fixed bool) *health.Report {
	tag := i18n.Tag()
	cr := func(id string, st health.Status, summary string, fix *health.FixHint, reasons ...string) health.CheckReport {
		return health.CheckReport{ID: id, Title: health.TitleOf(tag, id), Result: health.Result{Status: st, Summary: summary,
			Fix: fix, Reasons: reasons}}
	}
	en := tag == "en"
	say := func(zh, e string) string {
		if en {
			return e
		}
		return zh
	}
	dns := cr("system-dns", health.StatusFail, say("这些网络服务的 DNS 没有指向 mihomo：Wi-Fi", "These network services do not use mihomo for DNS: Wi-Fi"), &health.FixHint{Command: "nmcli connection modify <name> ipv4.dns 127.0.0.1"}, health.ReasonDNSNotMihomo)
	if fixed {
		dns = cr("system-dns", health.StatusOK, say("所有活动网络服务的 DNS 都指向 mihomo（127.0.0.1:53）", "Every active network service uses mihomo for DNS (127.0.0.1:53)"), nil)
	}
	r := &health.Report{Platform: platform, Started: time.Now().Add(-time.Minute), Duration: 1500 * time.Millisecond, Lang: tag}
	r.Checks = []health.CheckReport{
		dns,
		cr("vpn-conflict", health.StatusFail, say("Example VPN 已连接", "Example VPN is connected"), nil, health.ReasonVPNOther),
		cr("fake-ip-filter-mode", health.StatusWarn, say("fake-ip-filter 里有 '*'，但 fake-ip-filter-mode 是 whitelist", "fake-ip-filter has '*', but fake-ip-filter-mode is whitelist"), nil, health.ReasonFakeIPWildcard),
		cr("controller-security", health.StatusWarn, say("external-controller 没有设置密钥，本机任何程序都能控制 mihomo", "external-controller has no secret: any program on this computer can control mihomo"), &health.FixHint{Param: "9090"}, health.ReasonCtlNoSecret),
	}
	for _, id := range []string{"tailnet-route", "tun-route-exclude", "tailscale-direct-rules", "magicdns", "peer-short-names", "leak-guard"} {
		r.Checks = append(r.Checks, cr(id, health.StatusOK, "", nil))
	}
	r.Overall = health.StatusOK
	for _, c := range r.Checks {
		r.Overall = health.Worse(r.Overall, c.Status)
	}
	return r
}

// shotFixHelper answers the system DNS fix with synthetic data.
type shotFixHelper struct{ pointed bool }

func (h *shotFixHelper) Call(_ context.Context, method string, _, result any) error {
	var v any
	switch method {
	case rpc.MethodDNSStatus:
		v = rpc.DNSStatus{Supported: true, Target: []string{"127.0.0.1"}, Services: []rpc.DNSService{{Name: "Wi-Fi", Servers: []string{"192.0.2.53"}}}}
	case rpc.MethodDNSPoint:
		h.pointed = true
		v = rpc.DNSStatus{Supported: true}
	case rpc.MethodHealthRun:
		b, _ := json.Marshal(shotFixReport(health.Darwin, h.pointed))
		v = rpc.HealthReport{Report: b}
	case rpc.MethodSettingsGet:
		v = rpc.Settings{}
	default:
		return nil
	}
	if result == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, result)
}
