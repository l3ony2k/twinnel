package status

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
)

// Process is what the system says about a running daemon (mihomo,
// tailscaled): its memory and CPU, and when it started.
type Process struct {
	PID     int
	RSS     int64     // resident memory, bytes
	CPU     float64   // percent of one core, as ps reports it
	Started time.Time // now minus the elapsed time ps reports
}

// Uptime is how long the process has run at now.
func (p Process) Uptime(now time.Time) time.Duration { return max(0, now.Sub(p.Started)) }

// ProcessEntry is one line of a process listing.
type ProcessEntry struct {
	PID     int
	RSS     int64
	CPU     float64
	Elapsed time.Duration
	Command string // the executable's path (ps comm)
}

// ProcessSource lists the running processes; in the app it goes through
// the machine probes (synthetic in screenshots).
type ProcessSource func(ctx context.Context) ([]ProcessEntry, error)

// PSArgs are the ps arguments ParsePS reads: one line per process, the
// command last so a path with spaces stays whole.
var PSArgs = []string{"-axo", "pid=,rss=,pcpu=,etime=,comm="}

// ParsePS parses the output of ps with PSArgs. Lines it can't read are
// skipped.
func ParsePS(out string) []ProcessEntry {
	var list []ProcessEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		rss, err2 := strconv.ParseInt(f[1], 10, 64)
		cpu, err3 := strconv.ParseFloat(f[2], 64)
		el, err4 := parseEtime(f[3])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		// The command is the rest of the line, spaces included.
		cmd := strings.TrimSpace(line)
		for range 4 {
			cmd = strings.TrimLeft(cmd, " \t")
			if i := strings.IndexAny(cmd, " \t"); i >= 0 {
				cmd = cmd[i:]
			}
		}
		list = append(list, ProcessEntry{PID: pid, RSS: rss * 1024, CPU: cpu, Elapsed: el, Command: strings.TrimSpace(cmd)})
	}
	return list
}

// parseEtime reads ps's elapsed time: [[dd-]hh:]mm:ss.
func parseEtime(s string) (time.Duration, error) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, err
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("etime %q", s)
	}
	var secs int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, err
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second, nil
}

// IsMihomoCommand reports whether a process is a mihomo core: its file
// name starts with mihomo (mihomo, mihomo-darwin-arm64, mihomo.exe) or is
// a Clash Meta build.
func IsMihomoCommand(cmd string) bool {
	b := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(cmd, `\`, "/"))), ".exe")
	return strings.HasPrefix(b, "mihomo") || b == "clash-meta" || b == "clash"
}

// IsTailscaleDaemon reports whether a process is Tailscale's daemon: the
// open-source tailscaled, or the network extension of the app builds.
func IsTailscaleDaemon(cmd string) bool {
	b := path.Base(cmd)
	return b == "tailscaled" || b == "tailscaled.exe" || b == "IPNExtension" ||
		strings.HasPrefix(b, "io.tailscale.ipn.") && strings.HasSuffix(b, "network-extension")
}

// FindProcess returns the first process match accepts, as a Process at
// now; nil when none runs.
func FindProcess(list []ProcessEntry, now time.Time, match func(cmd string) bool) *Process {
	for _, e := range list {
		if match(e.Command) {
			return &Process{PID: e.PID, RSS: e.RSS, CPU: e.CPU, Started: now.Add(-e.Elapsed)}
		}
	}
	return nil
}

// Duration formats a running time coarsely: "9 天 20 小时", "3 小时 12 分",
// "5 分钟".
func Duration(d time.Duration) string {
	s := i18n.Cur()
	d = max(d, 0)
	days, hours, mins := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0:
		return fmt.Sprintf(s.DurDays, days, hours)
	case hours > 0:
		return fmt.Sprintf(s.DurHours, hours, mins)
	}
	return fmt.Sprintf(s.DurMinutes, max(mins, 1))
}

// --- the monitor's polling ---

// processLoop reads the process list every ProcessPoll and keeps
// mihomo's and tailscaled's entries.
func (m *Monitor) processLoop(ctx context.Context) {
	t := time.NewTicker(m.o.ProcessPoll)
	defer t.Stop()
	for {
		m.pollProcesses(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Monitor) pollProcesses(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	list, err := m.o.Processes(ctx)
	if err != nil {
		return // keep the last answer; the figures are secondary
	}
	now := m.o.Now()
	mp := FindProcess(list, now, IsMihomoCommand)
	tp := FindProcess(list, now, IsTailscaleDaemon)
	m.update(func(s *Snapshot) {
		s.Mihomo.Process = mp
		s.Tailscale.Process = tp
	})
}

// DNS modes besides mihomo's own enhanced-mode values (fake-ip,
// redir-host): health's, which the helper reports (rpc.MihomoStatus).
const (
	DNSModeOff    = health.DNSModeOff
	DNSModeNormal = health.DNSModeNormal
)
