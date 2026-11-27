package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

func TestTakeoverEffects(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	got := map[string]rpc.TakeoverEffect{}
	for _, ef := range p.Effects {
		got[ef.Item] = ef
	}
	want := map[string]string{
		rpc.EffMihomoService: rpc.ActKeep, rpc.EffMihomoBinary: rpc.ActKeep, rpc.EffConfig: rpc.ActRewrite,
		rpc.EffSubscription: rpc.ActStore, rpc.EffTailscale: rpc.ActUnchanged,
	}
	for item, act := range want {
		if got[item].Action != act {
			t.Errorf("%s: %+v, want %s", item, got[item], act)
		}
	}
	if got[rpc.EffMihomoService].Subject != "com.mihomo.proxy" || got[rpc.EffConfig].Subject != h.live || got[rpc.EffConfig].Warn != rpc.WarnEditsLost {
		t.Fatalf("effects %+v", p.Effects)
	}
}

func TestTakeoverUndo(t *testing.T) {
	h := newAdoptHarness(t)
	ctx := context.Background()
	var re *rpc.Error
	if _, err := h.e.TakeoverUndo(ctx, rpc.TakeoverUndoParams{DryRun: true}); !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams {
		t.Fatalf("undo before adopt: %v", err)
	}
	p := h.plan()
	if r, err := h.e.Adopt(ctx, rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); err != nil || !r.OK {
		t.Fatalf("adopt %+v %v", r, err)
	}
	if _, err := h.e.SecureController(ctx); err != nil {
		t.Fatal(err)
	}
	// A layer made after the adopt is kept, turned off.
	if _, err := h.e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Layers = append(st.Layers, store.Layer{ID: "mine", Name: "mine", Stage: "user", Format: store.FormatMerge, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.e.Store.WriteLayer("mine", []byte("rules: []\n"))

	dry, err := h.e.TakeoverUndo(ctx, rpc.TakeoverUndoParams{DryRun: true})
	if err != nil || !dry.DryRun || dry.PlanID != p.PlanID {
		t.Fatalf("dry %+v %v", dry, err)
	}
	codes := map[string]rpc.TakeoverUndoStep{}
	for _, s := range dry.Steps {
		codes[s.Code] = s
	}
	for _, c := range []string{rpc.UndoRestoreConfig, rpc.UndoReload, rpc.UndoRestoreSecret, rpc.UndoRemoveSub, rpc.UndoRemoveLayers, rpc.UndoDisableLayers, rpc.UndoKeepService, rpc.UndoKeepData} {
		if _, ok := codes[c]; !ok {
			t.Errorf("dry run lacks %s: %+v", c, dry.Steps)
		}
	}
	if b, _ := os.ReadFile(h.live); bytes.Equal(b, h.data) {
		t.Fatal("dry run changed the config")
	}

	r, err := h.e.TakeoverUndo(ctx, rpc.TakeoverUndoParams{})
	if err != nil || !r.OK {
		t.Fatalf("undo %+v %v", r, err)
	}
	if b, _ := os.ReadFile(h.live); !bytes.Equal(b, h.data) {
		t.Fatal("live config not restored byte for byte")
	}
	st, _ := h.e.Store.Settings()
	if st.Adopted != nil || len(st.Subscriptions) != 0 || len(st.Layers) != 1 || st.Layers[0].ID != "mine" || st.Layers[0].Enabled {
		t.Fatalf("settings after undo %+v", st)
	}
	if s, err := h.sec.Get(store.SecretController); err == nil && s != "" {
		t.Fatalf("controller secret kept %q (the live config had none)", s)
	}
	if _, err := h.sec.Get(store.SubscriptionURLKey(AdoptedSubscriptionID)); err == nil {
		t.Fatal("subscription url kept")
	}
	list, _ := h.e.TakeoverPlans()
	if list[0].State != rpc.PlanUndone {
		t.Fatalf("plan state %s", list[0].State)
	}
	if _, err := h.e.TakeoverUndo(ctx, rpc.TakeoverUndoParams{}); err == nil {
		t.Fatal("second undo accepted")
	}
}

func TestHelperInventory(t *testing.T) {
	h := newAdoptHarness(t)
	p := h.plan()
	if _, err := h.e.Adopt(context.Background(), rpc.AdoptParams{PlanID: p.PlanID, ConfirmHash: p.ConfirmHash}); err != nil {
		t.Fatal(err)
	}
	h.e.Socket = "/tmp/x.sock"
	inv, err := h.e.HelperInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]rpc.InventoryItem{}
	for _, it := range inv.Items {
		kinds[it.Kind] = it
	}
	for _, k := range []string{rpc.InvHelperBinary, rpc.InvHelperSocket, rpc.InvStateDir, rpc.InvSubscriptions, rpc.InvLayers, rpc.InvTakeoverBackups, rpc.InvSecrets} {
		if _, ok := kinds[k]; !ok {
			t.Errorf("inventory lacks %s: %+v", k, inv.Items)
		}
	}
	if kinds[rpc.InvStateDir].Size == 0 || kinds[rpc.InvSubscriptions].Count != 1 || inv.Adopted != p.PlanID {
		t.Fatalf("inventory %+v", inv)
	}
	for _, n := range kinds[rpc.InvSecrets].Names {
		if n == "pw-secret-1" || n == adoptSubURL {
			t.Fatal("inventory leaks a secret value")
		}
	}
}
