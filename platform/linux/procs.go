package linux

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/status"
)

// userHZ is the unit of /proc's times: USER_HZ, 100 on every Linux the
// app supports.
const userHZ = 100

// Processes lists the running processes from /proc (root: "/proc"), for
// the overview's memory, CPU and uptime of mihomo and tailscaled. It needs
// no ps (procps' option syntax differs from BSD's, and minimal systems
// have none) and reads root's daemons as any user: /proc/<pid>/stat is
// world-readable. CPU is ps's: the share of one core over the process's
// life. Command is the executable's path where readable, else its name.
func Processes(ctx context.Context, root string) ([]status.ProcessEntry, error) {
	up, err := os.ReadFile(filepath.Join(root, "uptime"))
	if err != nil {
		return nil, err
	}
	uptime, err := strconv.ParseFloat(strings.Fields(string(up) + " 0")[0], 64)
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	page := int64(os.Getpagesize())
	var out []status.ProcessEntry
	for _, d := range dirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(root, d.Name(), "stat"))
		if err != nil {
			continue // gone
		}
		e, ok := parseStat(stat, uptime, page)
		if !ok {
			continue
		}
		e.PID = pid
		if exe, err := os.Readlink(filepath.Join(root, d.Name(), "exe")); err == nil {
			e.Command = strings.TrimSuffix(exe, " (deleted)")
		}
		out = append(out, e)
	}
	return out, nil
}

// parseStat reads /proc/<pid>/stat: the name in parentheses (which may
// hold spaces and parentheses), then utime (14), stime (15), starttime
// (22) and rss (24, pages).
func parseStat(b []byte, uptime float64, page int64) (status.ProcessEntry, bool) {
	open, closing := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
	if open < 0 || closing < open {
		return status.ProcessEntry{}, false
	}
	name := string(b[open+1 : closing])
	f := strings.Fields(string(b[closing+1:])) // f[0] is field 3 (state)
	if len(f) < 22 {
		return status.ProcessEntry{}, false
	}
	num := func(field int) float64 {
		v, _ := strconv.ParseFloat(f[field-3], 64)
		return v
	}
	cpuSecs := (num(14) + num(15)) / userHZ
	elapsed := max(0, uptime-num(22)/userHZ)
	e := status.ProcessEntry{Command: name, RSS: int64(num(24)) * page,
		Elapsed: time.Duration(elapsed * float64(time.Second))}
	if elapsed > 0 {
		e.CPU = cpuSecs * 100 / elapsed
	}
	return e, true
}
