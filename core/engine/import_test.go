package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// Synthetic: no real subscription or node data.
const importToken = "synthetic-import-token"

var importURL = "https://sub.example.invalid/link?token=" + importToken

// freshEngine is a new install: an empty state dir, no config.
func freshEngine(t *testing.T) *Engine {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, &store.MemSecrets{})
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return e
}

// exportFrom builds the harness machine's export.
func exportFrom(t *testing.T, h *harness) []byte {
	t.Helper()
	ctx := context.Background()
	h.e.Secrets.Set(store.SubscriptionURLKey("main"), importURL)
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "off-rules", Name: "关掉的", Format: store.FormatVerge, Enabled: new(false),
		Content: "prepend-rules:\n  - DOMAIN,off.example.invalid,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	// User order: off-rules before my-rules.
	if err := h.e.ReorderLayers([]string{"base", "off-rules", "my-rules"}); err != nil {
		t.Fatal(err)
	}
	h.apply("before export")
	x, err := h.e.Export(ctx, rpc.ConfigExportParams{})
	if err != nil {
		t.Fatal(err)
	}
	if x.Layers != 3 || x.Subscriptions != 1 || x.NoContent {
		t.Fatalf("export: %v", x)
	}
	if s := x.String() + " " + mustJSONless(x); strings.Contains(s, importToken) {
		t.Fatal("export's String shows the URL")
	}
	return x.Zip
}

func mustJSONless(x rpc.ConfigExport) string {
	x.Zip = nil
	b, _ := json.Marshal(x)
	return string(b)
}

func importItems(r rpc.ConfigImportResult) map[string]rpc.ImportItem {
	m := map[string]rpc.ImportItem{}
	for _, it := range r.Items {
		m[it.Kind+":"+it.ID] = it
	}
	return m
}

func TestExportImportFreshMachine(t *testing.T) {
	h := newHarness(t)
	z := exportFrom(t, h)

	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "manifest.json,config.yaml,overrides/base.yaml,overrides/off-rules.yaml,overrides/my-rules.yaml,subscriptions/main.yaml" {
		t.Fatalf("zip: %v", names)
	}
	b, _ := bundle.Read(z)
	if strings.Contains(b.Config, testSecret) {
		t.Fatal("the controller secret is in the export")
	}

	e := freshEngine(t)
	ctx := context.Background()
	dry, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, Name: "x.zip", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	it := importItems(dry)
	if !dry.DryRun || dry.Source != bundle.SourceTwinnel || dry.Version != 1 || dry.Conflicts != 0 || dry.Stored != 0 || dry.Activated != "main" {
		t.Fatalf("dry: %+v", dry)
	}
	if it["layer:base"].Action != rpc.ImportGenerated || it["layer:my-rules"].Action != rpc.ImportAdd ||
		it["layer:off-rules"].Action != rpc.ImportAdd || it["subscription:main"].Action != rpc.ImportAdd ||
		!it["subscription:main"].HasURL || !it["subscription:main"].HasContent {
		t.Fatalf("items: %+v", dry.Items)
	}
	if !strings.Contains(strings.Join(dry.Warnings, ","), rpc.ImportWarnSecrets) {
		t.Fatalf("warnings: %v", dry.Warnings)
	}
	if st, _ := e.Store.Settings(); len(st.Layers)+len(st.Subscriptions) != 0 {
		t.Fatal("dry run wrote")
	}
	raw, _ := json.Marshal(dry)
	if strings.Contains(string(raw), importToken) {
		t.Fatal("the result carries the URL")
	}

	r, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, Name: "x.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Stored != 3 || r.Activated != "main" {
		t.Fatalf("result: %+v", r)
	}
	st, _ := e.Store.Settings()
	if len(st.Layers) != 2 || st.Layers[0].ID != "off-rules" || st.Layers[1].ID != "my-rules" {
		t.Fatalf("layers: %+v", st.Layers)
	}
	if st.Layers[0].Enabled || st.Layers[0].Name != "关掉的" || st.Layers[0].Format != store.FormatVerge || !st.Layers[1].Enabled {
		t.Fatalf("layer meta: %+v", st.Layers)
	}
	if u, _ := e.Secrets.Get(store.SubscriptionURLKey("main")); u != importURL {
		t.Fatal("url not stored")
	}
	sub, ok := st.ActiveSubscription()
	src, _ := h.e.Store.ReadSubscription("main")
	got, _ := e.Store.ReadSubscription("main")
	if !ok || sub.ID != "main" || sub.Name != "Main" || !bytes.Equal(src, got) || sub.Hash != store.Hash(src) {
		t.Fatalf("subscription: %+v", sub)
	}
	// The composed preview works from what was imported.
	if _, err := e.Layers(); err != nil {
		t.Fatal(err)
	}

	// Again: everything is already here.
	again, err := e.Import(ctx, rpc.ConfigImportParams{Data: z})
	if err != nil || again.Stored != 0 || again.Conflicts != 0 || again.Activated != "" {
		t.Fatalf("again: %v %+v", err, again)
	}
	for _, x := range again.Items {
		if x.Action != rpc.ImportUnchanged && x.Action != rpc.ImportGenerated {
			t.Fatalf("again: %+v", x)
		}
	}
}

