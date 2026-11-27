package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/i18n"
)

// Rate4 formats bytes per second in at most four characters, padded on the
// left with spaces to exactly four: "  0B", "999B", "1.2K", " 12K", "1.3M",
// "999M", "1.0G". For a menu bar title drawn in a monospaced font.
func Rate4(bps int64) string {
	if bps < 0 {
		bps = 0
	}
	units := []string{"B", "K", "M", "G", "T"}
	v := float64(bps)
	u := 0
	for v >= 999.5 && u < len(units)-1 {
		v /= 1024
		u++
	}
	var s string
	switch {
	case u == 0:
		s = fmt.Sprintf("%d%s", bps, units[0])
	case v < 9.95:
		s = fmt.Sprintf("%.1f%s", v, units[u])
	default:
		s = fmt.Sprintf("%.0f%s", v, units[u])
	}
	if len(s) < 4 {
		s = strings.Repeat(" ", 4-len(s)) + s
	}
	return s
}

// TrayTitle is the menu bar text: "↓1.3M ↑ 60K".
func TrayTitle(t TrafficSample) string {
	return "↓" + Rate4(t.Down) + " ↑" + Rate4(t.Up)
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
		return fmt.Sprintf(i18n.Cur().InHours, max(0, int(d/time.Hour)))
	}
	return fmt.Sprintf(i18n.Cur().InDays, int(d/(24*time.Hour)))
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
