// Package qr encodes short text (a login URL) as a QR code: byte mode,
// error correction level M, versions 1 to 10 (up to 213 bytes). It is
// small on purpose: the app shows Tailscale's login link as a code a
// phone can scan, and needs nothing more. It follows ISO/IEC 18004 and
// the structure of Project Nayuki's reference encoder.
package qr

import (
	"errors"
	"image"
	"image/color"
)

// ErrTooLong is returned for text longer than version 10 holds.
var ErrTooLong = errors.New("qr: text too long")

// Code is an encoded symbol: Size×Size modules, true is dark.
type Code struct {
	Size    int
	Version int
	Mask    int
	modules [][]bool
}

// Dark reports whether the module at column x, row y is dark.
func (c *Code) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
		return false
	}
	return c.modules[y][x]
}

// Image draws the code with a quiet zone of border modules, scale pixels
// a module, dark on white (scanners want that, whatever the theme).
func (c *Code) Image(scale, border int) *image.Gray {
	n := (c.Size + 2*border) * scale
	img := image.NewGray(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			v := uint8(255)
			if c.Dark(x/scale-border, y/scale-border) {
				v = 0
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return img
}

// block layout of level M per version (1-based): EC codewords per block,
// then the blocks of group 1 and 2 and their data codewords.
type layout struct{ ec, n1, d1, n2, d2 int }

var levelM = [...]layout{
	{}, // version 0 does not exist
	{10, 1, 16, 0, 0},
	{16, 1, 28, 0, 0},
	{26, 1, 44, 0, 0},
	{18, 2, 32, 0, 0},
	{24, 2, 43, 0, 0},
	{16, 4, 27, 0, 0},
	{18, 4, 31, 0, 0},
	{22, 2, 38, 2, 39},
	{22, 3, 36, 2, 37},
	{26, 4, 43, 1, 44},
}

var alignPos = [...][]int{
	{}, {}, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34},
	{6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
}

func (l layout) data() int { return l.n1*l.d1 + l.n2*l.d2 }

// Encode encodes text in the smallest version that holds it.
func Encode(text string) (*Code, error) {
	b := []byte(text)
	for v := 1; v < len(levelM); v++ {
		cc := 8
		if v >= 10 {
			cc = 16
		}
		if 4+cc+8*len(b) <= levelM[v].data()*8 {
			return encode(b, v, cc), nil
		}
	}
	return nil, ErrTooLong
}

func encode(b []byte, ver, cc int) *Code {
	l := levelM[ver]
	// The bit stream: byte mode, the count, the bytes, a terminator,
	// padding to a byte, then the pad bytes.
	var bits []bool
	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, v>>i&1 == 1)
		}
	}
	put(0b0100, 4)
	put(len(b), cc)
	for _, c := range b {
		put(int(c), 8)
	}
	capBits := l.data() * 8
	put(0, min(4, capBits-len(bits)))
	put(0, (8-len(bits)%8)%8)
	for pad := 0xEC; len(bits) < capBits; pad ^= 0xEC ^ 0x11 {
		put(pad, 8)
	}
	data := make([]byte, len(bits)/8)
	for i, on := range bits {
		if on {
			data[i/8] |= 0x80 >> (i % 8)
		}
	}
	all := interleave(data, l)

	c := &Code{Size: ver*4 + 17, Version: ver}
	q := newMatrix(c.Size)
	q.functionPatterns(ver)
	q.place(all)
	best, bestScore := 0, -1
	for m := range 8 {
		q.mask(m)
		q.format(m)
		if s := q.penalty(); bestScore < 0 || s < bestScore {
			best, bestScore = m, s
		}
		q.mask(m) // undo: XOR
	}
	q.mask(best)
	q.format(best)
	c.Mask, c.modules = best, q.mod
	return c
}

// interleave splits data into blocks, adds each block's error correction
// and interleaves them as the symbol stores them.
func interleave(data []byte, l layout) []byte {
	div := rsDivisor(l.ec)
	var blocks, ecs [][]byte
	off := 0
	for i := range l.n1 + l.n2 {
		n := l.d1
		if i >= l.n1 {
			n = l.d2
		}
		blk := data[off : off+n]
		off += n
		blocks = append(blocks, blk)
		ecs = append(ecs, rsRemainder(blk, div))
	}
	var out []byte
	for i := range max(l.d1, l.d2) {
		for _, blk := range blocks {
			if i < len(blk) {
				out = append(out, blk[i])
			}
		}
	}
	for i := range l.ec {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	return out
}

// --- Reed-Solomon over GF(256), polynomial 0x11D ---

func gfMul(x, y byte) byte {
	var z int
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int(y>>i&1) * int(x)
	}
	return byte(z)
}

