package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestEffectRows(t *testing.T) {
	p := &rpc.TakeoverPlan{LiveConfig: "/x/conf/config.yaml", MihomoVersion: "v1.19.34", Effects: []rpc.TakeoverEffect{
		{Item: rpc.EffMihomoService, Action: rpc.ActKeep, Subject: "com.example.mihomo"},
		{Item: rpc.EffMihomoBinary, Action: rpc.ActKeep, Subject: "/x/bin/mihomo", Warn: rpc.WarnReplaceable},
		{Item: rpc.EffConfig, Action: rpc.ActRewrite, Subject: "/x/conf/config.yaml", Warn: rpc.WarnEditsLost},
		{Item: rpc.EffSubscription, Action: rpc.ActNone},
		{Item: rpc.EffSecret, Action: rpc.ActNone, Warn: rpc.WarnNoSecret},
		{Item: rpc.EffTailscale, Action: rpc.ActUnchanged, Subject: "sh.brew.tailscale"},
		{Item: "future-item", Action: rpc.ActKeep},
	}}
	rows := effectRows(p)
	if len(rows) != 6 {
		t.Fatalf("rows %+v", rows)
	}
	if !rows[1].warn || rows[1].now != "mihomo · v1.19.34" || rows[4].warn { // H13: no secret is not a risk
		t.Fatalf("binary/secret rows %+v %+v", rows[1], rows[4])
	}
	rw := rewriteRows(p)
	if len(rw) != 1 || rw[0].nowTip != p.LiveConfig {
		t.Fatalf("rewrite rows %+v", rw)
	}
	if body := adoptConfirmBody(p); !strings.Contains(body, p.LiveConfig) || strings.Contains(body, "com.example.mihomo") {
		t.Fatalf("confirm lists more than the rewrites: %q", body)
	}
}

func TestChangeSamples(t *testing.T) {
	ch := rpc.TakeoverChange{Area: "dns.nameserver-policy", WhyCode: "coexist-nameserver-policy", WhyArgs: []string{"100.100.100.100"},
		Keys: []string{"*", "+.64.100.in-addr.arpa", "+.tailnet-0000.ts.net", "+.ts.net"}}
	got := changeSamples(ch)
	if len(got) != 3 || !strings.Contains(got[0], "100.100.100.100") || !strings.Contains(got[1], "tailnet-0000.ts.net") {
		t.Fatalf("samples %q", got)
	}
	if s := changeSamples(rpc.TakeoverChange{Area: "hosts", Keys: []string{"alpha", "alpha.x.ts.net", "beta"}}); len(s) != 1 || strings.Contains(s[0], "ts.net") {
		t.Fatalf("hosts samples %q", s)
	}
	if changeSamples(rpc.TakeoverChange{Area: "rules"}) != nil {
		t.Fatal("samples without keys")
	}
}

func TestPlanWarnings(t *testing.T) {
	p := &rpc.TakeoverPlan{Effects: []rpc.TakeoverEffect{{Item: rpc.EffConfig}}, Impact: &rpc.ReloadImpact{},
		Warnings: []string{
			"the controller has no secret; run 加固控制接口 after adopt",
			"adopt's reload interrupts the network briefly: x",
			"the mihomo binary /b can be replaced without root (owned by uid 501), and the service runs it as root: …",
			"something else",
		}}
	if w := planWarnings(p); len(w) != 1 || w[0] != "something else" {
		t.Fatalf("warnings %q", w)
	}
}

func TestStepWords(t *testing.T) {
	defer i18n.Set("zh-Hans")
	i18n.Set("en")
	if n := stepName(rpc.AdoptStep{Name: "health before"}); n != "Check (before)" {
		t.Fatalf("legacy step name %q", n)
	}
	if n := stepName(rpc.AdoptStep{Name: "frobnicate", Code: "frobnicate"}); n != "frobnicate" {
		t.Fatalf("unknown step %q", n)
	}
	if p := stepPhrase(rpc.AdoptStep{Code: "apply", Status: "ok", Detail: "written=true reloaded=true history abc"}); p != "Written and reloaded" {
		t.Fatalf("phrase %q", p)
	}
	r := &rpc.AdoptResult{HealthAfter: []rpc.AdoptCheck{{ID: "a", Status: "fail"}, {ID: "b", Status: "ok"}, {ID: "c", Status: "fail"}}}
	if f := adoptFailing(r); len(f) != 2 {
		t.Fatalf("failing %v", f)
	}
	if s := undoStepText(rpc.TakeoverUndoStep{Code: rpc.UndoRemoveLayers, Count: 3}); !strings.Contains(s, "3") {
		t.Fatalf("undo step %q", s)
	}
	if s := undoStepText(rpc.TakeoverUndoStep{Code: rpc.UndoRestoreConfig, Path: "/c.yaml"}); !strings.Contains(s, "/c.yaml") {
		t.Fatalf("undo step %q", s)
	}
}

func TestPendingRestart(t *testing.T) {
	for _, c := range []struct {
		running, current string
		want             bool
	}{
		{"v1.19.34", "v1.19.34", false},
		{"1.19.34", "v1.19.34", false},
		{"v1.19.33", "v1.19.34", true},
		{"", "v1.19.34", false},
		{"v1.19.34", "", false},
	} {
		if got := pendingRestart(c.running, c.current); got != c.want {
			t.Errorf("pendingRestart(%q, %q) = %v", c.running, c.current, got)
		}
	}
}

