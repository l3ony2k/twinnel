package windows

import (
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/status"
)

// procSample is one process as the system's process list gives it
// (SYSTEM_PROCESS_INFORMATION): times in 100 ns units, CreateTime as a
// FILETIME.
type procSample struct {
	pid     int
	name    string // the image's file name: mihomo.exe
	private int64  // private working set, bytes (Task Manager's 内存)
	ws      int64  // working set, bytes
	cpu     int64  // user + kernel time
	create  int64  // FILETIME
}

// filetimeEpoch is 1970-01-01 in FILETIME units (100 ns since 1601).
const filetimeEpoch = 116444736000000000

func filetime(ft int64) time.Time { return time.Unix(0, (ft-filetimeEpoch)*100) }

// ProcessLister lists the running processes for the overview's memory,
// CPU and uptime of mihomo and tailscaled (status.ProcessSource). It
// reads the system's process list, which needs no handle on the process,
// so it sees the service-hosted mihomo (LocalSystem) from the user's app.
// CPU is the share of one core since the previous call, as Task Manager
// shows it (the first call: since the process started).
type ProcessLister struct {
	mu   sync.Mutex
	last map[int]cpuMark
}

type cpuMark struct {
	cpu    int64
	at     time.Time
	create int64
}

// entries turns samples taken at now into the listing, and keeps their
// CPU times for the next call.
func (l *ProcessLister) entries(samples []procSample, now time.Time) []status.ProcessEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := make(map[int]cpuMark, len(samples))
	out := make([]status.ProcessEntry, 0, len(samples))
	for _, s := range samples {
		if s.pid == 0 || s.name == "" {
			continue // the idle process
		}
		started := filetime(s.create)
		e := status.ProcessEntry{PID: s.pid, RSS: s.private, Command: s.name}
		if e.RSS <= 0 {
			e.RSS = s.ws
		}
		if s.create > 0 && started.Before(now) {
			e.Elapsed = now.Sub(started)
		}
		from, cpu0 := started, int64(0)
		if m, ok := l.last[s.pid]; ok && m.create == s.create && m.at.Before(now) {
			from, cpu0 = m.at, m.cpu
		}
		if wall := now.Sub(from); wall > 0 && s.create > 0 && s.cpu >= cpu0 {
			e.CPU = float64(s.cpu-cpu0) * 100 / float64(wall.Nanoseconds()/100)
		}
		next[s.pid] = cpuMark{cpu: s.cpu, at: now, create: s.create}
		out = append(out, e)
	}
	l.last = next
	return out
}
