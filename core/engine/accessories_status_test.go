package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// TestGeoStatusOffline: geo.status answers from the disk and the last
// check alone (no request), and keeps the check's time and answer.
func TestGeoStatusOffline(t *testing.T) {
	h, g := accessoryHarness(t)
	ctx := context.Background()
	s, err := h.e.GeoStatus()
	if err != nil || len(s.Files) != 4 || s.Checked || !s.CheckedAt.IsZero() || s.AutoUpdate != rpc.GeoAutoOff {
		t.Fatalf("fresh status: %+v %v", s, err)
	}
	for _, f := range s.Files {
		if f.Installed || !f.UpdateAvailable {
			t.Errorf("fresh %+v", f)
		}
	}
	if g.apiHits != 0 {
		t.Fatalf("status asked GitHub %d times", g.apiHits)
	}
	if _, err := h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{}); err != nil {
		t.Fatal(err)
	}
	hits := g.apiHits
	s, _ = h.e.GeoStatus()
	if g.apiHits != hits {
		t.Fatal("status after update asked GitHub")
	}
	if !s.Checked || s.CheckedAt.IsZero() {
		t.Errorf("checked: %+v", s)
	}
	for _, f := range s.Files {
		if !f.Installed || f.UpdateAvailable || f.Version != "v1" || f.Source == "" || f.ModTime.IsZero() || f.Remote == "" {
			t.Errorf("installed %+v", f)
		}
	}
	// A new release seen by a check shows in the status afterwards.
	g.set("geoip.dat", []byte("\x0a\x05geoip-v2"))
	if _, err := h.e.GeoCheck(ctx, rpc.AccessoryCheckParams{}); err != nil {
		t.Fatal(err)
	}
	s, _ = h.e.GeoStatus()
	if !s.Files[0].UpdateAvailable || s.Files[1].UpdateAvailable {
		t.Errorf("after new release: %+v", s.Files[:2])
	}
	// A file replaced by hand is no longer Twinnel's record.
	os.WriteFile(s.Files[1].File, []byte("\x0a\x05by-hand"), 0o644)
	s, _ = h.e.GeoStatus()
	if s.Files[1].Source != "" || s.Files[1].Version != "" || !s.Files[1].Installed {
		t.Errorf("by hand %+v", s.Files[1])
	}
}

// TestGeoUpdateProgressAndReload: the stages come as events in order,
// with byte counts; Reload has the adopted mihomo load the config again.
func TestGeoUpdateProgressAndReload(t *testing.T) {
	h, _ := accessoryHarness(t)
	ctx := context.Background()
	h.apply("config on disk")
	h.e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Adopted = &store.Adoption{PlanID: "p1", At: time.Now()}
		return nil
	})
	_, seq := h.e.Bus.Since(0)
	reloads, _, _ := h.ctrl.State()
	r, err := h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{Reload: true, Proxy: rpc.ProxyDirect})
	if err != nil || !r.Reloaded || r.ReloadNeeded || r.Via != "direct" {
		t.Fatalf("update: %+v %v", r, err)
	}
	after, _, _ := h.ctrl.State()
	if len(after) != len(reloads)+1 {
		t.Errorf("reloads %d -> %d", len(reloads), len(after))
	}
	evs, _ := h.e.Bus.Since(seq)
	var stages []string
	var lastDone, total int64
	for _, e := range evs {
		if e.Type != rpc.EventAccessoryProgress {
			continue
		}
		var p rpc.AccessoryProgress
		json.Unmarshal(e.Data, &p)
		if p.Kind != rpc.AccessoryGeo {
			t.Errorf("kind %q", p.Kind)
		}
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
		lastDone, total = p.Done, p.Total
	}
	if want := []string{"download", "install", "verify", "reload"}; !slices.Equal(stages, want) {
		t.Errorf("stages %v, want %v", stages, want)
	}
	if total == 0 || lastDone != total {
		t.Errorf("bytes %d/%d", lastDone, total)
	}

	// Not adopted and not in the state dir: no reload, and it says why.
	h.e.Store.UpdateSettings(func(st *store.Settings) error { st.Adopted = nil; return nil })
	r, err = h.e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{Reload: true, Force: true})
	if err != nil || r.Reloaded || !r.ReloadNeeded || r.ReloadError == "" {
		t.Fatalf("unmanaged: %+v %v", r, err)
	}
}