func TestAppInventoryAndExtras(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	backups := filepath.Join(home, "Library", "Application Support", "twinnel", "backups")
	agents := filepath.Join(home, "Library", "LaunchAgents")
	os.MkdirAll(filepath.Join(backups, "Twinnel-0.1.app"), 0o755)
	os.WriteFile(filepath.Join(backups, "Twinnel-0.1.app", "x"), []byte("12345"), 0o644)
	os.MkdirAll(agents, 0o755)
	agent := filepath.Join(agents, twinnelAgentPrefix+".dev.plist")
	other := filepath.Join(agents, "com.other.plist")
	os.WriteFile(agent, []byte("<plist/>"), 0o644)
	os.WriteFile(other, []byte("<plist/>"), 0o644)
	prefs := filepath.Join(home, "app.json")
	os.WriteFile(prefs, []byte("{}"), 0o644)

	items := appInventory("", prefs, true)
	kinds := map[string]rpc.InventoryItem{}
	for _, it := range items {
		kinds[it.Kind] = it
	}
	if kinds[invAppBackups].Size != 5 || kinds[invAppBackups].Count != 1 || kinds[invLaunchAgent].Path != agent || kinds[invAppPrefs].Path != prefs {
		t.Fatalf("inventory %+v", items)
	}
	if _, ok := kinds[invLoginItem]; !ok {
		t.Fatal("no login item")
	}
	if n := len(appExtraItems()); n != 2 {
		t.Fatalf("extras %d", n)
	}

	var booted []string
	old := bootoutAgent
	bootoutAgent = func(p string) error { booted = append(booted, p); return nil }
	defer func() { bootoutAgent = old }()
	removeAppExtras()
	if _, err := os.Stat(backups); !os.IsNotExist(err) {
		t.Fatal("backups kept")
	}
	if _, err := os.Stat(agent); !os.IsNotExist(err) || len(booted) != 1 || booted[0] != agent {
		t.Fatalf("agent %v booted %v", err, booted)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another app's agent removed")
	}
	if _, err := os.Stat(filepath.Dir(backups)); err != nil {
		t.Fatal("the Twinnel support dir itself went")
	}
}

type fakeCaller map[string]any

func (f fakeCaller) Call(_ context.Context, method string, params, result any) error {
	key := method
	if p, ok := params.(rpc.IDParams); ok {
		key += "/" + p.ID
	}
	if _, ok := f[key]; !ok && method == rpc.MethodConfigExport {
		return &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "unknown method"} // a helper from before it
	}
	b, _ := json.Marshal(f[key])
	return json.Unmarshal(b, result)
}

func TestWriteExport(t *testing.T) {
	to := filepath.Join(t.TempDir(), "out.zip")
	hc := fakeCaller{
		rpc.MethodConfigGet:             rpc.ConfigText{Content: "mixed-port: 7890\n"},
		rpc.MethodLayerGet + "/mine":    rpc.LayerContent{Content: "rules: []\n"},
		rpc.MethodLayerGet + "/coexist": rpc.LayerContent{Content: "managed\n"},
	}
	hc[rpc.MethodLayerList] = []rpc.Layer{{ID: "mine"}, {ID: "coexist", Managed: true}}
	if err := writeExport(context.Background(), hc, to); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(to)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "manifest.json,config.yaml,overrides/mine.yaml" {
		t.Fatalf("zip has %v", names)
	}
	if fi, _ := os.Stat(to); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	data, _ := os.ReadFile(to)
	b, err := bundle.Read(data)
	if err != nil || b.Source != bundle.SourceTwinnel || len(b.Layers) != 1 || b.Layers[0].ID != "mine" || b.Layers[0].Content != "rules: []\n" {
		t.Fatalf("%v %v", err, b)
	}

	// A helper with config.export: its zip as is.
	hc[rpc.MethodConfigExport] = rpc.ConfigExport{Zip: []byte("PK-from-helper"), Layers: 1}
	if err := writeExport(context.Background(), hc, to); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(to); string(got) != "PK-from-helper" {
		t.Fatalf("got %q", got)
	}
}

func TestImportSheetChoices(t *testing.T) {
	plan := &rpc.ConfigImportResult{Items: []rpc.ImportItem{
		{Kind: "layer", ID: "a", Action: rpc.ImportAdd},
		{Kind: "layer", ID: "b", Action: rpc.ImportConflict},
		{Kind: "subscription", ID: "s", Action: rpc.ImportConflict},
		{Kind: "layer", ID: "g", Action: rpc.ImportGenerated},
		{Kind: "layer", ID: "u", Action: rpc.ImportUnchanged},
	}}
	skip, rename := importDefault, 2
	v := &importView{plan: plan, choice: map[string]*int{"layer:b": &skip, "subscription:s": &rename}}
	r := importResolve(v)
	if r["layer:b"] != rpc.ImportSkip || r["subscription:s"] != rpc.ImportRename || len(r) != 2 {
		t.Fatalf("%v", r)
	}
	if n := importWrites(plan, r); n != 2 {
		t.Fatalf("writes = %d", n)
	}
	replace := 0
	v.choice["layer:b"] = &replace
	if n := importWrites(plan, importResolve(v)); n != 3 {
		t.Fatalf("writes = %d", n)
	}
}
