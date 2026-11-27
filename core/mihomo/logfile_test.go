package mihomo

import (
	"testing"
	"time"
)

func TestParseLogLines(t *testing.T) {
	got := ParseLogLines([]string{
		`time="2026-01-02T03:04:05.5+08:00" level=info msg="Start initial configuration in progress"`,
		`time="2026-01-02T03:04:06+08:00" level=fatal msg="Parse config error: rules[3] [X] error: \"bad\""`,
		`panic: runtime error`,
		``,
		`time=2026-01-02T03:04:07.000+08:00 level=INFO msg=exited service=TwinnelMihomo err="exit status 1"`,
		`time="2026-01-02T03:04:08+08:00" level=warning msg="[TUN] stack mixed"`,
	})
	if len(got) != 5 {
		t.Fatalf("%d entries: %+v", len(got), got)
	}
	if got[1].Type != "error" || got[1].Payload != `Parse config error: rules[3] [X] error: "bad"` {
		t.Errorf("fatal: %+v", got[1])
	}
	if got[2].Type != "error" || got[2].Payload != "panic: runtime error" || !got[2].Time.Equal(got[1].Time) {
		t.Errorf("raw line: %+v", got[2])
	}
	if got[3].Type != "info" || got[3].Payload != "exited service=TwinnelMihomo err=exit status 1" {
		t.Errorf("svchost: %+v", got[3])
	}
	if got[4].Type != "warning" || got[0].Time.Format(time.RFC3339) != "2026-01-02T03:04:05+08:00" {
		t.Errorf("%+v %+v", got[4], got[0])
	}
	if !LevelAtLeast("error", "warning") || LevelAtLeast("debug", "info") {
		t.Error("LevelAtLeast")
	}
}
