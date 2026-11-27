package mihomo

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Lines of mihomo's output file (service.log) as LogEntry values, so the
// app can show them with the controller's stream. mihomo writes logrus
// text lines:
//
//	time="2026-01-02T03:04:05.123456789+08:00" level=info msg="[TCP] ..."
//
// twinnel-svchost adds its own slog lines in the same key=value form
// (time unquoted, level upper case). Anything else (a panic, a Go stack)
// is kept as an error line at the time of the line before it.

var kvRE = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S*)`)

// ParseLogLines turns output lines into entries, oldest first. Lines
// without a time get the previous line's.
func ParseLogLines(lines []string) []LogEntry {
	var out []LogEntry
	var last time.Time
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		e, ok := ParseLogLine(l)
		if !ok {
			e = LogEntry{Type: "error", Payload: l, Time: last}
		} else if e.Time.IsZero() {
			e.Time = last
		}
		last = e.Time
		out = append(out, e)
	}
	return out
}

// ParseLogLine parses one key=value log line; ok is false when it has no
// level and no msg.
func ParseLogLine(l string) (LogEntry, bool) {
	var e LogEntry
	var msg, lvl string
	var extra []string
	for _, m := range kvRE.FindAllStringSubmatch(l, -1) {
		v := m[2]
		if strings.HasPrefix(v, `"`) {
			if u, err := strconv.Unquote(v); err == nil {
				v = u
			}
		}
		switch m[1] {
		case "time":
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				e.Time = t
			}
		case "level":
			lvl = v
		case "msg":
			msg = v
		default:
			extra = append(extra, m[1]+"="+v)
		}
	}
	if lvl == "" || msg == "" && len(extra) == 0 {
		return LogEntry{}, false
	}
	if len(extra) > 0 {
		msg = strings.TrimSpace(msg + " " + strings.Join(extra, " "))
	}
	e.Type, e.Payload = normLevel(lvl), msg
	return e, true
}

// normLevel maps logrus/slog levels to the stream's: debug, info,
// warning, error.
func normLevel(l string) string {
	switch strings.ToLower(l) {
	case "debug", "trace":
		return "debug"
	case "warn", "warning":
		return "warning"
	case "error", "fatal", "panic":
		return "error"
	}
	return "info"
}

// LevelAtLeast reports whether entry level t is at or above level (the
// stream's names); an unknown level passes.
func LevelAtLeast(t, level string) bool {
	rank := map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}
	a, ok1 := rank[t]
	b, ok2 := rank[level]
	return !ok1 || !ok2 || a >= b
}