// TestGeoUpdateFromValidateCache: a database mihomo -t already
// downloaded into the validation cache is copied, not downloaded again.
func TestGeoUpdateFromValidateCache(t *testing.T) {
	h, g := accessoryHarness(t)
	cache := h.e.Store.Path("validate", "geo")
	os.MkdirAll(cache, 0o700)
	g.mu.Lock()
	site := g.files["geosite.dat"]
	g.files["geosite.dat"] = []byte("\x0a\x05would-not-match")
	g.mu.Unlock()
	os.WriteFile(filepath.Join(cache, "GeoSite.dat"), site, 0o444)
	r, err := h.e.GeoUpdate(context.Background(), rpc.AccessoryUpdateParams{})
	if err != nil || len(r.Updated) != 4 {
		t.Fatalf("update: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.confDir, "GeoSite.dat")); string(b) != string(site) {
		t.Errorf("geosite %q, want the cached copy", b)
	}
}

// TestGeoAutoUpdate: the weekly schedule runs when the last update is a
// week old, and not when it is off.
func TestGeoAutoUpdate(t *testing.T) {
	h, _ := accessoryHarness(t)
	ctx := context.Background()
	h.e.geoAutoDue(ctx)
	if s, _ := h.e.GeoStatus(); s.Files[0].Installed {
		t.Fatal("ran while off")
	}
	off := rpc.GeoAutoWeekly
	if _, err := h.e.UpdateSettings(rpc.SettingsUpdate{GeoAutoUpdate: &off}); err != nil {
		t.Fatal(err)
	}
	h.e.geoAutoDue(ctx)
	s, _ := h.e.GeoStatus()
	if !s.Files[0].Installed || s.LastAuto.IsZero() || s.AutoUpdate != rpc.GeoAutoWeekly {
		t.Fatalf("weekly: %+v", s)
	}
	last := s.LastAuto
	h.e.geoAutoDue(ctx)
	if s, _ := h.e.GeoStatus(); !s.LastAuto.Equal(last) {
		t.Error("ran again within the week")
	}
	bad := "daily"
	if _, err := h.e.UpdateSettings(rpc.SettingsUpdate{GeoAutoUpdate: &bad}); err == nil {
		t.Error("accepted daily")
	}
}

// TestUIInstallWithoutExternalUI: installing the dashboard into a config
// without external-ui adds `external-ui: ui` and applies it.
func TestUIInstallWithoutExternalUI(t *testing.T) {
	h, _ := accessoryHarness(t)
	ctx := context.Background()
	h.apply("first")
	s, _ := h.e.UIStatus()
	if s.Configured || s.Files[0].Installed || !s.Files[0].UpdateAvailable || s.Path != "/ui/" {
		t.Fatalf("status: %+v", s)
	}
	r, err := h.e.UIUpdate(ctx, rpc.AccessoryUpdateParams{})
	if err != nil || !r.Configured || len(r.Updated) != 1 {
		t.Fatalf("install: %+v %v", r, err)
	}
	if !strings.Contains(string(h.disk()), "external-ui: ui") {
		t.Errorf("config has no external-ui:\n%s", h.disk())
	}
	s, _ = h.e.UIStatus()
	if !s.Configured || !s.Files[0].Installed || s.Files[0].UpdateAvailable || s.Files[0].Version != "v1" {
		t.Fatalf("after: %+v", s)
	}
	// A config that sets its own external-ui keeps it.
	h.e.PutLayer(rpc.LayerPutParams{ID: "ui", Content: "external-ui: dash\n"})
	h.apply("own ui")
	if d := string(h.disk()); !strings.Contains(d, "external-ui: dash") || strings.Contains(d, "external-ui: ui\n") {
		t.Errorf("own external-ui:\n%s", d)
	}
}
