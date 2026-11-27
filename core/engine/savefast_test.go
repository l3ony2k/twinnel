package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/engine/enginetest"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// countValidate makes h's mihomo -t runs countable.
func countValidate(h *harness) *atomic.Int32 {
	var n atomic.Int32
	h.e.Validate = func(ctx context.Context, bin string, cfg []byte, opts ...config.ValidateOption) (*config.Validation, error) {
		n.Add(1)
		return config.Validate(ctx, bin, cfg, opts...)
	}
	return &n
}

// TestComposeStatus: compose.status says what compose.preview's Changed
// says, without running mihomo -t.
func TestComposeStatus(t *testing.T) {
	h := newHarness(t)
	runs := countValidate(h)
	ctx := context.Background()
	st, err := h.e.ComposeStatus(ctx)
	if err != nil || !st.Changed || st.Hash == "" {
		t.Fatalf("before apply: %+v %v", st, err)
	}
	if runs.Load() != 0 {
		t.Errorf("compose.status ran mihomo -t %d times", runs.Load())
	}
	pv, _ := h.e.Preview(ctx)
	if pv.Hash != st.Hash || pv.Changed != st.Changed {
		t.Errorf("status %+v, preview hash %s changed %v", st, pv.Hash, pv.Changed)
	}
	h.apply("first")
	if st, _ = h.e.ComposeStatus(ctx); st.Changed {
		t.Error("changed right after apply")
	}
	if _, err := h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"}); err != nil {
		t.Fatal(err)
	}
	if st, _ = h.e.ComposeStatus(ctx); !st.Changed {
		t.Error("not changed after a layer edit")
	}
}

// TestValidationReused: a passing mihomo -t is not run again for the same
// bytes (预览 then 应用); a failing one always is.
func TestValidationReused(t *testing.T) {
	h := newHarness(t)
	runs := countValidate(h)
	ctx := context.Background()
	if pv, err := h.e.Preview(ctx); err != nil || !pv.Valid {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	h.apply("after preview")
	if n := runs.Load(); n != 1 {
		t.Errorf("preview + apply ran mihomo -t %d times, want 1", n)
	}
	// Other bytes: checked again.
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"})
	h.e.Preview(ctx)
	if n := runs.Load(); n != 2 {
		t.Errorf("a changed config was not checked again (%d runs)", n)
	}
	// A failing config is checked every time.
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "x-marker: " + enginetest.MarkInvalid + "\n"})
	for i := 0; i < 2; i++ {
		if pv, _ := h.e.Preview(ctx); pv.Valid {
			t.Fatal("invalid config passed")
		}
	}
	if n := runs.Load(); n != 4 {
		t.Errorf("failing checks ran %d times in all, want 4", n)
	}
	// A different binary is a different check.
	h.e.PutLayer(rpc.LayerPutParams{ID: "my-rules", Content: "+rules:\n  - DOMAIN-SUFFIX,example.net,DIRECT\n"})
	st, _ := h.e.Store.Settings()
	bin, _ := h.e.mihomoBinary(st)
	k1 := h.e.validKey(st, bin, []byte("x"))
	if k2 := h.e.validKey(st, bin+"-other", []byte("x")); k1 == k2 {
		t.Error("the key ignores the binary")
	}
	if k3 := h.e.validKey(st, bin, []byte("y")); k1 == k3 {
		t.Error("the key ignores the bytes")
	}
}