func rsDivisor(deg int) []byte {
	r := make([]byte, deg)
	r[deg-1] = 1
	root := byte(1)
	for range deg {
		for j := range deg {
			r[j] = gfMul(r[j], root)
			if j+1 < deg {
				r[j] ^= r[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return r
}

func rsRemainder(data, div []byte) []byte {
	r := make([]byte, len(div))
	for _, b := range data {
		f := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i := range r {
			r[i] ^= gfMul(div[i], f)
		}
	}
	return r
}

// --- the module matrix ---

type matrix struct {
	n   int
	mod [][]bool
	fn  [][]bool // function modules: never masked or used for data
}

func newMatrix(n int) *matrix {
	q := &matrix{n: n, mod: make([][]bool, n), fn: make([][]bool, n)}
	for i := range n {
		q.mod[i], q.fn[i] = make([]bool, n), make([]bool, n)
	}
	return q
}

func (q *matrix) set(x, y int, dark bool) {
	q.mod[y][x] = dark
	q.fn[y][x] = true
}

func (q *matrix) functionPatterns(ver int) {
	n := q.n
	for i := range n {
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	q.finder(3, 3)
	q.finder(n-4, 3)
	q.finder(3, n-4)
	pos := alignPos[ver]
	for i, x := range pos {
		for j, y := range pos {
			last := len(pos) - 1
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue // the finders are there
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					q.set(x+dx, y+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	q.format(0) // reserve; drawn again with the chosen mask
	if ver >= 7 {
		rem := ver
		for range 12 {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		bits := ver<<12 | rem
		for i := range 18 {
			dark := bits>>i&1 == 1
			a, b := n-11+i%3, i/3
			q.set(a, b, dark)
			q.set(b, a, dark)
		}
	}
}

func (q *matrix) finder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= q.n || y >= q.n {
				continue
			}
			d := max(abs(dx), abs(dy))
			q.set(x, y, d != 2 && d != 4)
		}
	}
}

// format draws the format information for level M and mask m.
func (q *matrix) format(m int) {
	data := 0<<3 | m // level M is 00
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return bits>>i&1 == 1 }
	n := q.n
	for i := 0; i <= 5; i++ {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		q.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, n-15+i, bit(i))
	}
	q.set(8, n-8, true) // the dark module
}

// place lays the codewords out in the two-column zigzag.
func (q *matrix) place(data []byte) {
	n, i := q.n, 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := range n {
			for j := range 2 {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = n - 1 - vert
				}
				if !q.fn[y][x] && i < len(data)*8 {
					q.mod[y][x] = data[i>>3]>>(7-i&7)&1 == 1
					i++
				}
			}
		}
	}
}

func (q *matrix) mask(m int) {
	for y := range q.n {
		for x := range q.n {
			if q.fn[y][x] {
				continue
			}
			var inv bool
			switch m {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			case 7:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				q.mod[y][x] = !q.mod[y][x]
			}
		}
	}
}

// penalty scores the symbol as the standard does (lower reads better):
// runs, 2×2 blocks, finder-like patterns and the dark/light balance.
func (q *matrix) penalty() int {
	n, p := q.n, 0
	at := func(x, y int, vert bool) bool {
		if vert {
			return q.mod[x][y]
		}
		return q.mod[y][x]
	}
	for _, vert := range []bool{false, true} {
		for y := range n {
			run := 1
			for x := 1; x <= n; x++ {
				if x < n && at(x, y, vert) == at(x-1, y, vert) {
					run++
					continue
				}
				if run >= 5 {
					p += 3 + run - 5
				}
				run = 1
			}
			// 1:1:3:1:1 with four light modules on a side.
			for x := 0; x+7 <= n; x++ {
				pat := [7]bool{true, false, true, true, true, false, true}
				ok := true
				for k, d := range pat {
					if at(x+k, y, vert) != d {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				light := func(from int) bool {
					for k := from; k < from+4; k++ {
						if k >= 0 && k < n && at(k, y, vert) {
							return false
						}
					}
					return true
				}
				if light(x-4) || light(x+7) {
					p += 40
				}
			}
		}
	}
	dark := 0
	for y := range n {
		for x := range n {
			if q.mod[y][x] {
				dark++
			}
			if x+1 < n && y+1 < n {
				c := q.mod[y][x]
				if q.mod[y][x+1] == c && q.mod[y+1][x] == c && q.mod[y+1][x+1] == c {
					p += 3
				}
			}
		}
	}
	total := n * n
	k := (abs(dark*20-total*10) + total - 1) / total
	return p + max(0, k-1)*10
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
