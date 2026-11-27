package qr

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
)

// The error correction of 1-M "HELLO WORLD" (ISO/IEC 18004 annex I, as
// thonky.com's tutorial works it).
func TestRSRemainder(t *testing.T) {
	msg := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	if got := rsRemainder(msg, rsDivisor(10)); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEncodeVersions(t *testing.T) {
	for _, tc := range []struct {
		text string
		ver  int
	}{
		{"a", 1},
		{strings.Repeat("x", 14), 1},
		{strings.Repeat("x", 15), 2},
		{"https://login.tailscale.com/a/1a2b3c4d5e6f7", 4},
		{strings.Repeat("x", 213), 10},
	} {
		c, err := Encode(tc.text)
		if err != nil || c.Version != tc.ver || c.Size != tc.ver*4+17 {
			t.Fatalf("%d bytes: %+v %v", len(tc.text), c, err)
		}
		// The finders' corners and the dark module.
		n := c.Size
		if !c.Dark(0, 0) || !c.Dark(n-1, 0) || !c.Dark(0, n-1) || c.Dark(7, 7) || !c.Dark(8, n-8) {
			t.Fatalf("version %d: function patterns", tc.ver)
		}
	}
	if _, err := Encode(strings.Repeat("x", 214)); err != ErrTooLong {
		t.Fatalf("214 bytes: %v", err)
	}
}

// TWINNEL_QR_OUT=<dir> writes PNGs for checking with a real decoder
// (HANDOFF.md: zxing-cpp).
func TestWritePNG(t *testing.T) {
	dir := os.Getenv("TWINNEL_QR_OUT")
	if dir == "" {
		t.Skip("TWINNEL_QR_OUT not set")
	}
	for i, s := range []string{
		"https://login.tailscale.com/a/1a2b3c4d5e6f7",
		"hi",
		strings.Repeat("https://example.com/", 8),
		strings.Repeat("y", 200),
	} {
		c, err := Encode(s)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(dir + "/" + string(rune('a'+i)) + ".png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, c.Image(4, 4)); err != nil {
			t.Fatal(err)
		}
		f.Close()
		os.WriteFile(dir+"/"+string(rune('a'+i))+".txt", []byte(s), 0o644)
	}
}
