package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/status"
)

// Every tray state has a Windows mark whose entries are square PNGs, and
// icoPNG picks the size asked for, the next larger, or the largest.
func TestWindowsTrayMarks(t *testing.T) {
	states := []status.TrayIcon{status.TrayOK, status.TrayWarn, status.TrayError, status.TrayUnknown, status.TrayTSDown, status.TrayMode, status.TrayNoHelper}
	for _, st := range states {
		b, err := os.ReadFile(filepath.Join("..", "..", "assets", "icons", "tray", "windows", string(st)+"-light.ico"))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct{ ask, want int }{{16, 16}, {20, 20}, {24, 24}, {32, 32}, {40, 40}, {48, 48}, {28, 32}, {12, 16}, {64, 48}} {
			img, err := icoPNG(b, c.ask)
			if err != nil {
				t.Fatalf("%s %d: %v", st, c.ask, err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(img))
			if err != nil {
				t.Fatal(err)
			}
			if m, err := png.Decode(bytes.NewReader(img)); err == nil {
				r := m.Bounds()
				for y := r.Min.Y; y < r.Max.Y; y++ {
					for x := r.Min.X; x < r.Max.X; x++ {
						if cr, cg, cb, ca := m.At(x, y).RGBA(); ca > 0 && (cr|cg|cb) != 0 {
							t.Fatalf("%s %d: pixel %d,%d is not black: a template is black on transparent", st, c.ask, x, y)
						}
					}
				}
			}
			if cfg.Width != c.want || cfg.Height != c.want {
				t.Errorf("%s: asked %d, got %d×%d, want %d", st, c.ask, cfg.Width, cfg.Height, c.want)
			}
		}
	}
	if _, err := icoPNG([]byte("nope"), 16); err == nil {
		t.Error("garbage accepted")
	}
}

func TestTrayIconPx(t *testing.T) {
	for dpi, want := range map[int]int{0: 16, 96: 16, 120: 20, 144: 24, 192: 32, 240: 40, 288: 48} {
		if got := trayIconPx(dpi); got != want {
			t.Errorf("dpi %d: %d, want %d", dpi, got, want)
		}
	}
}
