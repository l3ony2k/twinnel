package engine

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// U41: an applied entry records its overrides; a rollback to an entry the
// overrides no longer compose sets the drift mark, automatic re-compose
// waits, layer.restore sets the overrides back, and an apply clears it.
func TestRollbackDriftAndRestore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.apply("")
	hist, _ := h.history()
	if hist[0].ReasonCode != store.ReasonApply || hist[0].Layers == nil || len(hist[0].Layers.Layers) != 2 || !hist[0].Layers.Coexist {
		t.Fatalf("applied entry: %+v", hist[0])
	}

	// Change the overrides: edit a layer, add one, turn coexistence off.
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN,changed.example,DIRECT\n"})
	h.e.PutLayer(rpc.LayerPutParams{ID: "extra", Content: "+rules:\n  - DOMAIN,extra.example,DIRECT\n"})
	off := false
	h.e.UpdateSettings(rpc.SettingsUpdate{TailscaleCoexist: &off})
	h.apply("v2")
	if s, _ := h.e.Settings(); s.Drift != nil {
		t.Fatalf("drift after an apply: %+v", s.Drift)
	}

	if _, err := h.e.Rollback(ctx, first.HistoryID); err != nil {
		t.Fatal(err)
	}
	s, _ := h.e.Settings()
	if s.Drift == nil || s.Drift.HistoryID != first.HistoryID || !s.Drift.Manifest {
		t.Fatalf("drift after rollback: %+v", s.Drift)
	}
	hist, _ = h.history()
	if hist[0].ReasonCode != store.ReasonRollback || hist[0].ReasonArgs[0] != first.HistoryID || hist[0].Layers == nil {
		t.Errorf("rollback entry: %+v", hist[0])
	}

	// A tailnet change does not re-compose while drift is set.
	on := true
	h.e.UpdateSettings(rpc.SettingsUpdate{TailscaleCoexist: &on})
	status, _ := os.ReadFile("testdata/tailscale-status.json")
	h.tailnet.set(bytes.Replace(status, []byte(`"nas.example`), []byte(`"nas2.example`), 1))
	if r, err := h.e.RefreshTailnet(ctx); err != nil || r.Apply != nil {
		t.Fatalf("auto apply during drift: %+v %v", r, err)
	}

	// Restore the overrides of the entry.
	res, err := h.e.RestoreLayers(ctx, rpc.LayerRestoreParams{HistoryID: first.HistoryID})
	if err != nil || len(res.Restored) != 2 || len(res.Removed) != 1 || res.Removed[0] != "extra" {
		t.Fatalf("restore: %+v %v", res, err)
	}
	lc, _ := h.e.Layer(ctx, "my-rules")
	if bytes.Contains([]byte(lc.Content), []byte("changed.example")) {
		t.Error("layer content not restored")
	}
	if s, _ := h.e.Settings(); s.Drift != nil || !s.TailscaleCoexist {
		t.Errorf("after restore: %+v", s)
	}

	// One layer only.
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules: []\n"})
	if _, err := h.e.RestoreLayers(ctx, rpc.LayerRestoreParams{HistoryID: first.HistoryID, IDs: []string{"my-rules"}}); err != nil {
		t.Fatal(err)
	}
	if lc, _ := h.e.Layer(ctx, "my-rules"); lc.Content == "+rules: []\n" {
		t.Error("single layer not restored")
	}
	if _, err := h.e.RestoreLayers(ctx, rpc.LayerRestoreParams{HistoryID: first.HistoryID, IDs: []string{"nope"}}); err == nil {
		t.Error("restoring an unknown layer succeeded")
	}
}

// A rollback to the overrides' own entry is no drift; the first backup is
// marked as the config before Twinnel.
func TestRollbackSameLayersNoDrift(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(h.cfgPath, []byte("mixed-port: 7890\n"), 0o600)
	first := h.apply("one")
	h.apply("two") // unchanged, nothing new
	hist, _ := h.history()
	var backup *store.HistoryEntry
	for i := range hist {
		if hist[i].Kind == store.HistoryBackup {
			backup = &hist[i]
		}
	}
	if backup == nil || !backup.BeforeTwinnel || backup.ReasonCode != store.ReasonBackup {
		t.Fatalf("backup: %+v", backup)
	}
	if _, err := h.e.Rollback(context.Background(), first.HistoryID); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.e.Settings(); s.Drift != nil {
		t.Errorf("drift for the same overrides: %+v", s.Drift)
	}
	if _, err := h.e.Rollback(context.Background(), backup.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.e.Settings(); s.Drift == nil || s.Drift.Manifest {
		t.Errorf("drift to a backup: %+v", s.Drift)
	}
}
