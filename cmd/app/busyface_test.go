package main

import (
	"fmt"
	"go/ast"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Windows QA round 5 N1: a busy button's spinner reads on the face it
// sits on, at least 3:1 (WCAG's non-text contrast), on every platform,
// dark and light, for primary and push buttons, enabled or disabled
// while they work. The spinner of a busy 保存 was AccentText, black, on
// the grey face of a disabled primary.
func TestBusyMarksReadOnTheirFace(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		usePlat(t, goos)
		for _, dark := range []bool{true, false} {
			type btn struct {
				label             string
				primary, disabled bool
			}
			var btns []btn
			for _, primary := range []bool{true, false} {
				for _, disabled := range []bool{false, true} {
					btns = append(btns, btn{fmt.Sprintf("b-%v-%v", primary, disabled), primary, disabled})
				}
			}
			tr := renderControls(t, dark, func(c *ui.Context) {
				ui.Column(c).Gap(12).Children(func() {
					for _, b := range btns {
						ui.Row(c).Children(func() { busyButton(c.Key(b.label), b.label, b.primary, true, b.disabled) })
					}
				})
			})
			tr.SetScale(2) // a Retina or 200 % display: the mark's own ink, not its antialiasing
			tr.Advance(busyDelay + 50*time.Millisecond)
			tr.Frame()
			img := tr.Image()
			for _, b := range btns {
				r, ok := tr.Find(b.label)
				if !ok {
					t.Fatalf("%s: %s not found", goos, b.label)
				}
				face, mark := faceAndMark(img, ui.Rect{X: r.X * 2, Y: r.Y * 2, W: r.W * 2, H: r.H * 2})
				if got := contrast(face, mark); got < 3 {
					t.Errorf("%s dark=%v primary=%v disabled=%v: spinner %v on face %v, %.2f:1, want >= 3",
						goos, dark, b.primary, b.disabled, mark, face, got)
				}
			}
		}
	}
}

// faceAndMark samples a button at r with its busy mark in front of the
// label: the face from the middle of its top padding, the mark as the
// pixel of the mark's box (the leading square) that stands out most.
func faceAndMark(img *image.RGBA, r ui.Rect) (face, mark ui.Color) {
	face = pixel(img, r.X+r.W/2, r.Y+5)
	best := -1.0
	for y := r.Y + 4; y < r.Y+r.H-4; y++ {
		for x := r.X + 4; x < r.X+r.H; x++ {
			p := pixel(img, x, y)
			if c := contrast(p, face); c > best {
				best, mark = c, p
			}
		}
	}
	return face, mark
}

// N1: a busy mark takes the color of its face in one place (onBusyFace),
// not from a caller that guesses the face.
func TestBusyMarksColoredByFace(t *testing.T) {
	got := fileCalls(t, func(fn *ast.FuncDecl, call *ast.CallExpr) bool {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TextColor" || fn.Name.Name == "onBusyFace" {
			return false
		}
		arg, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || arg.Sel.Name != "AccentText" {
			return false
		}
		for _, name := range []string{"spinnerIcon", "spinner", "busyIcon", "progressRing"} {
			if _, ok := isCall(sel.X, "", name); ok {
				return true
			}
		}
		return false
	})
	checkMigrations(t, "busy marks colored AccentText outside onBusyFace", got, map[string]int{})
}