func TestImportConflicts(t *testing.T) {
	h := newHarness(t)
	z := exportFrom(t, h)
	ctx := context.Background()
	e := h.e
	// Change things here after the export.
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN,changed.example.invalid,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PutLayer(rpc.LayerPutParams{ID: "later", Content: "mode: rule\n"}); err != nil {
		t.Fatal(err)
	}
	e.Secrets.Set(store.SubscriptionURLKey("main"), importURL+"-new")

	dry, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	it := importItems(dry)
	if dry.Conflicts != 2 || it["layer:my-rules"].Action != rpc.ImportConflict || it["subscription:main"].Action != rpc.ImportConflict ||
		it["layer:off-rules"].Action != rpc.ImportUnchanged || it["layer:my-rules"].Existing != "my-rules" {
		t.Fatalf("dry: %+v", dry)
	}
	// Unresolved: refused, the plan in the error.
	_, err = e.Import(ctx, rpc.ConfigImportParams{Data: z})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != rpc.CodeConflict {
		t.Fatalf("want a conflict error, got %v", err)
	}
	var plan rpc.ConfigImportResult
	if json.Unmarshal(re.Data, &plan) != nil || plan.Conflicts != 2 || strings.Contains(string(re.Data), importToken) {
		t.Fatalf("conflict data: %s", re.Data)
	}

	// Rename the layer, skip the subscription.
	r, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, OnConflict: rpc.ImportSkip,
		Resolve: map[string]string{"layer:my-rules": rpc.ImportRename}})
	if err != nil {
		t.Fatal(err)
	}
	it = importItems(r)
	if r.Stored != 1 || it["layer:my-rules"].NewID != "my-rules-2" || it["subscription:main"].Action != rpc.ImportSkip {
		t.Fatalf("rename: %+v", r)
	}
	st, _ := e.Store.Settings()
	ids := []string{}
	for _, l := range st.Layers {
		ids = append(ids, l.ID)
	}
	if strings.Join(ids, ",") != "base,off-rules,my-rules,later,my-rules-2" {
		t.Fatalf("order after rename: %v", ids)
	}
	if u, _ := e.Secrets.Get(store.SubscriptionURLKey("main")); u != importURL+"-new" {
		t.Fatal("skip changed the URL")
	}

	// Replace: the layer keeps its place, the URL comes back.
	if _, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, OnConflict: rpc.ImportReplace}); err != nil {
		t.Fatal(err)
	}
	st, _ = e.Store.Settings()
	if st.Layers[2].ID != "my-rules" {
		t.Fatalf("replace moved it: %+v", st.Layers)
	}
	if c, _ := e.Store.ReadLayer("my-rules"); !strings.Contains(string(c), "example.org") {
		t.Fatalf("not replaced: %s", c)
	}
	if u, _ := e.Secrets.Get(store.SubscriptionURLKey("main")); u != importURL {
		t.Fatal("replace kept the old URL")
	}
	if _, err := e.Import(ctx, rpc.ConfigImportParams{Data: z, OnConflict: "merge"}); err == nil {
		t.Fatal("unknown resolution accepted")
	}
}

