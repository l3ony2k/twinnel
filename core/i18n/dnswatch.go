package i18n

// DNSWatchStrings are the DNS watchdog's texts: the state while the
// system DNS is temporarily back to the earlier settings because mihomo
// does not answer (core/dnswatch), its actions, and 应急恢复's DNS items.
type DNSWatchStrings struct {
	// ReasonDNSRestored: the overview banner's and the tray's reason.
	ReasonDNSRestored string
	// The notification.
	AlertDNSRestored, AlertDNSRestoredBody string
	// Actions on the state: keep the earlier DNS for good (dns.restore).
	DNSKeepRestored string
	// ⓘ after the reason.
	TipDNSRestored string
	// 应急恢复: write the earlier DNS back now; how to do it by hand.
	RecoveryRestoreDNS, RecoveryManual string
	// 日志 → Twinnel: the dns.watchdog event. EvDNSBack: pointed back at
	// mihomo; EvDNSBackLeft: and %d services the user changed were left
	// alone; EvDNSEnded: the restore ended (dns.point or 保持原设置).
	EvDNSRestored, EvDNSBack, EvDNSBackLeft, EvDNSEnded string
}

var zhDNSWatch = DNSWatchStrings{
	ReasonDNSRestored:    "DNS 已临时恢复为原来的设置，因为 mihomo 没有响应",
	AlertDNSRestored:     "DNS 已临时恢复为原来的设置",
	AlertDNSRestoredBody: "mihomo 没有响应，网络暂时改用原来的 DNS。mihomo 恢复后自动改回。",
	DNSKeepRestored:      "保持原设置",
	TipDNSRestored:       "mihomo 停止响应时，Twinnel 把系统 DNS 临时改回接管前的设置，这台电脑仍能上网。mihomo 恢复后自动改回；你手动改过的 DNS 不会被覆盖。选「保持原设置」则不再改回。",
	RecoveryRestoreDNS:   "恢复系统 DNS",
	RecoveryManual:       "手动恢复方法",
	EvDNSRestored:        "mihomo 没有响应，系统 DNS 已临时恢复为原来的设置",
	EvDNSBack:            "mihomo 已恢复，系统 DNS 已改回 mihomo",
	EvDNSBackLeft:        "mihomo 已恢复，系统 DNS 已改回 mihomo；%d 个你手动改过的网络未改动",
	EvDNSEnded:           "DNS 临时恢复已结束",
}

var enDNSWatch = DNSWatchStrings{
	ReasonDNSRestored:    "DNS is temporarily back to your earlier settings because mihomo isn't responding",
	AlertDNSRestored:     "DNS temporarily back to earlier settings",
	AlertDNSRestoredBody: "mihomo isn't responding, so your earlier DNS is in use. It switches back once mihomo recovers.",
	DNSKeepRestored:      "Keep Earlier DNS",
	TipDNSRestored:       "When mihomo stops responding, Twinnel temporarily puts the system DNS back to what it was before takeover, so this computer stays online. It switches back once mihomo recovers; DNS you changed by hand is never overwritten. Choose Keep Earlier DNS to stay on it.",
	RecoveryRestoreDNS:   "Restore System DNS",
	RecoveryManual:       "Manual Recovery Steps",
	EvDNSRestored:        "mihomo isn't responding; system DNS temporarily back to earlier settings",
	EvDNSBack:            "mihomo recovered; system DNS points at mihomo again",
	EvDNSBackLeft:        "mihomo recovered; system DNS points at mihomo again, %d network(s) you changed by hand left alone",
	EvDNSEnded:           "Temporary DNS restore ended",
}
