package main

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/store"
)

// TestGeoAndDashboardCommands runs the read-only commands against a
// scratch helper (no network: status reads only the disk).
func TestGeoAndDashboardCommands(t *testing.T) {
	sock, e := importHelper(t)
	conf := t.TempDir()
	e.Store.UpdateSettings(func(st *store.Settings) error {
		st.Mihomo.ConfigDir = conf
		return nil
	})
	out := withIO(t, "")
	if err := runGeo([]string{"status", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "checked: never") || !strings.Contains(s, "GeoSite.dat") || !strings.Contains(s, "missing") {
		t.Errorf("geo status:\n%s", s)
	}
	out.Reset()
	if err := runDashboard([]string{"status", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "served:  false") {
		t.Errorf("dashboard status:\n%s", s)
	}
	if err := runDashboard([]string{"open", "--socket", sock, "--print-url"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("open without install: %v", err)
	}
	if err := runGeo([]string{"auto", "daily", "--socket", sock}); err == nil {
		t.Error("geo auto daily accepted")
	}
	out.Reset()
	if err := runGeo([]string{"auto", "weekly", "--socket", sock}); err != nil || !strings.Contains(out.String(), "weekly") {
		t.Errorf("geo auto weekly: %v %s", err, out.String())
	}
	if err := runGeo([]string{"nope", "--socket", sock}); err == nil {
		t.Error("unknown subcommand accepted")
	}
}
