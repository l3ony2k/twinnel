// Package clilink puts a command Twinnel manages on the user's PATH as a
// symlink, and takes it away again, without ever replacing a command
// someone else installed. Used for the tailscale CLI that a fresh setup
// installs (<state>/tailscale/current/tailscale), next to the twinnel CLI
// link of 设置 → 本地 Agent → 命令行工具.
package clilink

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// OnPathError: a command of that name, not Twinnel's, is already there.
type OnPathError struct{ Path string }

func (e *OnPathError) Error() string {
	return fmt.Sprintf("%s already exists and is not Twinnel's; left as it is", e.Path)
}

// Tool is one managed command.
type Tool struct {
	Name   string // the command, e.g. "tailscale"
	Target string // what the link points to
	// Ours reports whether a link destination is Twinnel's (Target, or one
	// from an earlier version or state dir).
	Ours func(dest string) bool
	// Shadows are other places a command of this name is commonly found
	// (/opt/homebrew/bin, /usr/local/bin): one there that is not ours
	// counts as on PATH, so the link is not made. A GUI app's own PATH is
	// too short to tell.
	Shadows []string
	// LookPath finds Name on the process's PATH (exec.LookPath; tests
	// fake it).
	LookPath func(string) (string, error)
}

// IsOurs reports whether path is a symlink to Twinnel's tool.
func (t Tool) IsOurs(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dst, err := os.Readlink(path)
	return err == nil && (dst == t.Target || t.Ours != nil && t.Ours(dst))
}

// foreign returns a command of this name that is not ours, "" if none.
func (t Tool) foreign(dirs []string) string {
	if t.LookPath != nil {
		if p, err := t.LookPath(t.Name); err == nil && !t.IsOurs(p) {
			return p
		}
	}
	for _, d := range append(append([]string{}, t.Shadows...), dirs...) {
		p := filepath.Join(d, t.Name)
		if _, err := os.Lstat(p); err == nil && !t.IsOurs(p) {
			return p
		}
	}
	return ""
}

// Link makes <dir>/<name> → Target in the first of dirs it can write,
// unless a foreign command of that name exists (*OnPathError). A link of
// ours already in place is returned as it is (re-pointed when stale).
func (t Tool) Link(dirs []string) (string, error) {
	if _, err := os.Stat(t.Target); err != nil {
		return "", fmt.Errorf("%s: %w", t.Target, err)
	}
	if p := t.foreign(dirs); p != "" {
		return "", &OnPathError{Path: p}
	}
	home, _ := os.UserHomeDir()
	var errs []error
	for _, d := range dirs {
		dst := filepath.Join(d, t.Name)
		if t.IsOurs(dst) {
			if cur, _ := os.Readlink(dst); cur == t.Target {
				return dst, nil
			}
			if err := os.Remove(dst); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if home != "" && strings.HasPrefix(d, home+string(filepath.Separator)) {
			if err := os.MkdirAll(d, 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := os.Symlink(t.Target, dst); err != nil {
			errs = append(errs, err)
			continue
		}
		return dst, nil
	}
	if len(errs) == 0 {
		errs = append(errs, fs.ErrNotExist)
	}
	return "", errors.Join(errs...)
}

// Remove deletes our links in dirs and returns the ones it removed.
func (t Tool) Remove(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		p := filepath.Join(d, t.Name)
		if t.IsOurs(p) && os.Remove(p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// Links lists our links in dirs.
func (t Tool) Links(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if p := filepath.Join(d, t.Name); t.IsOurs(p) {
			out = append(out, p)
		}
	}
	return out
}

// Tailscale is the tailscale CLI a setup installs under stateDir.
func Tailscale(stateDir string) Tool {
	return Tool{
		Name:   "tailscale",
		Target: filepath.Join(stateDir, "tailscale", "current", "tailscale"),
		// Any state dir's managed CLI (a dev helper, an old location).
		Ours: func(dst string) bool {
			return strings.HasSuffix(dst, string(filepath.Separator)+filepath.Join("tailscale", "current", "tailscale")) &&
				strings.Contains(strings.ToLower(dst), "twinnel")
		},
		Shadows: []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/usr/sbin"},
	}
}
