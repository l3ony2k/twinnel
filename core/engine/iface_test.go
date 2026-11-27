package engine

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
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

// The first refresh of a new helper always reports a change (no cache yet).
// Before anything was applied it must not apply on its own: that apply
// raced the user's setup (seen as a stray "tailnet changed" history entry
// composed from half the layers).
func TestTailnetFirstRefreshDoesNotApplyBeforeAnyApply(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r, err := h.e.RefreshTailnet(ctx)
	if err != nil || !r.Changed || r.Apply != nil {
		t.Fatalf("first refresh: %+v %v", r, err)
	}
	if hist, _ := h.history(); len(hist) != 0 {
		t.Errorf("history: %+v", hist)
	}
	// A second refresh of the same tailnet is not a change.
	if r, err := h.e.RefreshTailnet(ctx); err != nil || r.Changed {
		t.Errorf("second refresh: %+v %v", r, err)
	}
}

// After an apply composed without tailnet data (tailscaled unreadable, so
// no cache was written), the first successful refresh re-applies.
func TestTailnetFirstRefreshAfterApplyReapplies(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	status, _ := h.tailnet.Status(ctx)
	h.tailnet.mu.Lock()
	h.tailnet.err = errors.New("tailscaled not running")
	h.tailnet.mu.Unlock()
	h.apply("initial")
	if bytes.Contains(h.disk(), []byte("DOMAIN,nas,DIRECT")) {
		t.Fatal("peers composed although tailscale was unreadable")
	}
	h.tailnet.mu.Lock()
	h.tailnet.raw, h.tailnet.err = status, nil
	h.tailnet.mu.Unlock()
	r, err := h.e.RefreshTailnet(ctx)
	if err != nil || !r.Changed || r.Apply == nil || !r.Apply.Reloaded {
		t.Fatalf("refresh: %+v %v", r, err)
	}
	if !bytes.Contains(h.disk(), []byte("DOMAIN,nas,DIRECT")) {
		t.Error("peers not added")
	}
}

// Concurrent refreshes (poll, network change, tailnet.refresh) of a new
// helper report the first sight once, not once each.
func TestTailnetConcurrentRefreshOneChange(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	changed := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := h.e.RefreshTailnet(ctx)
			if err != nil {
				t.Error(err)
			}
			if r.Changed {
				mu.Lock()
				changed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if changed != 1 {
		t.Errorf("%d refreshes reported a change", changed)
	}
}
