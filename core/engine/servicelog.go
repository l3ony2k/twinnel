package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// Log files the helper serves over service.log (rpc.ServiceLogSources):
//
//   - mihomo: its service output. The service definition sends stdout and
//     stderr to <state>/logs/mihomo.{out,err}.log (Windows: one file,
//     written by twinnel-svchost, which also rotates it). Startup errors
//     land only there: the controller's log stream starts once mihomo
//     runs.
//   - helper: the helper's own log, <state>/logs/helper.log. On Windows
//     and Linux only administrators can read the file itself; through the
//     helper the app and the CLI can.
//
// Every line is redacted (config.RedactText) on the way out.

const (
	// maxServiceLog is the size above which a log file is rotated to
	// <file>.1 (one copy kept).
	maxServiceLog = 10 << 20
	// serviceLogEvery is how often the helper looks.
	serviceLogEvery = 10 * time.Minute
	// tailBytes bounds how much of a file a service.log call reads.
	tailBytes  = 1 << 20
	maxLogLine = 4096
)

// HelperLogName is the helper's log file in <state>/logs.
const HelperLogName = "helper.log"

// HelperLogFile is the helper's log: where it writes (cmd/helper), what
// helper.info reports and what service.log {helper} reads.
func HelperLogFile(st *store.Store) string { return st.Path("logs", HelperLogName) }

// logSource is one name service.log serves.
type logSource struct {
	// files are its log files, existing or not, inside the state dir.
	files func(e *Engine) ([]string, error)
	// rotate: the helper rotates them (copyTruncate) even when
	// Engine.RotateServiceLogs is off. The helper's own log is the
	// helper's to rotate on every platform.
	rotate bool
}

var logSources = map[string]logSource{
	rpc.ServiceLogMihomo: {files: (*Engine).mihomoLogFiles},
	rpc.ServiceLogHelper: {files: func(e *Engine) ([]string, error) { return e.inStateDir(HelperLogFile(e.Store)), nil }, rotate: true},
}

// mihomoLogFiles are mihomo's output files.
func (e *Engine) mihomoLogFiles() ([]string, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return nil, err
	}
	dir := st.Mihomo.LogDir
	if dir == "" {
		dir = e.Store.Path("logs")
	}
	spec := service.MihomoSpec("", "", "", "", dir)
	return e.inStateDir(spec.StdoutPath, spec.StderrPath), nil
}

// inStateDir keeps the paths inside the state dir, each once: the helper
// runs as root and reads or rotates nothing elsewhere.
func (e *Engine) inStateDir(paths ...string) []string {
	root := filepath.Clean(e.Store.Dir) + string(filepath.Separator)
	var out []string
	for _, p := range paths {
		p = filepath.Clean(p)
		if p == "." || !strings.HasPrefix(p, root) || slices.Contains(out, p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ServiceLog returns the tail of a log source's files.
func (e *Engine) ServiceLog(p rpc.ServiceLogParams) (rpc.ServiceLog, error) {
	src, ok := logSources[p.Name]
	if !ok {
		return rpc.ServiceLog{}, rpc.ErrInvalid("service.log: no log named %q (%s)", p.Name, strings.Join(rpc.ServiceLogSources, ", "))
	}
	n := p.Lines
	if n <= 0 {
		n = 300
	}
	n = min(n, 2000)
	files, err := src.files(e)
	if err != nil {
		return rpc.ServiceLog{}, err
	}
	out := rpc.ServiceLog{Name: p.Name, Files: []rpc.ServiceLogFile{}}
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		lines, err := tailLines(f, n)
		if err != nil {
			return out, err
		}
		if len(lines) < n {
			if older, err := tailLines(f+".1", n-len(lines)); err == nil {
				lines = append(older, lines...)
			}
		}
		lines = linesSince(lines, p.Since)
		for i, l := range lines {
			lines[i] = config.RedactText(l)
		}
		out.Files = append(out.Files, rpc.ServiceLogFile{File: filepath.Base(f), Size: fi.Size(), Lines: lines})
	}
	return out, nil
}

// linesSince drops the lines before since (none when it is zero). A
// line's time is its time= field (mihomo's logrus lines and the helper's
// slog lines both have one); a line without one (a panic, a stack) goes
// with the line before it.
func linesSince(lines []string, since time.Time) []string {
	if since.IsZero() {
		return lines
	}
	keep := false
	out := lines[:0:0]
	for _, l := range lines {
		if e, ok := mihomo.ParseLogLine(l); ok && !e.Time.IsZero() {
			keep = !e.Time.Before(since)
		}
		if keep {
			out = append(out, l)
		}
	}
	return out
}

// tailLines returns the last n lines of a file (from its last tailBytes),
// each cut to maxLogLine bytes.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := max(fi.Size()-tailBytes, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, tailBytes))
	if err != nil {
		return nil, err
	}
	if off > 0 { // the first line is cut: drop it
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	raw := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		return []string{}, nil
	}
	if len(raw) > n {
		raw = raw[len(raw)-n:]
	}
	out := make([]string, len(raw))
	for i, l := range raw {
		l = strings.TrimRight(l, "\r")
		if len(l) > maxLogLine {
			l = l[:maxLogLine] + "…"
		}
		out[i] = l
	}
	return out, nil
}

// rotateServiceLogs moves a log file that grew past maxServiceLog to
// <file>.1 by copy and truncate: its writers (launchd, systemd, the
// helper's own logger) keep it open in append mode, so they go on writing
// at its new end. mihomo's output only with Engine.RotateServiceLogs
// (macOS, Linux; on Windows twinnel-svchost rotates its own file); the
// helper's log always.
func (e *Engine) rotateServiceLogs() {
	for _, name := range rpc.ServiceLogSources {
		src := logSources[name]
		if !src.rotate && !e.RotateServiceLogs {
			continue
		}
		files, err := src.files(e)
		if err != nil {
			continue
		}
		for _, f := range files {
			if err := copyTruncate(f, maxServiceLog); err != nil {
				e.log().Warn("rotate log", "file", filepath.Base(f), "err", err)
			}
		}
	}
}

func copyTruncate(path string, limit int64) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if fi.Size() <= limit {
		return nil
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := path + ".1.tmp"
	_ = os.Remove(tmp)
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path+".1"); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Truncate(path, 0)
}

// serviceLogLoop rotates the output files now and every serviceLogEvery.
func (e *Engine) serviceLogLoop(ctx context.Context) {
	e.rotateServiceLogs()
	t := time.NewTicker(serviceLogEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.rotateServiceLogs()
		}
	}
}
