package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func testManifest() *rpc.LayerManifest {
	return &rpc.LayerManifest{
		Subscription: "main",
		Coexist:      true,
		Layers: []rpc.ManifestLayer{
			{ID: "base", Name: "Base", Stage: "base", Enabled: true, Hash: "b1"},
			{ID: "app3-demo", Name: "app3 demo", Stage: "user", Enabled: true, Hash: "a1"},
			{ID: "gone", Name: "Gone", Stage: "user", Enabled: true, Hash: "g1"},
		},
	}
}

func testLayers() []rpc.Layer {
	return []rpc.Layer{
		{ID: "base", Name: "Base", Stage: "base", Enabled: true, Hash: "b1"},
		{ID: "app3-demo", Name: "app3 demo", Stage: "user", Enabled: true, Hash: "a2"}, // edited
		{ID: "new", Name: "New", Stage: "user", Enabled: true, Hash: "n1"},
		{ID: coexistLayerID, Stage: "coexist", Enabled: false, Managed: true, NameCode: "coexist"}, // turned off
		{ID: "leak-guard", Stage: "guard", Enabled: false, Managed: true, NameCode: "leak-guard"},
	}
}

func TestPendingChanges(t *testing.T) {
	i18n.Set("zh")
	ch, sub := pendingChanges(testManifest(), testLayers(), &rpc.Settings{}, "other")
	if !sub {
		t.Error("subscription change missed")
	}
	got := strings.Join(changeNames(ch), "|")
	want := "app3 demo|New|" + layerTitle(testLayers()[3]) + "|Gone（已删除）"
	if got != want {
		t.Errorf("names = %q, want %q", got, want)
	}
	if r := changeReason(ch[:1]); r != "改动 app3 demo" {
		t.Errorf("reason = %q", r)
	}
	if r := changeReason(ch[2:3]); !strings.HasPrefix(r, "停用 ") {
		t.Errorf("reason = %q", r)
	}
	if r := changeReason(ch); r != "改动 4 个覆写" {
		t.Errorf("reason = %q", r)
	}
	if ch, _ := pendingChanges(nil, testLayers(), nil, ""); ch != nil {
		t.Error("changes without a manifest")
	}
}

func TestRollbackEffects(t *testing.T) {
	i18n.Set("zh")
	cur := testLayers()
	cur[3].Enabled = true // coexist on now; the target has it on too
	target := &rpc.LayerManifest{Layers: []rpc.ManifestLayer{
		{ID: "base", Stage: "base", Enabled: true, Hash: "b1"},
		{ID: "app3-demo", Name: "app3 demo", Stage: "user", Enabled: true, Hash: "a1"},
	}}
	got := strings.Join(rollbackEffects(target, cur), "\n")
	coexist := layerTitle(cur[3])
	want := "将停用：New、" + coexist + "\n内容不同：app3 demo"
	if got != want {
		t.Errorf("effects =\n%s\nwant\n%s", got, want)
	}
	if got := rollbackEffects(nil, cur); len(got) != 1 || got[0] != i18n.T.RollbackNoLayers {
		t.Errorf("backup: %q", got)
	}
}

func TestDescribeEvent(t *testing.T) {
	i18n.Set("zh")
	data, _ := json.Marshal(rpc.ApplyResult{ReasonCode: "rollback", ReasonArgs: []string{"20261127T060539Z-52d20aaa"}, HistoryID: "h1",
		ComposeResult: rpc.ComposeResult{Changed: true}})
	r, ok := describeEvent(rpc.Event{Type: rpc.EventComposeApplied, Data: data, Time: time.Now()}, func(_, id string) string { return id })
	if !ok || r.kind != "应用" || !strings.HasPrefix(r.text, "已应用 · 回滚到 ") || r.hist != "h1" || r.page != pageHistory {
		t.Errorf("applied: %+v", r)
	}
	data, _ = json.Marshal(rpc.ApplyResult{Reason: "tailnet changed", Error: "reload: EOF"})
	r, _ = describeEvent(rpc.Event{Type: rpc.EventComposeFailed, Data: data}, nil)
	if !r.failed || !strings.Contains(r.text, "Tailscale 设备变化") || !strings.Contains(r.text, "mihomo 重载失败") {
		t.Errorf("failed: %+v", r)
	}
	data, _ = json.Marshal(map[string]any{"id": "main", "changed": true})
	r, _ = describeEvent(rpc.Event{Type: rpc.EventSubUpdated, Data: data}, func(string, string) string { return "Home" })
	if r.text != "订阅「Home」已更新" {
		t.Errorf("sub: %+v", r)
	}
	if _, ok := describeEvent(rpc.Event{Type: rpc.EventEventsTruncated}, nil); ok {
		t.Error("truncated shown")
	}
}

func TestHistoryReasonLegacy(t *testing.T) {
	i18n.Set("zh")
	for in, want := range map[string]string{
		"file on disk before apply": "应用前备份",
		"controller.secure":         "加固控制接口",
		"adopt plan-2026":           "接管",
		"加了公司内网规则":                  "加了公司内网规则",
	} {
		if got := historyReason(rpc.HistoryEntry{Reason: in}); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	got := historyReason(rpc.HistoryEntry{Reason: "rollback to 20261127T060539Z-52d20aaa"})
	if !strings.HasPrefix(got, "回滚到 11-27 ") && !strings.HasPrefix(got, "回滚到 11-26 ") {
		t.Errorf("rollback: %q", got)
	}
}

// 日志 → twinnel: a failed apply is one line in words (no HTTP, no 应用 twice),
// and an edit names what it changed (walkthrough 2, N6 and N7).
func TestDescribeEventWords(t *testing.T) {
	i18n.Set("zh")
	raw := "reload: PUT /configs?force=false: 400 Bad Request: synthetic reload failure"
	data, _ := json.Marshal(rpc.ApplyResult{ReasonCode: "apply", Error: raw})
	r, _ := describeEvent(rpc.Event{Type: rpc.EventComposeFailed, Data: data}, nil)
	if r.text != "应用失败 · mihomo 重载失败，已恢复旧配置" || r.raw != raw {
		t.Errorf("failed apply: %+v", r)
	}
	names := func(kind, id string) string {
		if kind == "layer" && id == "lan" {
			return "局域网直连"
		}
		return id
	}
	for _, tc := range []struct {
		data map[string]string
		want string
	}{
		{map[string]string{"layer": "lan", "op": "on"}, "启用了覆写「局域网直连」"},
		{map[string]string{"layer": "gone", "op": "deleted", "name": "旧覆写"}, "删除了覆写「旧覆写」"},
		{map[string]string{"layer": "lan"}, "覆写「局域网直连」已修改"},
		{map[string]string{"subscription": "main", "op": "activated"}, "改用订阅「main」"},
		{nil, "覆写或订阅已修改"},
	} {
		d, _ := json.Marshal(tc.data)
		if r, _ := describeEvent(rpc.Event{Type: rpc.EventStateChanged, Data: d}, names); r.text != tc.want {
			t.Errorf("%v: %q, want %q", tc.data, r.text, tc.want)
		}
	}
}
