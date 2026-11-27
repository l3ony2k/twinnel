package main

import (
	"context"
	"sort"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// Log files the helper keeps, read over service.log. mihomo's service
// output: what mihomo wrote to stdout and stderr, its startup errors
// included, which the controller's stream never carries (it starts once
// mihomo runs, and the app connects later still). 日志 → mihomo shows it
// before the stream: the lines older than the stream's first, at the
// chosen level; with mihomo down it is all there is. The helper's own
// log is 日志 → 后台助手 (on Windows and Linux only administrators can
// open the file).

// serviceLogEvery is how often the page fetches it again while shown.
const serviceLogEvery = 10 * time.Second

type serviceLog struct {
	entries  []mihomo.LogEntry
	fetching bool
	failed   bool      // the last fetch failed (an older helper, or none)
	at       time.Time // the last fetch
	cleared  time.Time // 清空: hide what came before
}

// fetchServiceLog fetches log name (rpc.ServiceLogSources) into s through
// the helper when it is due.
func (a *app) fetchServiceLog(name string, s *serviceLog) {
	if s.fetching || !a.hd.connected || a.hc == nil || a.shotMode || time.Since(s.at) < serviceLogEvery {
		return
	}
	s.fetching = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var r rpc.ServiceLog
		err := hc.Call(ctx, rpc.MethodServiceLog, rpc.ServiceLogParams{Name: name, Lines: 500}, &r)
		mygo.RunOnMain(func() {
			s.fetching, s.at, s.failed = false, time.Now(), err != nil
			if err != nil {
				return
			}
			s.entries = serviceEntries(r)
			a.invalidate()
		})
	}()
}

// serviceEntries are the output files' lines as entries, by time.
func serviceEntries(r rpc.ServiceLog) []mihomo.LogEntry {
	var out []mihomo.LogEntry
	for _, f := range r.Files {
		out = append(out, mihomo.ParseLogLines(f.Lines)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	for i := range out {
		out[i].Time = out[i].Time.Local()
	}
	return out
}

// withServiceLog puts the output lines older than the stream's first
// (all of them when the stream is empty) at level and up, and not
// cleared, before the stream.
func withServiceLog(file, live []mihomo.LogEntry, level string, cleared time.Time) []mihomo.LogEntry {
	var before time.Time
	if len(live) > 0 {
		before = live[0].Time
	}
	var out []mihomo.LogEntry
	for _, e := range file {
		if !before.IsZero() && !e.Time.Before(before) {
			break
		}
		if !cleared.IsZero() && !e.Time.After(cleared) {
			continue
		}
		if mihomo.LevelAtLeast(e.Type, level) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return live
	}
	return append(out, live...)
}
