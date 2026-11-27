package takeover

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/store"
)

// Own describes the mihomo setup Twinnel itself runs, so discovery can
// tell it from a hand-made one: taking over Twinnel's own config would
// plan Twinnel's output back into layers (Windows QA round 1, I).
type Own struct {
	// Dirs are Twinnel's state dirs: a config under one is Twinnel's.
	Dirs []string `json:"dirs,omitempty"`
	// Configs are config files Twinnel writes outside its state dir (the
	// live config of an adopted setup).
	Configs []string `json:"configs,omitempty"`
	// Labels are the service labels of Twinnel's own mihomo service.
	Labels []string `json:"labels,omitempty"`
}

// DefaultOwn is what any Twinnel install uses: the default state dir and
// mihomo service label, and the same under the old name.
func DefaultOwn() Own {
	return Own{
		Dirs:   []string{store.DefaultDir, legacy.StateDir},
		Labels: []string{store.DefaultMihomoLabel, legacy.MihomoLabel},
	}
}

// With returns o plus more.
func (o Own) With(more Own) Own {
	return Own{
		Dirs:    append(append([]string(nil), o.Dirs...), more.Dirs...),
		Configs: append(append([]string(nil), o.Configs...), more.Configs...),
		Labels:  append(append([]string(nil), o.Labels...), more.Labels...),
	}
}

// Has reports whether m is Twinnel's own setup: its service carries one of
// Twinnel's labels, or its config is one Twinnel writes.
func (o Own) Has(m *MihomoSetup) bool {
	if m == nil {
		return false
	}
	if m.Service != nil {
		for _, l := range o.Labels {
			if l != "" && m.Service.Label == l {
				return true
			}
		}
	}
	if m.Config == "" {
		return false
	}
	for _, c := range o.Configs {
		if c != "" && samePath(c, m.Config) {
			return true
		}
	}
	for _, d := range o.Dirs {
		if d != "" && underDir(d, m.Config) {
			return true
		}
	}
	return false
}

// foldPaths: the platform's file names ignore case (macOS and Windows by
// default).
var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

func normPath(p string) string {
	p = filepath.Clean(p)
	if foldPaths {
		p = strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool { return normPath(a) == normPath(b) }

// underDir reports whether p is inside dir.
func underDir(dir, p string) bool {
	rel, err := filepath.Rel(normPath(dir), normPath(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
