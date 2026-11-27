package health

// The parts of system-dns that depend on mihomo being there: a system DNS
// that points at mihomo while mihomo is down (no name resolves on the
// machine), the DNS watchdog holding the earlier DNS while mihomo does not
// answer, and services that point at mihomo with nothing recorded to put
// back (the watchdog cannot protect them). Texts are kept here instead of
// in the shared tables; TestDNSGuardTextsComplete checks them.

import (
	"context"
)

// DNSGuard is what the helper knows about the system DNS it manages
// (Settings.DNSGuard). Service names are the ones NetworkServices uses.
type DNSGuard struct {
	// Unprotected are the services that point at mihomo with no value
	// recorded to restore (pointed before Twinnel did it): if mihomo
	// stops, nothing can put their DNS back.
	Unprotected []string
	// Restored are the services the DNS watchdog put back on their
	// earlier DNS because mihomo did not answer; it points them back once
	// mihomo is healthy again.
	Restored []string
}

// Reasons of system-dns besides not-mihomo and via-hijack.
const (
	// ReasonDNSMihomoDown: the system DNS points at mihomo and mihomo
	// does not answer: no name resolves.
	ReasonDNSMihomoDown = "system-dns/mihomo-down"
	// ReasonDNSRestored: the watchdog holds the earlier DNS while mihomo
	// is down (or until it has answered long enough).
	ReasonDNSRestored = "system-dns/watchdog-restored"
	// ReasonDNSNoBackup: services point at mihomo with nothing to restore.
	ReasonDNSNoBackup = "system-dns/no-backup"
)

// FixDNSBackup records automatic (DHCP) as the value to restore for the
// unprotected services (the helper's dns.backup); no DNS setting changes.
const FixDNSBackup = "dns-backup"

// dnsGuard is the helper's DNSGuard, nil when this Env has none or it
// cannot be read (those parts of the check are left out then).
func dnsGuard(ctx context.Context, env Env) *DNSGuard {
	read := env.Settings().DNSGuard
	if read == nil {
		return nil
	}
	g, err := read(ctx)
	if err != nil {
		return nil
	}
	return &g
}

// mihomoDown reports that mihomo is not there: neither its controller nor
// its DNS listener answers. A controller that refuses the secret, or a DNS
// that answers NXDOMAIN, is there.
func mihomoDown(ctx context.Context, env Env) bool {
	if _, err := env.MihomoRuntime(ctx); err == nil {
		return false
	}
	_, err := env.LookupMihomo(ctx, DataplaneProbeName)
	return err != nil
}

type dnsGuardTexts struct {
	msgDown         string
	msgRestored     string
	msgRestoredBack string
	msgNoBackup     string
	evNoBackup      string
	evRestored      string
	evWatchdogWill  string
	fixRestart      string
	fixBackup       string
}

var zhDNSGuard = dnsGuardTexts{
	msgDown:         "DNS 指向 mihomo，但 mihomo 没有响应，本机解析不了域名：%s",
	msgRestored:     "mihomo 没有响应，系统 DNS 已临时恢复为原来的设置：%s",
	msgRestoredBack: "mihomo 已恢复，系统 DNS 稍后自动指回 mihomo：%s",
	msgNoBackup:     "DNS 指向 mihomo，但没有可恢复的备份，mihomo 停了会断网：%s",
	evNoBackup:      "没有可恢复的备份",
	evRestored:      "看门狗临时恢复",
	evWatchdogWill:  "看门狗会临时恢复原来的 DNS",
	fixRestart:      "重启 mihomo",
	fixBackup:       "把「自动（DHCP）」记为这些连接的备份，mihomo 停了时看门狗能临时恢复（不改现在的 DNS）",
}

var enDNSGuard = dnsGuardTexts{
	msgDown:         "DNS points at mihomo, but mihomo does not answer: no name resolves on this computer: %s",
	msgRestored:     "mihomo does not answer; the system DNS is temporarily back to its earlier settings: %s",
	msgRestoredBack: "mihomo is back; the system DNS points at it again shortly: %s",
	msgNoBackup:     "DNS points at mihomo with no backup to restore; if mihomo stops, the network goes down: %s",
	evNoBackup:      "no backup to restore",
	evRestored:      "temporarily restored by the watchdog",
	evWatchdogWill:  "The watchdog restores the earlier DNS for the time being",
	fixRestart:      "Restart mihomo",
	fixBackup:       "Record Automatic (DHCP) as these connections' backup, so the watchdog can restore it when mihomo stops (the DNS is not changed now)",
}

func dnsGuardFor(L *texts) *dnsGuardTexts {
	if L == textTables["en"] {
		return &enDNSGuard
	}
	return &zhDNSGuard
}

func init() {
	fixCatalog[ReasonDNSMihomoDown] = FixSpec{Kind: FixAuto, Action: FixRestartMihomo}
	fixCatalog[ReasonDNSRestored] = FixSpec{Kind: FixAuto, Action: FixRestartMihomo}
	fixCatalog[ReasonDNSNoBackup] = FixSpec{Kind: FixAuto, Action: FixDNSBackup}

	zhFixTexts[ReasonDNSMihomoDown] = FixText{"系统 DNS 指向 mihomo，但 mihomo 没有响应：本机所有域名都解析不了，网页打不开。",
		"重启 mihomo。"}
	zhFixTexts[ReasonDNSRestored] = FixText{"mihomo 没有响应。为了不断网，看门狗临时把系统 DNS 恢复成了原来的设置，现在不经过 mihomo 分流。",
		"重启 mihomo。它恢复后，看门狗会自动把 DNS 指回 mihomo。"}
	zhFixTexts[ReasonDNSNoBackup] = FixText{"这些连接的 DNS 在 Twinnel 之前就指向了 mihomo，没有记下原来的设置：mihomo 一停，本机就解析不了域名，看门狗也没法恢复。",
		"把「自动（DHCP）」记为它们的备份。现在的 DNS 不变；以后 mihomo 停了，看门狗会临时恢复成自动获取。"}

	enFixTexts[ReasonDNSMihomoDown] = FixText{"The system DNS points at mihomo, but mihomo does not answer: no name resolves on this computer and web pages do not open.",
		"Restart mihomo."}
	enFixTexts[ReasonDNSRestored] = FixText{"mihomo does not answer. To keep you online, the watchdog put the system DNS back to its earlier settings for now; DNS does not go through mihomo meanwhile.",
		"Restart mihomo. Once it answers, the watchdog points the DNS back at it."}
	enFixTexts[ReasonDNSNoBackup] = FixText{"These connections pointed their DNS at mihomo before Twinnel did, so their earlier settings were never recorded: if mihomo stops, no name resolves and the watchdog cannot help.",
		"Record Automatic (DHCP) as their backup. The DNS stays as it is; if mihomo stops later, the watchdog switches it to automatic for the time being."}
}
