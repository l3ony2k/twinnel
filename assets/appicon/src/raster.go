//go:build ignore

// raster renders every bitmap icon from its SVG master with MyGo's own SVG renderer (the same
// one the app draws its icons with), so no browser or image tool is needed:
//
//	go run ./assets/appicon/src/raster.go
//
// Inputs:  assets/appicon/src/{mac,win,win-small,linux}.svg (appicon.mjs writes them)
//
//	assets/icons/tray/svg/*.svg (assets/icons/src/export.mjs writes them)
//
// Outputs: see assets/appicon/README.md.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	appSrc  = "assets/appicon/src"
	appOut  = "assets/appicon"
	trayDir = "assets/icons/tray"
)

var states = []string{"duet", "duet-warn", "duet-error", "duet-unknown", "duet-tsdown", "duet-mode", "duet-nohelper"}

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fail(fmt.Errorf("run from the repo root"))
	}
	mac, win, winSmall, linux := load(appSrc+"/mac.svg", ""), load(appSrc+"/win.svg", ""), load(appSrc+"/win-small.svg", ""), load(appSrc+"/linux.svg", "")

	// macOS .icns (PNG entries) and the 1024 master.
	icns := []struct {
		typ  string
		size int
	}{{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
		{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512}}
	var ents []entry
	for _, e := range icns {
		ents = append(ents, entry{e.typ, e.size, pngOf(mac.Image(e.size, e.size, nil))})
	}
	write(appOut+"/macos/duet.icns", icnsOf(ents))
	write(appOut+"/macos/duet-1024.png", pngOf(mac.Image(1024, 1024, nil)))
	// The flat master for mygo.json "icon" (every platform but macOS, which takes the .icns).
	write(appOut+"/duet-1024.png", pngOf(win.Image(1024, 1024, nil)))

	// Windows .ico: the small drawing up to 32 px, the full one above.
	var ico []entry
	for _, n := range []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256} {
		src := win
		if n <= 32 {
			src = winSmall
		}
		ico = append(ico, entry{"", n, pngOf(src.Image(n, n, nil))})
	}
	write(appOut+"/windows/duet.ico", icoOf(ico))

	// Linux hicolor theme.
	for _, n := range []int{16, 22, 24, 32, 48, 64, 128, 256, 512} {
		write(fmt.Sprintf("%s/linux/hicolor/%dx%d/apps/duet.png", appOut, n, n), pngOf(linux.Image(n, n, nil)))
	}
	copyFile(appSrc+"/linux.svg", appOut+"/linux/hicolor/scalable/apps/duet.svg")

	// Menu bar (macOS template images): 18 pt tall (mygo sizes tray images to 18 pt), @2x and @1x.
	for _, s := range states {
		wide := load(trayDir+"/svg/"+s+".svg", "")
		w, h := wide.Size()
		for _, sc := range []struct {
			suffix string
			px     int
		}{{"", 36}, {"@1x", 18}} {
			pw := int(math.Round(float64(w) / float64(h) * float64(sc.px)))
			write(fmt.Sprintf("%s/%s%s.png", trayDir, s, sc.suffix), pngOf(wide.Image(pw, sc.px, nil)))
		}
		// Windows notification area: monochrome, white for the dark taskbar, black for light.
		for _, v := range []struct{ name, color string }{{"dark", "#FFFFFF"}, {"light", "#000000"}} {
			sq := load(trayDir+"/svg/"+s+"-sq.svg", v.color)
			var e []entry
			for _, n := range []int{16, 20, 24, 32, 40, 48} {
				e = append(e, entry{"", n, pngOf(sq.Image(n, n, nil))})
			}
			write(fmt.Sprintf("%s/windows/%s-%s.ico", trayDir, s, v.name), icoOf(e))
		}
		// Linux: symbolic icons (GTK recolours #2e3436-family symbolic SVGs with the theme).
		b, err := os.ReadFile(trayDir + "/svg/" + s + "-sq.svg")
		check(err)
		write(fmt.Sprintf("%s/linux/%s-symbolic.svg", trayDir, s), bytes.ReplaceAll(b, []byte("#000000"), []byte("#2e3436")))
	}
	b, err := os.ReadFile(trayDir + "/svg/duet-sq.svg")
	check(err)
	write(appOut+"/linux/hicolor/symbolic/apps/duet-symbolic.svg", bytes.ReplaceAll(b, []byte("#000000"), []byte("#2e3436")))
	fmt.Println("done")
}

// load parses an SVG, recolouring black to col when col is set.
func load(path, col string) *ui.SVG {
	b, err := os.ReadFile(path)
	check(err)
	if col != "" {
		b = []byte(strings.ReplaceAll(string(b), "#000000", col))
	}
	s := ui.MustParseSVG(b)
	if u := s.Unsupported(); len(u) > 0 {
		fail(fmt.Errorf("%s uses %v, which the renderer skips", path, u))
	}
	return s
}

type entry struct {
	typ  string
	size int
	png  []byte
}

func pngOf(img image.Image) []byte {
	var buf bytes.Buffer
	check(png.Encode(&buf, img))
	return buf.Bytes()
}

// icnsOf writes an Apple icon family: "icns", total length, then (type, length, PNG) entries.
func icnsOf(es []entry) []byte {
	var body bytes.Buffer
	for _, e := range es {
		body.WriteString(e.typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(e.png)))
		body.Write(e.png)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// icoOf writes a Windows icon with PNG-compressed images (Vista and later read them at every size).
func icoOf(es []entry) []byte {
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(es))})
	off := 6 + 16*len(es)
	for _, e := range es {
		d := byte(e.size)
		if e.size >= 256 {
			d = 0
		}
		out.Write([]byte{d, d, 0, 0})
		binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(e.png)), uint32(off)})
		off += len(e.png)
	}
	for _, e := range es {
		out.Write(e.png)
	}
	return out.Bytes()
}

func write(path string, b []byte) {
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	check(os.WriteFile(path, b, 0o644))
}

func copyFile(from, to string) {
	b, err := os.ReadFile(from)
	check(err)
	write(to, b)
}

func check(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "raster:", err)
	os.Exit(1)
}
