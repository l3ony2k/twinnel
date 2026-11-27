package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/status"
)

// The traffic rate beside the menu bar icon (macOS). One line is the
// status item's own title, drawn by AppKit in the monospaced system font.
// Two lines (upload over download, as iStat Menus and Stats show them)
// cannot be a title, which is one line at the menu bar's font size: the
// icon and both lines are drawn into one template image instead, black on
// transparent, which macOS tints for the bar and the highlight like the
// icon alone.
//
// The lines are set from a glyph atlas drawn once (ui.Render, headless, in
// the system's monospaced font): each frame only copies glyph cells in Go,
// so the once-a-second update makes no native objects and holds no memory
// beyond the PNG it hands to the status item.

// menuBarCard is the settings card for the menu bar item.
func (a *app) menuBarCard(c *ui.Context) {
	T := i18n.T
	t := c.Theme()
	on := !a.cfg.TraySpeedOff
	layout := 0
	if a.cfg.TraySpeedLayout == status.TraySpeedTwoLines {
		layout = 1
	}
	card(c, T.MenuBar, func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.TraySpeedSwitch).Grow(1)
			if ui.Switch(c.Key("tray-speed"), &on).Label(T.TraySpeedSwitch).Changed() {
				a.setTraySpeed(!on, a.cfg.TraySpeedLayout)
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			label := ui.Text(c, T.TraySpeedLayout).Grow(1)
			if !on {
				label.TextColor(t.TextMuted)
			}
			if ui.Segmented(c.Key("tray-layout"), &layout, T.TraySpeedOneLine, T.TraySpeedTwo).
				Label(T.TraySpeedLayout).Disabled(!on).Changed() {
				v := ""
				if layout == 1 {
					v = status.TraySpeedTwoLines
				}
				a.setTraySpeed(a.cfg.TraySpeedOff, v)
			}
		})
	})
}

// setTraySpeed saves the menu bar rate settings and shows them at once.
// Main thread.
func (a *app) setTraySpeed(off bool, layout string) {
	cfg := a.cfg
	cfg.TraySpeedOff, cfg.TraySpeedLayout = off, layout
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		a.settings.msg, a.settings.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	a.cfg = cfg
	if a.mon != nil {
		a.syncTray(a.mon.Snapshot())
	}
}

// trayLook is what the status item shows: the title, and the image by
// the icon's shape and, in two lines, the lines drawn beside it.
type trayLook struct {
	title    string
	shape    status.TrayIcon
	up, down string // two lines; "" in one line or off
}

// trayLookFor is the menu bar item for a snapshot and the settings.
func trayLookFor(s status.Snapshot, cfg status.Config) trayLook {
	l := trayLook{shape: status.TrayIconFor(s)}
	if !s.Mihomo.Reachable || cfg.TraySpeedOff || !plat.trayTitle {
		return l
	}
	if cfg.TraySpeedLayout == status.TraySpeedTwoLines {
		l.up, l.down = status.TrayUp(s.Mihomo.Traffic), status.TrayDown(s.Mihomo.Traffic)
		return l
	}
	l.title = status.TrayTitle(s.Mihomo.Traffic)
	return l
}

// syncTray updates the status item to s, changing only what differs from
// what it shows. Main thread.
func (a *app) syncTray(s status.Snapshot) {
	if a.tray == nil { // shot mode
		return
	}
	l := trayLookFor(s, a.cfg)
	if l.shape != a.trayLook.shape || l.up != a.trayLook.up || l.down != a.trayLook.down || !a.trayShown {
		img := trayIcon(l.shape)
		if l.up != "" {
			img = traySpeedImage(img, l.up, l.down)
		}
		a.tray.SetIcon(img, true)
	}
	if l.title != a.trayLook.title || !a.trayShown {
		a.tray.SetTitle(l.title)
	}
	a.trayLook, a.trayShown = l, true
}

// Two-line metrics, in pixels at 2× (the PNG is 36 px = 18 pt tall; macOS
// halves it on Retina and scales it on 1× screens, as the icon).
const (
	traySpeedFont = 9  // pt: two lines in the 18 pt of the bar
	trayIconGap   = 6  // px between the icon and the text
	trayArrowGap  = 5  // px between an arrow and its number: 2.5 pt, under a space
	trayLineH     = 18 // px a line
	trayScale     = 2  // device pixels per point
	trayGlyphs    = "0123456789BKMGT↑↓"
	trayRateChars = 4    // status.RateCompact's width
	trayInk       = 0.45 // below this coverage a pixel is antialiasing noise
)

