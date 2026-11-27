package status

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/i18n"
)

// Menu bar rates use binary units (1 K = 1024 B), as Bytes and Rate do in
// the window, with the compact unit letter alone ("K", not "KB/s"): the
// menu bar has no room for more. A value switches to the next unit once it
// would round to 1000 or more, so it never takes more than three digits:
// 999 B is "999B", 1000 B is "1K" (1000/1024 rounds to 1).

// RateCompact formats bytes per second as a whole number and a unit
// letter, padded on the left with spaces to exactly four characters:
// "  0B", "999B", "  1K", "512K", "  3M", "  1G". For the menu bar, drawn
// in a monospaced font so the width never changes.
func RateCompact(bps int64) string {
	if bps < 0 {
		bps = 0
	}
	const units = "BKMGT"
	v := float64(bps)
	u := 0
	for math.Round(v) >= 1000 && u < len(units)-1 {
		v /= 1024
		u++
	}
	s := strconv.FormatFloat(math.Round(v), 'f', 0, 64) + units[u:u+1]
	if len(s) < 4 {
		s = strings.Repeat(" ", 4-len(s)) + s
	}
	return s
}

// ArrowGap is the gap between a rate's arrow and its number in the menu
// bar title: a thin space, narrower than a full one.
const ArrowGap = "\u2009"

// TrayDown and TrayUp are one line each of the menu bar rate: "↓ 512K".
func TrayDown(t TrafficSample) string { return "↓" + ArrowGap + RateCompact(t.Down) }
func TrayUp(t TrafficSample) string   { return "↑" + ArrowGap + RateCompact(t.Up) }

// TrayTitle is the one-line menu bar text: "↓ 512K ↑   3M" (thin spaces
// after the arrows, numbers right-aligned in four characters).
func TrayTitle(t TrafficSample) string {
	return TrayDown(t) + " " + TrayUp(t)
}

// Bytes formats a byte count: "512 B", "1.5 KB", "3.2 GB".
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// Rate formats bytes per second: "1.5 KB/s".
func Rate(bps int64) string { return Bytes(bps) + "/s" }

// Ago formats how long ago t was, in Chinese.
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	s := i18n.Cur()
	switch {
	case d < 10*time.Second:
		return s.JustNow
	case d < time.Minute:
		return fmt.Sprintf(s.SecondsAgo, int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf(s.MinutesAgo, int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf(s.HoursAgo, int(d/time.Hour))
	}
	return fmt.Sprintf(s.DaysAgo, int(d/(24*time.Hour)))
}

// Until formats how long until t: "3 天后".
func Until(now, t time.Time) string {
	d := t.Sub(now)
	if d < 48*time.Hour {
		return i18n.F(i18n.Cur().InHours, max(0, int(d/time.Hour)))
	}
	return i18n.F(i18n.Cur().InDays, int(d/(24*time.Hour)))
}

// DelayText formats a delay in ms: "88 ms", "超时" for 0, "未测" when unknown.
func DelayText(ms int, known bool) string {
	switch {
	case !known:
		return i18n.Cur().DelayUnknown
	case ms <= 0:
		return i18n.Cur().DelayTimeout
	}
	return fmt.Sprintf("%d ms", ms)
}
