package scm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/version"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// HostReader reads the service host's state. Every function only reads;
// tests replace them. DefaultHostReader is the real one.
type HostReader struct {
	Exists   func(path string) bool
	Version  func(ctx context.Context, bin string) (string, error) // `bin --version`
	Revision func(ctx context.Context, bin string) int             // `bin svchost --revision`
	Same     func(a, b string) bool                                // same bytes
	ReadDir  func(dir string) ([]string, error)                    // file names
	// Service reads a service's command line and whether it runs, with
	// its process id.
	Service func(ctx context.Context, label string) (cmdline string, running bool, pid int, err error)
	// Image is the executable a process was started from ("" unknown).
	Image func(pid int) string
}

// DefaultHostReader reads the real host (the SCM parts on Windows only).
func DefaultHostReader() HostReader {
	return HostReader{
		Exists:   func(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() },
		Version:  version.Binary,
		Revision: HostRevision,
		Same:     sameBytes,
		ReadDir: func(dir string) ([]string, error) {
			es, err := os.ReadDir(dir)
			var names []string
			for _, e := range es {
				names = append(names, e.Name())
			}
			return names, err
		},
		Service: serviceInfo,
		Image:   processImage,
	}
}

// Read reads the host at path (default winpath.SvcHostBin()) against src,
// the helper binary it should be a copy of, and the services among labels
// that run under it.
func (r HostReader) Read(ctx context.Context, path, src string, labels []string) Host {
	if path == "" {
		path = winpath.SvcHostBin()
	}
	h := Host{Path: path, Present: r.Exists(path)}
	if names, err := r.ReadDir(winpath.Dir(path)); err == nil {
		for _, n := range names {
			if IsAside(n) {
				h.Aside = append(h.Aside, winpath.Join(winpath.Dir(path), n))
			}
		}
	}
	if !h.Present {
		return h
	}
	h.Version, _ = r.Version(ctx, path)
	h.Same = src != "" && r.Same(src, path)
	seen := map[string]bool{}
	for _, l := range labels {
		if l == "" || seen[strings.ToLower(l)] {
			continue
		}
		seen[strings.ToLower(l)] = true
		cl, running, pid, err := r.Service(ctx, l)
		if err != nil {
			continue
		}
		argv := SplitCommandLine(cl)
		if len(argv) == 0 || !strings.EqualFold(argv[0], path) {
			continue // not hosted, or by another host
		}
		s := HostedService{Label: l, Running: running}
		if running {
			if pid > 0 {
				s.Image = r.Image(pid)
			}
			img := s.Image
			if img == "" {
				img = path // unknown (no right to read it): the file it was started from
			}
			s.Revision = r.Revision(ctx, img)
		}
		h.Services = append(h.Services, s)
	}
	return h
}

// HostRevision runs `<bin> svchost --revision` (0 when the host is from
// before revisions: it refuses the flag).
func HostRevision(ctx context.Context, bin string) int {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "svchost", "--revision").Output()
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func sameBytes(a, b string) bool {
	ha, err1 := fileSum(a)
	hb, err2 := fileSum(b)
	return err1 == nil && err2 == nil && bytes.Equal(ha, hb)
}

// sums caches file hashes by path, size and modification time: status.get
// compares the host with the helper every few minutes.
var sums sync.Map // sumKey → []byte

type sumKey struct {
	path string
	size int64
	mod  time.Time
}

func fileSum(p string) ([]byte, error) {
	p = filepath.Clean(p)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	key := sumKey{p, fi.Size(), fi.ModTime()}
	if v, ok := sums.Load(key); ok {
		return v.([]byte), nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	sum := h.Sum(nil)
	sums.Store(key, sum)
	return sum, nil
}