// glyphAtlas holds each glyph of trayGlyphs as coverage (alpha) in a cell
// of one monospaced advance.
type glyphAtlas struct {
	cell   int // px, the font's advance at 2×
	glyphs map[rune]*image.Alpha
}

var (
	atlasOnce sync.Once
	atlas     *glyphAtlas
)

func trayAtlas() *glyphAtlas {
	atlasOnce.Do(func() { atlas = buildAtlas() })
	return atlas
}

// renderCoverage draws text black on white in a box w×h pt (the text
// aligned as align, centered vertically) and returns its coverage.
func renderCoverage(text string, w, h float32, align ui.Align) *image.Alpha {
	img := ui.Render(func(c *ui.Context) {
		ui.Column(c).Background(ui.RGB(255, 255, 255)).Width(w).Height(h).Justify(ui.Center).Children(func() {
			ui.Text(c, text).Font("monospace").FontSize(traySpeedFont).FontWeight(500).
				TextColor(ui.RGB(0, 0, 0)).TextAlign(align).SingleLine().Width(w)
		})
	}, int(math.Ceil(float64(w))), int(math.Ceil(float64(h))), trayScale)
	b := img.Bounds()
	out := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			lum := (r + g + bl) / 3 >> 8
			out.SetAlpha(x, y, color.Alpha{A: uint8(255 - lum)})
		}
	}
	return out
}

// inkRight is one past the rightmost column with ink, 0 when none.
func inkRight(a *image.Alpha) int {
	b := a.Bounds()
	for x := b.Max.X - 1; x >= b.Min.X; x-- {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if float64(a.AlphaAt(x, y).A) > 255*trayInk {
				return x + 1
			}
		}
	}
	return 0
}

func buildAtlas() *glyphAtlas {
	// The advance: how far the ink of "0000000000" reaches past that of
	// "0", over the nine advances between them.
	const n = 10
	one := inkRight(renderCoverage("0", 100, 9, ui.Start))
	ten := inkRight(renderCoverage(strings.Repeat("0", n), 100, 9, ui.Start))
	adv := float32(ten-one) / (n - 1) // px
	if adv <= 0 {
		adv = traySpeedFont * 0.6 * trayScale // SF Mono's advance, as a fallback
	}
	cell := int(math.Round(float64(adv)))
	at := &glyphAtlas{cell: cell, glyphs: map[rune]*image.Alpha{}}
	for _, r := range trayGlyphs {
		at.glyphs[r] = renderCoverage(string(r), float32(cell)/trayScale, trayLineH/trayScale, ui.Center)
	}
	return at
}

// lineWidth is the width in px of one line: an arrow, the gap, the rate.
func (at *glyphAtlas) lineWidth() int { return at.cell*(1+trayRateChars) + trayArrowGap }

// drawLine draws a line ("↑   3M": an arrow, a gap, the rate) at x, y.
func (at *glyphAtlas) drawLine(dst *image.Alpha, x, y int, line string) {
	for i, r := range []rune(strings.ReplaceAll(line, status.ArrowGap, "")) {
		cx := x + i*at.cell
		if i > 0 {
			cx += trayArrowGap
		}
		g := at.glyphs[r]
		if g == nil { // a space, or a glyph not in the atlas
			continue
		}
		draw.Draw(dst, image.Rect(cx, y, cx+g.Bounds().Dx(), y+g.Bounds().Dy()), g, g.Bounds().Min, draw.Over)
	}
}

// traySpeedImage is the icon (a template PNG 36 px tall) with the two
// lines beside it, upload over download, as one template PNG.
func traySpeedImage(iconPNG []byte, up, down string) []byte {
	icon, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return iconPNG
	}
	at := trayAtlas()
	ib := icon.Bounds()
	tx := ib.Dx() + trayIconGap
	cov := image.NewAlpha(image.Rect(0, 0, tx+at.lineWidth(), 2*trayLineH))
	at.drawLine(cov, tx, 0, up)
	at.drawLine(cov, tx, trayLineH, down)
	out := image.NewNRGBA(cov.Bounds())
	draw.Draw(out, ib.Sub(ib.Min).Add(image.Pt(0, (2*trayLineH-ib.Dy())/2)), icon, ib.Min, draw.Src)
	for y := 0; y < cov.Rect.Dy(); y++ {
		for x := tx; x < cov.Rect.Dx(); x++ {
			if a := cov.AlphaAt(x, y).A; a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{A: a})
			}
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, out); err != nil {
		return iconPNG
	}
	return buf.Bytes()
}
