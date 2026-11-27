package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestConfigText(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.e.ConfigText(ctx, rpc.ConfigGetParams{Source: "live"}); err == nil {
		t.Error("live without a file")
	}
	r := h.apply("x")
	live, err := h.e.ConfigText(ctx, rpc.ConfigGetParams{Source: rpc.ConfigLive})
	if err != nil || strings.Contains(live.Content, testSecret) || !strings.Contains(string(h.disk()), testSecret) {
		t.Fatalf("live: %v (secret leaked: %v)", err, strings.Contains(live.Content, testSecret))
	}
	pre, err := h.e.ConfigText(ctx, rpc.ConfigGetParams{Source: rpc.ConfigPreview})
	if err != nil || pre.Hash != live.Hash || pre.Hash != r.Hash {
		t.Errorf("preview %s live %s apply %s %v", pre.Hash, live.Hash, r.Hash, err)
	}
	hist, err := h.e.ConfigText(ctx, rpc.ConfigGetParams{Source: rpc.ConfigHistory, ID: r.HistoryID})
	if err != nil || hist.Hash != live.Hash || hist.Size != len(hist.Content) {
		t.Errorf("history %+v %v", hist.Hash, err)
	}
	for _, p := range []rpc.ConfigGetParams{{Source: "history"}, {Source: "history", ID: "nope"}, {Source: "file"}} {
		if _, err := h.e.ConfigText(ctx, p); err == nil {
			t.Errorf("%+v accepted", p)
		}
	}
}
