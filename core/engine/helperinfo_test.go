package engine

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

func TestHelperInfo(t *testing.T) {
	h := newHarness(t)
	info, err := h.e.HelperInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != HelperVersion || info.StateDir != h.e.Store.Dir || info.Subscriptions != 1 || info.Layers == 0 {
		t.Fatalf("info = %+v", info)
	}
	if info.Mihomo.Label != store.DefaultMihomoLabel || info.Hardened != nil {
		t.Fatalf("mihomo = %+v", info.Mihomo)
	}
	// Only keys that hold a secret; never values.
	if !slices.Equal(info.SecretKeys, []string{store.SecretController}) {
		t.Fatalf("keys = %v", info.SecretKeys)
	}
	h.e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Hardened = &store.Hardening{PlanID: "p", OldLabel: "com.example.mihomo", OldPlist: "/L/com.example.mihomo.plist"}
		return nil
	})
	info, _ = h.e.HelperInfo(context.Background())
	if info.Hardened == nil || info.Hardened.OldLabel != "com.example.mihomo" {
		t.Fatalf("hardened = %+v", info.Hardened)
	}
}

func TestRestartHelper(t *testing.T) {
	h := newHarness(t)
	if _, err := h.e.RestartHelper(); err == nil {
		t.Fatal("restart without a Restart hook")
	}
	done := make(chan struct{})
	h.e.Restart = func() { close(done) }
	r, err := h.e.RestartHelper()
	if err != nil || r.PID == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Restart not called")
	}
	var _ rpc.HelperRestartResult = r
}
