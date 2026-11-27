package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

// mihomo's service output: the service definition sends its stdout and
// stderr to <state>/logs/mihomo.{out,err}.log (Windows: one file, written
// by twinnel-svchost, which also rotates it). Startup errors land only
// there: the controller's log stream starts once mihomo runs.

const (
	// maxServiceLog is the size above which an output file is rotated to
	// <file>.1 (one copy kept).
	maxServiceLog = 10 << 20
	// serviceLogEvery is how often the helper looks.
	serviceLogEvery = 10 * time.Minute
	// tailBytes bounds how much of a file a service.log call reads.
	tailBytes  = 1 << 20
	maxLogLine = 4096
)

// serviceLogFiles are mihomo's output files, existing or not. Only files
// inside the state dir are returned: the helper runs as root and reads
// or rotates nothing elsewhere.
func (e *Engine) serviceLogFiles() ([]string, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return nil, err
	}
	dir := st.Mihomo.LogDir
	if dir == "" {
		dir = e.Store.Path("logs")
	}
	spec := service.MihomoSpec("", "", "", "", dir)
	root := filepath.Clean(e.Store.Dir) + string(filepath.Separator)
	var out []string
	for _, p := range []string{spec.StdoutPath, spec.StderrPath} {
		p = filepath.Clean(p)
		if p == "." || !strings.HasPrefix(p, root) {
			continue
		}
		if len(out) == 0 || out[len(out)-1] != p {
			out = append(out, p)
		}
	}
	return out, nil
}

// ServiceLog returns the tail of mihomo's output files.
func (e *Engine) ServiceLog(p rpc.ServiceLogParams) (rpc.ServiceLog, error) {
	if p.Name != "mihomo" {
		return rpc.ServiceLog{}, rpc.ErrInvalid("service.log: only mihomo writes an output file Twinnel keeps")
	}
	n := p.Lines
	if n <= 0 {
		n = 300
	}
	n = min(n, 2000)
	files, err := e.serviceLogFiles()
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
		for i, l := range lines {
			lines[i] = config.RedactText(l)
		}
		out.Files = append(out.Files, rpc.ServiceLogFile{File: filepath.Base(f), Size: fi.Size(), Lines: lines})
	}
	return out, nil
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

// RotateServiceLogs moves an output file that grew past maxServiceLog to
// <file>.1 by copy and truncate: launchd and systemd keep it open in
// append mode, so they go on writing at its new end. Off unless
// Engine.RotateServiceLogs (macOS, Linux; on Windows twinnel-svchost
// rotates its own file).
func (e *Engine) rotateServiceLogs() {
	if !e.RotateServiceLogs {
		return
	}
	files, err := e.serviceLogFiles()
	if err != nil {
		return
	}
	for _, f := range files {
		if err := copyTruncate(f, maxServiceLog); err != nil {
			e.log().Warn("rotate service log", "file", filepath.Base(f), "err", err)
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
