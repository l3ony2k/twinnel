package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

type fakeHostKeeper struct {
	state   *rpc.ServiceHost
	refresh *rpc.ServiceHostRefresh
	calls   int
}

func (f *fakeHostKeeper) State(context.Context) *rpc.ServiceHost { return f.state }
func (f *fakeHostKeeper) Refresh(context.Context) (*rpc.ServiceHostRefresh, error) {
	f.calls++
	return f.refresh, nil
}

func TestRefreshServiceHost(t *testing.T) {
	h := newHarness(t)
	k := &fakeHostKeeper{
		state:   &rpc.ServiceHost{Path: `C:\T\twinnel-svchost.exe`, Version: "0.2.0"},
		refresh: &rpc.ServiceHostRefresh{From: "0.1.0", To: "0.2.0", Replaced: true, Restarted: []string{"TwinnelMihomo"}},
	}
	h.e.ServiceHost = k
	_, seq := h.e.Bus.Since(0)
	h.e.RefreshServiceHost(context.Background())
	evs, _ := h.e.Bus.Since(seq)
	var found bool
	for _, ev := range evs {
		if ev.Type != rpc.EventServiceChanged {
			continue
		}
		var d struct {
			Op   string                 `json:"op"`
			Host rpc.ServiceHostRefresh `json:"host"`
		}
		json.Unmarshal(ev.Data, &d)
		found = d.Op == "host-refresh" && len(d.Host.Restarted) == 1
	}
	if k.calls != 1 || !found {
		t.Fatalf("calls %d, event %v: %+v", k.calls, found, evs)
	}
	// Nothing to do: no event.
	k.refresh = nil
	_, seq = h.e.Bus.Since(0)
	h.e.RefreshServiceHost(context.Background())
	if evs, _ := h.e.Bus.Since(seq); len(evs) != 0 {
		t.Errorf("events %+v", evs)
	}
	st, err := h.e.Status(context.Background())
	if err != nil || st.ServiceHost == nil || st.ServiceHost.Version != "0.2.0" {
		t.Fatalf("status.get service host = %+v, %v", st.ServiceHost, err)
	}
}
