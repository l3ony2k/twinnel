package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/status"
)

func sample(up, down int64) status.TrafficSample {
	return status.TrafficSample{Traffic: mihomo.Traffic{Up: up, Down: down}}
}

func TestTrayLookFor(t *testing.T) {
	if !plat.trayTitle {
		t.Skip("no menu bar title on this platform")
	}
	s := status.Snapshot{}
	s.Mihomo.Reachable = true
	s.Mihomo.Traffic = sample(3<<20, 512*1024)
	if l := trayLookFor(s, status.Config{TraySpeedLayout: status.TraySpeedOneLine}); l.title != status.TrayTitle(s.Mihomo.Traffic) || l.up != "" {
		t.Errorf("one line: %+v", l)
	}
	l := trayLookFor(s, status.Config{TraySpeedLayout: status.TraySpeedTwoLines})
	if l.title != "" || l.up != "↑   3M" || l.down != "↓ 512K" {
		t.Errorf("two lines: %+v", l)
	}
	if l := trayLookFor(s, status.Config{}); l.title != "" || l.up == "" {
		t.Errorf("default should be two lines: %+v", l)
	}
	for _, cfg := range []status.Config{{TraySpeedOff: true}, {TraySpeedOff: true, TraySpeedLayout: status.TraySpeedTwoLines}} {
		if l := trayLookFor(s, cfg); l.title != "" || l.up != "" || l.down != "" {
			t.Errorf("off %+v: %+v", cfg, l)
		}
	}
	s.Mihomo.Reachable = false
	if l := trayLookFor(s, status.Config{}); l.title != "" || l.up != "" {
		t.Errorf("unreachable: %+v", l)
	}
}

// The two-line image keeps one size whatever the rates, and is a template:
// black, only its alpha varies.
func TestTraySpeedImage(t *testing.T) {
	icon := trayIcon(status.TrayOK)
	var size image.Point
	for i, v := range [][2]int64{{0, 0}, {999, 1000}, {512 * 1024, 3 << 20}, {999 << 30, 1 << 40}} {
		up, down := status.TrayUp(sample(v[0], v[1])), status.TrayDown(sample(v[0], v[1]))
		img, err := png.Decode(bytes.NewReader(traySpeedImage(icon, up, down)))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		if b.Dy() != 36 {
			t.Errorf("height %d, want 36 (18 pt @2x)", b.Dy())
		}
		if i == 0 {
			size = b.Size()
		} else if b.Size() != size {
			t.Errorf("%v: size %v, want %v (no jitter)", v, b.Size(), size)
		}
		ink := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if c.A > 0 && (c.R|c.G|c.B) != 0 {
					t.Fatalf("pixel %d,%d is %v: not black", x, y, c)
				}
				if x > 41 && c.A > 128 {
					ink++
				}
			}
		}
		if ink < 50 {
			t.Errorf("%v: %d text pixels, the lines did not draw", v, ink)
		}
	}
	// Same rates, same bytes: SetIcon is skipped by syncTray only on
	// equal strings, but the image must not depend on anything else.
	a := traySpeedImage(icon, "↑ 512K", "↓   3M")
	b := traySpeedImage(icon, "↑ 512K", "↓   3M")
	if !bytes.Equal(a, b) {
		t.Error("the same lines drew two images")
	}
}

// TestTraySpeedShots writes the menu bar item in both layouts at several
// rates, on a light and a dark bar, for review (the real menu bar cannot
// be captured without showing it). TWINNEL_TRAY_SHOTS=<dir>.
func TestTraySpeedShots(t *testing.T) {
	dir := os.Getenv("TWINNEL_TRAY_SHOTS")
	if dir == "" {
		t.Skip("TWINNEL_TRAY_SHOTS not set")
	}
	rates := [][2]int64{{0, 0}, {999, 1000}, {60 * 1024, 512 * 1024}, {3 << 20, 87 << 20}, {120 << 20, 1 << 30}}
	icon := trayIcon(status.TrayOK)
	var rows [][2]image.Image // one line (approximate: AppKit draws it), two lines
	for _, r := range rates {
		s := sample(r[0], r[1])
		one := oneLineApprox(icon, status.TrayTitle(s))
		two, err := png.Decode(bytes.NewReader(traySpeedImage(icon, status.TrayUp(s), status.TrayDown(s))))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, [2]image.Image{one, two})
	}
	for _, bar := range []struct {
		name   string
		bg, fg color.NRGBA
	}{
		{"light", color.NRGBA{0xEC, 0xEC, 0xEC, 255}, color.NRGBA{0, 0, 0, 0xE0}},
		{"dark", color.NRGBA{0x26, 0x26, 0x2A, 255}, color.NRGBA{255, 255, 255, 255}},
	} {
		const pad, rowH = 12, 44
		w := 0
		for _, r := range rows {
			w = max(w, r[0].Bounds().Dx(), r[1].Bounds().Dx())
		}
		sheet := image.NewNRGBA(image.Rect(0, 0, 2*w+3*pad*2, len(rows)*rowH+pad))
		draw.Draw(sheet, sheet.Bounds(), &image.Uniform{bar.bg}, image.Point{}, draw.Src)
		for i, r := range rows {
			for j, img := range r {
				at := image.Pt(pad*2+j*(w+pad*2), pad/2+i*rowH+(rowH-img.Bounds().Dy())/2)
				draw.DrawMask(sheet, img.Bounds().Sub(img.Bounds().Min).Add(at), &image.Uniform{bar.fg}, image.Point{}, img, img.Bounds().Min, draw.Over)
			}
		}
		f, err := os.Create(filepath.Join(dir, "tray-speed-"+bar.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, sheet); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

// oneLineApprox is the icon with the one-line title beside it, drawn as
// AppKit draws a status item title: the monospaced system font at the menu
// bar's 13 pt, 4 pt after the icon. Only for review.
func oneLineApprox(iconPNG []byte, title string) image.Image {
	icon, _ := png.Decode(bytes.NewReader(iconPNG))
	const w = 120 // pt
	img := ui.Render(func(c *ui.Context) {
		ui.Column(c).Background(ui.RGB(255, 255, 255)).Width(w).Height(18).Justify(ui.Center).Children(func() {
			ui.Text(c, title).Font("monospace").FontSize(13).TextColor(ui.RGB(0, 0, 0)).SingleLine()
		})
	}, w, 18, trayScale)
	ib := icon.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, ib.Dx()+8+inkRightRGBA(img), 36))
	draw.Draw(out, ib.Sub(ib.Min), icon, ib.Min, draw.Src)
	for y := 0; y < 36; y++ {
		for x := 0; x < out.Rect.Dx()-ib.Dx()-8; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if a := uint8(255 - r>>8); a > 0 {
				out.SetNRGBA(ib.Dx()+8+x, y, color.NRGBA{A: a})
			}
		}
	}
	return out
}

func inkRightRGBA(img *image.RGBA) int {
	b := img.Bounds()
	for x := b.Max.X - 1; x >= 0; x-- {
		for y := 0; y < b.Max.Y; y++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 200 {
				return x + 1
			}
		}
	}
	return 0
}
