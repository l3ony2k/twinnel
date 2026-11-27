package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/privfile/privfiletest"
	"github.com/l3ony2k/twinnel/core/rpc"
)

type exportFake map[string]any

func (f exportFake) Call(_ context.Context, method string, params, result any) error {
	key := method
	if p, ok := params.(rpc.IDParams); ok {
		key += "/" + p.ID
	}
	v, ok := f[key]
	if !ok {
		return &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "unknown method"}
	}
	return jsonCopy(v, result)
}

func TestExportConfigFallback(t *testing.T) {
	ctx := context.Background()
	// A helper with config.export: its zip as is.
	x, err := ExportConfig(ctx, exportFake{rpc.MethodConfigExport: rpc.ConfigExport{Zip: []byte("PK-helper"), Layers: 2}}, rpc.ConfigExportParams{})
	if err != nil || string(x.Zip) != "PK-helper" || x.Layers != 2 {
		t.Fatalf("%v %v", x, err)
	}
	// An older helper: built here, managed layers left out.
	old := exportFake{
		rpc.MethodLayerList:             []rpc.Layer{{ID: "mine"}, {ID: "coexist", Managed: true}},
		rpc.MethodConfigGet:             rpc.ConfigText{Content: "mixed-port: 7890\n"},
		rpc.MethodLayerGet + "/mine":    rpc.LayerContent{Content: "rules: []\n"},
		rpc.MethodLayerGet + "/coexist": rpc.LayerContent{Content: "managed\n"},
	}
	x, err = ExportConfig(ctx, old, rpc.ConfigExportParams{})
	if err != nil || x.Layers != 1 {
		t.Fatalf("%v %v", x, err)
	}
	b, err := bundle.Read(x.Zip)
	if err != nil || len(b.Layers) != 1 || b.Layers[0].ID != "mine" {
		t.Fatalf("%v %+v", err, b)
	}

	p := filepath.Join(t.TempDir(), ExportFileName(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	if filepath.Base(p) != "twinnel-config-20260102.zip" {
		t.Fatal(p)
	}
	if err := SaveExport(p, x.Zip); err != nil {
		t.Fatal(err)
	}
	// Owner-only on every platform: 0600, or on Windows the owner-only
	// DACL (it used to inherit the folder's, e.g. %TEMP%'s).
	privfiletest.AssertPrivate(t, p, false)
	// Saving again (--force) replaces the file and keeps it private.
	if err := SaveExport(p, x.Zip); err != nil {
		t.Fatal(err)
	}
	privfiletest.AssertPrivate(t, p, false)
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Fatalf("left behind: %v", es)
	}
}

func jsonCopy(v, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
