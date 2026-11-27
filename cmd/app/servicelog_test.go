package main

import (
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestWithServiceLog(t *testing.T) {
	t0 := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	file := serviceEntries(rpc.ServiceLog{Files: []rpc.ServiceLogFile{{Lines: []string{
		`time="2026-01-02T03:04:01Z" level=fatal msg="Parse config error: synthetic"`,
		`time="2026-01-02T03:04:02Z" level=debug msg="noise"`,
		`time="2026-01-02T03:04:05Z" level=info msg="also in the stream"`,
	}}}})
	live := []mihomo.LogEntry{{Type: "info", Payload: "also in the stream", Time: t0.Add(5 * time.Second)}}

	got := withServiceLog(file, live, "info", time.Time{})
	if len(got) != 2 || got[0].Payload != "Parse config error: synthetic" || got[0].Type != "error" || got[1].Payload != "also in the stream" {
		t.Fatalf("%+v", got)
	}
	// mihomo down: the file alone.
	if got := withServiceLog(file, nil, "debug", time.Time{}); len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	// 清空 hides what came before it.
	if got := withServiceLog(file, nil, "debug", t0.Add(3*time.Second)); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}
