package main

import (
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/status"
)

// flagSupport is whether a platform's fonts draw flag emoji (🇺🇸).
type flagSupport int

const (
	// flagsProbe: measure them (Linux: there are flags with a color
	// emoji font such as Noto Color Emoji, and none without).
	flagsProbe flagSupport = iota
	// flagsDrawn: the system's emoji font has them (Apple Color Emoji).
	flagsDrawn
	// flagsMissing: never (Windows: Segoe UI Emoji draws the two letters).
	flagsMissing
)

// flagsOff is true once the fonts are known not to draw flags.
var flagsOff atomic.Bool

func init() { resetFlags() }

// resetFlags starts over from plat (tests that switch platforms).
func resetFlags() {
	flagsOff.Store(plat.flags == flagsMissing)
	flagsProbed = false
}

// probeFlags measures, once, whether the UI font draws a flag: a pair of
// regional indicators shaped as one flag is about as wide as one of them,
// drawn as two letters (or two boxes) as wide as both. The main window
// and the panel call it on their first frame; it is a no-op where the
// platform's answer is known.
var flagsProbed bool // main thread

func probeFlags(c *ui.Context) {
	if plat.flags != flagsProbe || flagsProbed {
		return
	}
	flagsProbed = true
	w := func(s string) float32 {
		x, _ := c.MeasureText(0, ui.Span{Text: s, Size: 20})
		return x
	}
	pair, one, two := w("\U0001F1FA\U0001F1F8"), w("\U0001F1FA"), w("\U0001F1F8")
	flagsOff.Store(!flagsShaped(pair, one, two))
}

// flagsShaped reads probeFlags' widths: the pair shaped as one glyph.
func flagsShaped(pair, one, two float32) bool {
	return pair > 0 && pair < 0.75*(one+two)
}

// nodeText is how a mihomo node or group name shows: as it is, without its
// flag emoji where the fonts can't draw them (status.StripFlags), never as
// two stray letters. Every surface that shows a node or group name goes
// through it; what is selected, copied or sent stays the name itself.
func nodeText(name string) string {
	if !flagsOff.Load() {
		return name
	}
	return status.StripFlags(name)
}

// logPayloadText is a mihomo log line as it shows: the proxy chain after
// " using " (GROUP[NODE], nested for relays) with each name as nodeText
// shows it, so no flag turns into two stray letters where the fonts
// cannot draw it (Windows QA round 5 N9: "PROXY[us NODE]"). The rest is
// mihomo's text as it came; copying a line copies the raw text.
func logPayloadText(p string) string {
	if !flagsOff.Load() || !status.HasFlag(p) {
		return p
	}
	i := strings.LastIndex(p, " using ")
	if i < 0 {
		return p
	}
	i += len(" using ")
	var b strings.Builder
	b.WriteString(p[:i])
	rest := p[i:]
	for rest != "" {
		j := strings.IndexAny(rest, "[]")
		if j < 0 {
			b.WriteString(nodeText(rest))
			break
		}
		if j > 0 {
			b.WriteString(nodeText(rest[:j]))
		}
		b.WriteByte(rest[j])
		rest = rest[j+1:]
	}
	return b.String()
}

// mapStrings returns f of each of ss.
func mapStrings(ss []string, f func(string) string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = f(s)
	}
	return out
}
