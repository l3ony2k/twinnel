package main

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
)

func TestSummarizeLayer(t *testing.T) {
	i18n.Set("zh")
	defer i18n.Set("zh")
	for _, tc := range []struct{ in, zh, en string }{
		{"# synthetic\n+rules:\n  - DOMAIN-SUFFIX,lan.example,DIRECT\n  - IP-CIDR,192.0.2.0/24,DIRECT\n", "2 条规则", "2 rules"},
		{"dns:\n  nameserver: [https://dns.example/dns-query]\nhosts:\n  '<+.lan>': 192.0.2.10\n", "改 dns、hosts", "changes dns, hosts"},
		{"prepend-rules:\n  - DOMAIN,a.example,DIRECT\nappend-proxies:\n  - {name: x}\n<proxy-groups>: []\nrules+:\n  - MATCH,DIRECT\n", "2 条规则 · 1 个节点", "2 rules · 1 proxy"},
		{"tun:\n  enable: true\ndns: {}\nsniffer: {}\nprofile: {}\nmode: rule\n", "改 tun、dns、sniffer 等 2 项", "changes tun, dns, sniffer +2 more"},
		{"# only a comment\n", "空", "Empty"},
		{"- not a map\n", "", ""},
	} {
		i18n.Set("zh")
		if got := summarizeLayer(tc.in).text(); got != tc.zh {
			t.Errorf("zh %q: got %q, want %q", tc.in, got, tc.zh)
		}
		i18n.Set("en")
		if got := summarizeLayer(tc.in).text(); got != tc.en {
			t.Errorf("en %q: got %q, want %q", tc.in, got, tc.en)
		}
	}
}

func TestInterval(t *testing.T) {
	for _, tc := range []struct {
		mins   int
		zh, en string
	}{
		{1440, "每天更新", "Updates daily"},
		{2880, "每 2 天更新", "Updates every 2 days"},
		{60, "每小时更新", "Updates hourly"},
		{360, "每 6 小时更新", "Updates every 6 hours"},
		{30, "每 30 分钟更新", "Updates every 30 min"},
		{90, "每 90 分钟更新", "Updates every 90 min"},
	} {
		i18n.Set("zh")
		if got := i18n.T.Interval(tc.mins); got != tc.zh {
			t.Errorf("zh %d: %q, want %q", tc.mins, got, tc.zh)
		}
		i18n.Set("en")
		if got := i18n.T.Interval(tc.mins); got != tc.en {
			t.Errorf("en %d: %q, want %q", tc.mins, got, tc.en)
		}
	}
}
