package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/twinnel/core/status"
)

// The 16 px frame of each Windows tray mark (100 % scaling) is drawn on
// whole pixels (assets/icons/tray/windows/frames16.go): no grey from a
// downscale, which blurred the rings and badges (review 5.4), and every
// state differs from every other by a shape of several pixels.
func TestWindowsTrayMarks16(t *testing.T) {
	states := []status.TrayIcon{status.TrayOK, status.TrayWarn, status.TrayError, status.TrayUnknown, status.TrayTSDown, status.TrayMode, status.TrayNoHelper}
	for _, variant := range []string{"light", "dark"} {
		masks := map[status.TrayIcon][256]bool{}
		for _, st := range states {
			b, err := os.ReadFile(filepath.Join("..", "..", "assets", "icons", "tray", "windows", string(st)+"-"+variant+".ico"))
			if err != nil {
				t.Fatal(err)
			}
			img, err := icoPNG(b, 16)
			if err != nil {
				t.Fatal(err)
			}
			m, err := png.Decode(bytes.NewReader(img))
			if err != nil {
				t.Fatal(err)
			}
			if r := m.Bounds(); r.Dx() != 16 || r.Dy() != 16 {
				t.Fatalf("%s-%s: %v, want 16×16", st, variant, r)
			}
			var mask [256]bool
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					_, _, _, a := m.At(x, y).RGBA()
					if a != 0 && a != 0xffff {
						t.Fatalf("%s-%s: pixel %d,%d is partly covered (alpha %d): the 16 px frame is drawn on whole pixels", st, variant, x, y, a>>8)
					}
					mask[y*16+x] = a != 0
				}
			}
			for other, om := range masks {
				diff := 0
				for i := range mask {
					if mask[i] != om[i] {
						diff++
					}
				}
				if diff < 6 {
					t.Errorf("%s and %s (%s) differ in %d pixels at 16 px", st, other, variant, diff)
				}
			}
			masks[st] = mask
		}
	}
}