func TestImportLegacyAndInvalid(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range [][2]string{
		{"config.yaml", "mode: rule\n"},
		{"overrides/good.yaml", "+rules:\n  - DOMAIN,a.example.invalid,DIRECT\n"},
		{"overrides/broken.yaml", "rules: [unclosed\n"},
		{"overrides/tailscale-coexist.yaml", "a: 1\n"},
	} {
		w, _ := zw.Create(f[0])
		w.Write([]byte(f[1]))
	}
	zw.Close()
	e := freshEngine(t)
	r, err := e.Import(context.Background(), rpc.ConfigImportParams{Data: buf.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	it := importItems(r)
	if r.Source != bundle.SourceLegacy || r.Stored != 1 || it["layer:good"].Action != rpc.ImportAdd ||
		it["layer:broken"].Action != rpc.ImportInvalid || it["layer:broken"].Reason == "" ||
		it["layer:"+CoexistLayerID].Action != rpc.ImportGenerated {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(strings.Join(r.Warnings, ","), rpc.ImportWarnNoSubscriptions) {
		t.Fatalf("warnings: %v", r.Warnings)
	}
	st, _ := e.Store.Settings()
	if len(st.Layers) != 1 || st.Layers[0].ID != "good" {
		t.Fatalf("%+v", st.Layers)
	}
}

func TestImportURLListAndOverride(t *testing.T) {
	e := freshEngine(t)
	ctx := context.Background()
	list := "# subs\nhttp://plain.example.invalid/s?token=" + importToken + "\nWork https://work.example.invalid/s\n"
	r, err := e.Import(ctx, rpc.ConfigImportParams{Data: []byte(list), Name: "subs.txt"})
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(r.Warnings, ",")
	if r.Source != bundle.SourceURLs || r.Stored != 2 || r.Activated != "plain-example-invalid" ||
		!strings.Contains(w, rpc.ImportWarnPlainHTTP) || !strings.Contains(w, rpc.ImportWarnNoContent) || !strings.Contains(w, rpc.ImportWarnSecrets) {
		t.Fatalf("%+v", r)
	}
	subs, _ := e.Subscriptions()
	if len(subs) != 2 || !subs[0].Active || !subs[0].HasURL || subs[1].ID != "work" || subs[1].IntervalMinutes != 1440 {
		t.Fatalf("%+v", subs)
	}

	r, err = e.Import(ctx, rpc.ConfigImportParams{Data: []byte("prepend-rules:\n  - DOMAIN,v.example.invalid,DIRECT\n"), Name: "Verge Merge.yaml"})
	if err != nil || r.Stored != 1 || r.Items[0].NewID != "verge-merge" || r.Items[0].Format != store.FormatVerge {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := e.Import(ctx, rpc.ConfigImportParams{Data: []byte("\x00\x01")}); err == nil {
		t.Fatal("binary accepted")
	}
}

func TestExportWithoutSubscriptions(t *testing.T) {
	h := newHarness(t)
	h.e.Secrets.Set(store.SubscriptionURLKey("main"), importURL)
	x, err := h.e.Export(context.Background(), rpc.ConfigExportParams{NoSubscriptions: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Read(x.Zip)
	if err != nil || len(b.Subscriptions) != 0 || x.Subscriptions != 0 || bytes.Contains(x.Zip, []byte(importToken)) {
		t.Fatalf("%v %v", err, b)
	}
}
