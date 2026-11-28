//go:build ignore

// frames16 writes the 16 px frame of every Windows tray mark by hand, on
// the pixel grid, into the .ico files raster.go renders from the SVG
// masters (assets/appicon/src/raster.go): at 16 px (100 % scaling) a
// downscale of the 24-grid drawing blurs the rings to grey and the badges
// to smudges. Run it after raster.go, from the repo root:
//
//	go run ./assets/appicon/src/raster.go
//	go run ./assets/icons/tray/windows/frames16.go
//
// The marks: two rings of 9 px, 1 px strokes, interlocked (each passes
// under the other once). A state's badge sits in a 6 × 6 cell at the
// bottom-right corner with a 1 px gap cut out of the rings around it, its
// glyph on whole pixels: ! (warn), × (error), → (mode), − (no helper).
// tsdown is a slash through the mark with a 1 px gap either side; unknown
// is two small rings apart.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

const dir = "assets/icons/tray/windows"

// ring9 is a 9 px ring with a 1 px stroke.
var ring9 = []string{
	"...###...",
	".##...##.",
	".#.....#.",
	"#.......#",
	"#.......#",
	"#.......#",
	".#.....#.",
	".##...##.",
	"...###...",
}

// ring7 is unknown's smaller ring.
var ring7 = []string{
	"..###..",
	".#...#.",
	"#.....#",
	"#.....#",
	"#.....#",
	".#...#.",
	"..###..",
}

// The badges, drawn in the 6 × 6 cell at (10, 10).
var badges = map[string][]string{
	"warn": {
		"..##..",
		"..##..",
		"..##..",
		"..##..",
		"......",
		"..##..",
	},
	"error": {
		"#....#",
		".#..#.",
		"..##..",
		"..##..",
		".#..#.",
		"#....#",
	},
	"mode": {
		"...#..",
		"....#.",
		"######",
		"....#.",
		"...#..",
		"......",
	},
	"nohelper": {
		"......",
		"......",
		"######",
		"######",
		"......",
		"......",
	},
}

type grid [16][16]bool

func (g *grid) stamp(art []string, x0, y0 int) {
	for y, row := range art {
		for x, ch := range row {
			if ch == '#' {
				g[y0+y][x0+x] = true
			}
		}
	}
}

// mark is the two interlocked rings: the left one passes under the right
// at the top crossing, over it at the bottom one.
func mark() grid {
	var g grid
	g.stamp(ring9, 1, 3)
	g.stamp(ring9, 6, 3)
	g[5][8] = false // the left ring under the right, at the top
	g[9][7] = false // the right ring under the left, at the bottom
	return g
}

func frame(state string) grid {
	switch state {
	case "unknown":
		var g grid
		g.stamp(ring7, 0, 4)
		g.stamp(ring7, 9, 4)
		return g
	case "tsdown":
		g := mark()
		slash := [][2]int{{13, 4}, {12, 5}, {11, 6}, {10, 7}, {9, 8}, {8, 9}, {7, 10}}
		for _, p := range slash { // a 1 px gap either side of the slash
			for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				if x, y := p[0]+d[0], p[1]+d[1]; x >= 0 && x < 16 && y >= 0 && y < 16 {
					g[y][x] = false
				}
			}
		}
		for _, p := range slash {
			g[p[1]][p[0]] = true
		}
		return g
	case "":
		return mark()
	}
	g := mark()
	for y := 9; y < 16; y++ { // the badge's cell and a 1 px gap
		for x := 9; x < 16; x++ {
			g[y][x] = false
		}
	}
	g.stamp(badges[state], 10, 10)
	return g
}

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fail(errors.New("run from the repo root"))
	}
	for _, state := range []string{"", "warn", "error", "unknown", "tsdown", "mode", "nohelper"} {
		name := "twinnel"
		if state != "" {
			name += "-" + state
		}
		g := frame(state)
		for _, v := range []struct {
			suffix string
			c      color.NRGBA
		}{{"light", color.NRGBA{0, 0, 0, 255}}, {"dark", color.NRGBA{255, 255, 255, 255}}} {
			img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
			for y := range g {
				for x := range g[y] {
					if g[y][x] {
						img.SetNRGBA(x, y, v.c)
					}
				}
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				fail(err)
			}
			path := fmt.Sprintf("%s/%s-%s.ico", dir, name, v.suffix)
			ico, err := os.ReadFile(path)
			if err != nil {
				fail(err)
			}
			out, err := replaceEntry(ico, 16, buf.Bytes())
			if err != nil {
				fail(fmt.Errorf("%s: %v", path, err))
			}
			if err := os.WriteFile(path, out, 0o644); err != nil {
				fail(err)
			}
		}
		fmt.Printf("%s\n%s\n", name, art(g))
	}
}

// replaceEntry rewrites an .ico of PNG entries with the entry px wide
// replaced by img (added when it has none).
func replaceEntry(ico []byte, px int, img []byte) ([]byte, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not an icon file")
	}
	type ent struct {
		w   int
		png []byte
	}
	var ents []ent
	found := false
	for i := 0; i < int(binary.LittleEndian.Uint16(ico[4:])); i++ {
		e := 6 + 16*i
		if e+16 > len(ico) {
			return nil, errors.New("icon directory cut short")
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		size, off := int(binary.LittleEndian.Uint32(ico[e+8:])), int(binary.LittleEndian.Uint32(ico[e+12:]))
		if off+size > len(ico) {
			return nil, errors.New("icon entry out of range")
		}
		b := ico[off : off+size]
		if w == px {
			b, found = img, true
		}
		ents = append(ents, ent{w, b})
	}
	if !found {
		ents = append([]ent{{px, img}}, ents...)
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(ents))})
	off := 6 + 16*len(ents)
	for _, e := range ents {
		w := byte(e.w)
		if e.w >= 256 {
			w = 0
		}
		out.Write([]byte{w, w, 0, 0})
		binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(e.png)), uint32(off)})
		off += len(e.png)
	}
	for _, e := range ents {
		out.Write(e.png)
	}
	return out.Bytes(), nil
}

func art(g grid) string {
	var b strings.Builder
	for _, row := range g {
		for _, on := range row {
			if on {
				b.WriteString("██")
			} else {
				b.WriteString("··")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
