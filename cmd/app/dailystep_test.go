package main

import (
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/status"
)

// The first everyday operation is saved once, and 关于 → 入门's last step
// reads it.
func TestMarkDailyUsed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.json")
	a := &app{cfgPath: p}
	if a.cfg.DailyUsed {
		t.Fatal("new config already marks the daily step")
	}
	a.markDailyUsed()
	if !a.cfg.DailyUsed {
		t.Fatal("markDailyUsed did not set the flag")
	}
	got, err := status.LoadConfig(p, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if !got.DailyUsed {
		t.Fatal("flag not saved")
	}
	shot := &app{cfgPath: filepath.Join(t.TempDir(), "x.json"), shotMode: true}
	shot.markDailyUsed()
	if shot.cfg.DailyUsed {
		t.Fatal("shot runs must not save")
	}
}
