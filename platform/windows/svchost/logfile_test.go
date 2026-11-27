package svchost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogFileRotates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mihomo.out.log")
	os.WriteFile(p, []byte("from before\n"), 0o644)
	l, err := OpenLog(p, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Write([]byte("startup error line\n")) // 12+19 > 30: rotates first
	l.Write([]byte("second\n"))
	cur, _ := os.ReadFile(p)
	old, _ := os.ReadFile(p + ".1")
	if string(old) != "from before\n" || string(cur) != "startup error line\nsecond\n" {
		t.Fatalf("cur %q old %q", cur, old)
	}
	l.Write([]byte(strings.Repeat("x", 40) + "\n")) // rotates again, replacing .1
	old, _ = os.ReadFile(p + ".1")
	if string(old) != "startup error line\nsecond\n" {
		t.Fatalf("old %q", old)
	}
	// A line longer than Max on an empty file is written, not looped.
	cur, _ = os.ReadFile(p)
	if len(cur) != 41 {
		t.Fatalf("cur %d bytes", len(cur))
	}
}
