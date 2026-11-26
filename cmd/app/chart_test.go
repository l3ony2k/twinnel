package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui/chart"
)

// The traffic chart's ticks are round numbers in the unit they are
// labelled in.
func TestTrafficTicksRound(t *testing.T) {
	for _, hi := range []float64{1024, 600 * 1024, 585.9 * 1024, 3.3 * (1 << 20), 1.7 * (1 << 30)} {
		unit, name := rateUnit(hi)
		y := chart.NewLinear(0, hi/unit).Nice(3)
		for _, tk := range y.Ticks(3) {
			v := tk.Value
			if r := math.Round(v*10) / 10; math.Abs(r-v) > 1e-9 {
				t.Errorf("hi %.0f: tick %v %s is not round", hi, v, name)
			}
		}
	}
	if u, n := rateUnit(585.9 * 1024); u != 1024 || n != "KB/s" {
		t.Errorf("rateUnit(585.9 KB) = %v %s", u, n)
	}
	if _, n := rateUnit(2 << 20); n != "MB/s" {
		t.Errorf("rateUnit(2 MB) = %s", n)
	}
}
