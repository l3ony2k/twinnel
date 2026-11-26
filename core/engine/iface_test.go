package engine

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

type fakeIface struct {
	mu   sync.Mutex
	name string
	err  error
}

func (f *fakeIface) get(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.err
}

func (f *fakeIface) set(name string, err error) {
	f.mu.Lock()
	f.name, f.err = name, err
	f.mu.Unlock()
}

func TestTailnetInterfaceReapplies(t *testing.T) {
	h := newHarness(t)
	fi := &fakeIface{name: "utun9"}
	h.e.TailnetInterface = fi.get
	ctx := context.Background()
	h.apply("initial")
	if !bytes.Contains(h.disk(), []byte("100.100.100.100#utun9")) {
		t.Fatalf("resolver not bound to utun9:\n%s", h.disk())
	}
	if r, err := h.e.RefreshTailnet(ctx); err != nil || r.Changed {
		t.Fatalf("same interface: %+v %v", r, err)
	}

	// tailscaled restarted on another utun: re-apply with the new name.
	fi.set("utun4", nil)
	r, err := h.e.RefreshTailnet(ctx)
	if err != nil || !r.Changed || r.Apply == nil || !r.Apply.Reloaded || r.Tailnet.Interface != "utun4" {
		t.Fatalf("new interface: %+v %v", r, err)
	}
	if !bytes.Contains(h.disk(), []byte("100.100.100.100#utun4")) || bytes.Contains(h.disk(), []byte("#utun9")) {
		t.Error("config not regenerated for utun4")
	}

	// tailscaled down: the route follows en0, and lookups may fail. Neither
	// changes anything.
	for _, c := range []struct {
		name string
		err  error
	}{{"en0", nil}, {"", errors.New("route: not in table")}} {
		fi.set(c.name, c.err)
		if r, err := h.e.RefreshTailnet(ctx); err != nil || r.Changed {
			t.Errorf("%q/%v: %+v %v", c.name, c.err, r, err)
		}
	}
	st, _ := h.e.Status(ctx)
	if st.Tailnet.Interface != "utun4" {
		t.Errorf("status interface %q", st.Tailnet.Interface)
	}
}

func TestResolverDefault(t *testing.T) {
	h := newHarness(t)
	s, err := h.e.UpdateSettings(rpc.SettingsUpdate{TailscaleResolver: ptr("system")})
	if err == nil {
		t.Fatalf("system accepted: %+v", s)
	}
	// An old settings.json with "system" loads as the default.
	h.e.Store.UpdateSettings(func(st *store.Settings) error { return nil })
	var raw map[string]any
	if err := h.e.Store.ReadJSON("settings.json", &raw); err != nil {
		t.Fatal(err)
	}
	raw["tailscale"].(map[string]any)["resolver"] = "system"
	if err := h.e.Store.WriteJSON("settings.json", raw); err != nil {
		t.Fatal(err)
	}
	st, err := h.e.Store.Settings()
	if err != nil || st.Tailscale.Resolver != "" {
		t.Fatalf("resolver %q %v", st.Tailscale.Resolver, err)
	}
	h.apply("default resolver")
	if !bytes.Contains(h.disk(), []byte("100.100.100.100")) || bytes.Contains(h.disk(), []byte("system")) {
		t.Errorf("composed resolver:\n%s", h.disk())
	}
}

func ptr[T any](v T) *T { return &v }
