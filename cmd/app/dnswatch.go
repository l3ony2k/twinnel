package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/version"
)

// The DNS watchdog's state in the app: while the helper holds the system
// DNS back on the earlier settings because mihomo does not answer
// (status.ReasonDNSRestored), the banner offers 重启 mihomo, 查看日志 and
// 保持原设置; 应急恢复 has 恢复系统 DNS and the manual steps.

// recoveryDocURL is docs/recovery.md: restoring the DNS by hand, per
// system (works without Twinnel; the page opens in the browser, which
// needs the network, so it is also in the repo and the release archive).
func recoveryDocURL() string { return version.RepoURL() + "/blob/main/docs/recovery.md" }

// dnsOwned: Twinnel pointed the system DNS (a backup exists), so 恢复系统
// DNS has something to write back.
func dnsOwned(s status.Snapshot) bool {
	st := s.Helper.Status
	return st != nil && st.DNSWatch != nil && st.DNSWatch.Phase != rpc.DNSWatchOff
}

// restoreSystemDNS writes the earlier DNS back for good (dns.restore):
// 保持原设置 and 应急恢复 → 恢复系统 DNS.
func (a *app) restoreSystemDNS(label string) {
	a.helperOp(label, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodDNSRestore, rpc.Empty{}, nil)
	}, func(error) { a.mon.Refresh() })
}

// dnsRestoredActs are the banner's actions for ReasonDNSRestored: bring
// mihomo back (the DNS follows by itself), see why, or stay on the
// earlier DNS.
func (a *app) dnsRestoredActs(s status.Snapshot) []bannerAct {
	if !s.DNSRestored() {
		return nil
	}
	T := i18n.T
	var acts []bannerAct
	if a.canRestartMihomo(s) {
		acts = append(acts, bannerAct{key: "dns-restart", label: T.RestartMihomo,
			run: func() { a.mihomoService(rpc.MethodServiceRestart, T.RestartMihomo) }})
	}
	acts = append(acts, bannerAct{key: "dns-log", label: T.HelperViewLog, run: a.showTwinnelLog})
	if s.Can().Helper && a.hd.busy == "" {
		acts = append(acts, bannerAct{key: "dns-keep", label: T.DNSKeepRestored,
			run: func() { a.restoreSystemDNS(T.DNSKeepRestored) }})
	}
	return acts
}

// dnsRecoveryItems are 应急恢复's DNS items: write the earlier DNS back
// now (while Twinnel owns it), and the manual steps for when nothing else
// works.
func (a *app) dnsRecoveryItems(m *ui.Menu, s status.Snapshot) {
	T := i18n.T
	if dnsOwned(s) && m.Item(T.RecoveryRestoreDNS).Disabled(!s.Can().Helper || a.hd.busy != "").Chosen() {
		a.restoreSystemDNS(T.RecoveryRestoreDNS)
	}
	if m.Item(T.RecoveryManual).Chosen() {
		_ = mygo.Shell.OpenExternal(recoveryDocURL())
	}
}

// reasonTip is the ⓘ after a banner reason that needs explaining, or "".
func reasonTip(r status.Reason) string {
	if r.Code == status.ReasonDNSRestored {
		return i18n.T.TipDNSRestored
	}
	return ""
}

// dnsWatchEventText words a dns.watchdog event for 日志 → Twinnel; a
// restore counts as a failure (mihomo is down) and goes to the overview.
func dnsWatchEventText(data json.RawMessage) (text string, failed bool, page string) {
	T := i18n.T
	var w rpc.DNSWatch
	json.Unmarshal(data, &w)
	switch {
	case w.Phase == rpc.DNSWatchRestored:
		return T.EvDNSRestored, true, pageOverview
	case w.Phase == rpc.DNSWatchWatching && len(w.LeftAlone) > 0:
		return i18n.F(T.EvDNSBackLeft, len(w.LeftAlone)), false, pageOverview
	case w.Phase == rpc.DNSWatchWatching:
		return T.EvDNSBack, false, pageOverview
	}
	return T.EvDNSEnded, false, pageOverview
}
