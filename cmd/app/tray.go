package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/l3ony2k/duet/core/status"
)

// trayIcon draws the placeholder menu bar icon, black on transparent for
// macOS to tint: two overlapping rings (mihomo and Tailscale). A warning
// adds a ring in the top right corner, an error a filled dot there. Real
// icons come later (assets/icons).
func trayIcon(l status.Level) []byte {
	const n = 32
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	ink := func(x, y int, a float64) {
		if a <= 0 {
			return
		}
		old := img.NRGBAAt(x, y).A
		v := uint8(math.Min(255, float64(old)+a*255))
		img.SetNRGBA(x, y, color.NRGBA{A: v})
	}
	// ring covers a pixel by how much of it is within the stroke, with
	// four by four samples for smooth edges.
	ring := func(cx, cy, r, w float64, filled bool) func(x, y float64) float64 {
		return func(x, y float64) float64 {
			d := math.Hypot(x-cx, y-cy)
			if filled {
				if d <= r {
					return 1
				}
				return 0
			}
			if math.Abs(d-r) <= w/2 {
				return 1
			}
			return 0
		}
	}
	shapes := []func(x, y float64) float64{
		ring(11.5, 17, 8, 2.6, false),
		ring(20.5, 17, 8, 2.6, false),
	}
	cut := func(x, y float64) bool { return false }
	switch l {
	case status.LevelWarn:
		shapes = append(shapes, ring(26, 6, 4, 2.2, false))
		cut = func(x, y float64) bool { return math.Hypot(x-26, y-6) < 6.3 }
	case status.LevelError:
		shapes = append(shapes, ring(26, 6, 5, 0, true))
		cut = func(x, y float64) bool { return math.Hypot(x-26, y-6) < 7.3 && math.Hypot(x-26, y-6) > 5 }
	}
	for y := range n {
		for x := range n {
			cov := 0.0
			for sy := range 4 {
				for sx := range 4 {
					px, py := float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4
					if cut(px, py) {
						continue
					}
					for _, s := range shapes {
						if s(px, py) > 0 {
							cov += 1.0 / 16
							break
						}
					}
				}
			}
			ink(x, y, cov)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
