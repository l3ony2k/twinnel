package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

func TestProxyEnv(t *testing.T) {
	for _, tc := range []struct {
		cfg  mihomo.Config
		want string
	}{
		{mihomo.Config{MixedPort: 7890}, "export https_proxy=http://127.0.0.1:7890 http_proxy=http://127.0.0.1:7890 all_proxy=socks5://127.0.0.1:7890"},
		{mihomo.Config{Port: 7891, SocksPort: 7892}, "export https_proxy=http://127.0.0.1:7891 http_proxy=http://127.0.0.1:7891 all_proxy=socks5://127.0.0.1:7892"},
		{mihomo.Config{SocksPort: 7892}, "export all_proxy=socks5://127.0.0.1:7892"},
		{mihomo.Config{}, ""},
	} {
		if got := proxyEnv(tc.cfg); got != tc.want {
			t.Errorf("%+v: %q", tc.cfg, got)
		}
	}
}

func TestPortsAndDNSText(t *testing.T) {
	i18n.Set("zh-Hans")
	if got := portsText(mihomo.Config{MixedPort: 7890, SocksPort: 7891}); got != "mixed 7890 · socks 7891" {
		t.Errorf("ports %q", got)
	}
	for in, want := range map[string]string{"": "", "fake-ip": "fake-ip", status.DNSModeOff: "未启用", status.DNSModeNormal: "普通"} {
		if got := dnsModeText(in); got != want {
			t.Errorf("dns %q: %q", in, got)
		}
	}
	if got := dashboardURL("http://127.0.0.1:9090/"); got != "http://127.0.0.1:9090/ui/" {
		t.Errorf("dashboard %q", got)
	}
	if got := shortTSVersion("1.90.6-t1234abcd-g5678"); got != "1.90.6" {
		t.Errorf("version %q", got)
	}
}

// The health row says the banner's one number at the banner's level
// (R-kubai-6): findings rated a warning are 个问题, never 项失败.
func TestHealthReasonShort(t *testing.T) {
	i18n.Set("zh-Hans")
	if got := healthReasonShort(status.Reason{Level: status.LevelWarn, Count: 1, Code: status.ReasonHealth}); got != "1 个问题" {
		t.Errorf("warn %q", got)
	}
	if got := healthReasonShort(status.Reason{Level: status.LevelError, Count: 2, Code: status.ReasonHealth}); got != "2 项失败" {
		t.Errorf("error %q", got)
	}
	if levelIcon(status.LevelWarn) != iconWarning || levelIcon(status.LevelError) != iconError || levelIcon(status.LevelOK) != iconOK {
		t.Error("level glyphs")
	}
}

// 最近活动 folds a run of events that read the same (the periodic health
// runs) into one row with a count, newest first, at most limit rows.
func TestRecentRows(t *testing.T) {
	i18n.Set("zh-Hans")
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.Local)
	health := func(seq uint64) rpc.Event {
		b, _ := json.Marshal(rpc.HealthSummary{Counts: map[string]int{"fail": 1}})
		return rpc.Event{Seq: seq, Time: now.Add(time.Duration(seq) * time.Minute), Type: rpc.EventHealthUpdated, Data: b}
	}
	evs := []rpc.Event{
		{Seq: 1, Time: now, Type: rpc.EventTailnetChanged},
		{Seq: 2, Time: now, Type: "unknown-kind"},
		{Seq: 3, Time: now, Type: rpc.EventKernelChanged},
		health(4), health(5), health(6),
	}
	rows := recentRows(evs, nil, 2)
	if len(rows) != 2 || rows[0].e.Seq != 6 || rows[0].n != 3 || rows[1].e.Seq != 3 || rows[1].n != 1 {
		t.Fatalf("rows %+v", rows)
	}
	if rows := recentRows(evs, nil, 3); len(rows) != 3 || rows[2].e.Seq != 1 {
		t.Errorf("three rows %+v", rows)
	}
	if rows := recentRows(nil, nil, 3); len(rows) != 0 {
		t.Errorf("none %+v", rows)
	}
}

func TestEventTime(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.Local)
	if got := eventTime(now, now.Add(-2*time.Hour)); got != "10:00" {
		t.Errorf("today %q", got)
	}
	if got := eventTime(now, now.Add(-36*time.Hour)); got != "11-26" {
		t.Errorf("yesterday %q", got)
	}
}

func TestPageMaxWidth(t *testing.T) {
	if pageMaxWidth(pageOverview) <= plat.contentMax || pageMaxWidth(pageHealth) != plat.contentMax {
		t.Errorf("overview %v, health %v", pageMaxWidth(pageOverview), pageMaxWidth(pageHealth))
	}
}
