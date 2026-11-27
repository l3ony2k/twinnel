// Package binswap replaces Twinnel's own executables while they may be
// running: an agent's `twinnel mcp` keeps bin\twinnel.exe open, the
// service host keeps twinnel-svchost.exe open while mihomo runs.
//
// Windows refuses to overwrite or delete a running image but lets it be
// renamed, and the process keeps running the renamed file. So a swap
// stages every new file next to its destination first, then for each one
// renames the current file aside (<name>.old-<stamp>) and moves the staged
// one into place. If any step fails, the files already swapped are put
// back: the set is all new or all old. Files moved aside that are no
// longer running are deleted straight away; the ones still running are
// deleted by Clean on the next swap (or install).
//
// On macOS and Linux the same steps work (a rename over a running binary
// is allowed there anyway), so callers need no per-platform branch.
//
// packaging/windows/install.ps1 follows the same naming (OldInfix,
// NewInfix) so either one cleans what the other left.
package binswap

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// OldInfix marks a file moved aside: twinnel.exe.old-<stamp>.
	OldInfix = ".old-"
	// NewInfix marks a staged file not yet swapped in (left only by a
	// crash): twinnel.exe.new-<stamp>.
	NewInfix = ".new-"
)

// File is one destination and what replaces it.
type File struct {
	Src  string
	Dst  string
	Mode fs.FileMode
}

// ops is the filesystem the package uses (tests fake Windows' locks).
var ops = struct {
	rename func(old, new string) error
	remove func(path string) error
	now    func() time.Time
}{os.Rename, os.Remove, time.Now}

// Replace puts a copy of src at dst (see Swap).
func Replace(src, dst string, mode fs.FileMode) error {
	return Swap([]File{{Src: src, Dst: dst, Mode: mode}})
}

// Swap replaces every Dst with a copy of its Src, all or none.
func Swap(files []File) error {
	stamp := strconv.FormatInt(ops.now().UnixNano(), 10)
	for _, f := range files {
		Clean(f.Dst)
	}

	// Stage: a full copy next to each destination (same volume, so the
	// swap below is a rename).
	staged := make([]string, 0, len(files))
	unstage := func() {
		for _, s := range staged {
			ops.remove(s)
		}
	}
	for _, f := range files {
		s := f.Dst + NewInfix + stamp
		if err := copyFile(f.Src, s, f.Mode); err != nil {
			ops.remove(s)
			unstage()
			return fmt.Errorf("stage %s: %w", f.Dst, err)
		}
		staged = append(staged, s)
	}

	// Swap: current aside, staged in.
	type done struct{ dst, old string } // old "" when there was no file
	var swapped []done
	rollback := func() {
		for i := len(swapped) - 1; i >= 0; i-- {
			d := swapped[i]
			ops.remove(d.dst) // the new file: nothing runs it yet
			if d.old != "" {
				ops.rename(d.old, d.dst)
			}
		}
	}
	for i, f := range files {
		old := ""
		if _, err := os.Lstat(f.Dst); err == nil {
			old = f.Dst + OldInfix + stamp
			if err := ops.rename(f.Dst, old); err != nil {
				rollback()
				unstage()
				return fmt.Errorf("move %s aside: %w", f.Dst, err)
			}
		}
		if err := ops.rename(staged[i], f.Dst); err != nil {
			if old != "" {
				ops.rename(old, f.Dst)
			}
			rollback()
			unstage()
			return fmt.Errorf("replace %s: %w", f.Dst, err)
		}
		swapped = append(swapped, done{f.Dst, old})
	}

	// The old files nothing runs go now; running ones on the next Clean.
	for _, d := range swapped {
		if d.old != "" {
			ops.remove(d.old)
		}
	}
	return nil
}

// Clean deletes what earlier swaps left next to dst: files moved aside
// that no longer run, and staged copies a crash left behind. Files still
// in use stay (Windows refuses), for the next time. It returns the ones
// that stay.
func Clean(dst string) (inUse []string) {
	for _, p := range Leftovers(dst) {
		if err := ops.remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			inUse = append(inUse, p)
		}
	}
	return inUse
}

// Leftovers lists the moved-aside and staged files next to dst.
func Leftovers(dst string) []string {
	dir, base := filepath.Split(dst)
	if dir == "" {
		dir = "."
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, base) {
			continue
		}
		rest := n[len(base):]
		for _, infix := range []string{OldInfix, NewInfix} {
			if strings.HasPrefix(rest, infix) && isStamp(rest[len(infix):]) {
				out = append(out, filepath.Join(dir, n))
			}
		}
	}
	return out
}

// isStamp: the digits Swap writes (install.ps1 writes digits too).
func isStamp(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}
