package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/privfile"
)

// ValidateOption configures Validate.
type ValidateOption func(*validateOpts)

type validateOpts struct {
	geoDir    string
	geoCache  string
	safePaths []string
	timeout   time.Duration
	runAs     *runAs
}

type runAs struct {
	uid, gid int
	prepare  func(*exec.Cmd)
}

// WithRunAs runs mihomo as uid:gid instead of the caller's user: the work
// dir is made in /tmp and handed to that user, and prepare (which sets the
// credential on the command) is called last. A root helper uses it when
// the mihomo binary is controlled by a non-root user, so running the test
// never executes that user's file as root.
func WithRunAs(uid, gid int, prepare func(*exec.Cmd)) ValidateOption {
	return func(o *validateOpts) { o.runAs = &runAs{uid: uid, gid: gid, prepare: prepare} }
}

// WithGeoDir copies the geo databases (*.mmdb, *.metadb, *.dat) found in dir
// into the temporary work dir, so mihomo does not try to download them.
// They are copied, not symlinked, so nothing mihomo does can write back into
// dir.
func WithGeoDir(dir string) ValidateOption { return func(o *validateOpts) { o.geoDir = dir } }

// WithGeoCache keeps a copy of WithGeoDir's databases in cache (made
// once, again only when a source file changes) and hard-links that copy
// into each run's work dir instead of copying tens of megabytes every
// time (on Windows each new copy is also scanned by Defender). The work
// dir is then made next to cache, so the links stay on one volume; where
// a link cannot be made the file is copied as before. The cached files
// are read-only, and a cached file whose size or time no longer matches
// its source (something wrote to it) is copied again.
//
// Databases mihomo had to download during a passing run (the config dir
// has none yet, as on a fresh install) are kept in cache too, and given
// to later runs, so each check does not download them again. Not when
// mihomo runs as another user (WithRunAs): its work dir is that user's.
func WithGeoCache(cache string) ValidateOption { return func(o *validateOpts) { o.geoCache = cache } }

// WithSafePaths adds directories to mihomo's SAFE_PATHS. mihomo only accepts
// file paths in the config (external-ui, providers' path, ...) under its -d
// dir or SAFE_PATHS; the test runs with -d set to a temp dir, so absolute
// paths that are fine for the real service must be allowed here.
func WithSafePaths(dirs ...string) ValidateOption {
	return func(o *validateOpts) { o.safePaths = append(o.safePaths, dirs...) }
}

// WithTimeout bounds the mihomo run (default 60s).
func WithTimeout(d time.Duration) ValidateOption { return func(o *validateOpts) { o.timeout = d } }

// Validation is the outcome of a mihomo config test.
type Validation struct {
	OK     bool
	Errors []string // messages of level=error/fatal lines
	Output string   // full combined output
}

// Validate checks config with the official mihomo binary:
// `mihomo -t -d <tmp> -f <tmp>/config.yaml`. Everything happens in a fresh
// temporary directory that is removed afterwards. The returned error is only
// for failures to run the test; an invalid config gives OK=false.
func Validate(ctx context.Context, mihomoBin string, config []byte, opts ...ValidateOption) (*Validation, error) {
	o := validateOpts{timeout: 60 * time.Second}
	for _, f := range opts {
		f(&o)
	}
	tmpRoot := ""
	switch {
	case o.runAs != nil:
		// The helper's own TMPDIR may not be reachable by that user.
		tmpRoot = "/tmp"
	case o.geoCache != "":
		// Next to the cache: hard links need one volume.
		tmpRoot = filepath.Dir(o.geoCache)
		if err := os.MkdirAll(tmpRoot, 0o700); err != nil {
			tmpRoot = ""
		}
	}
	dir, err := privfile.MkdirTemp(tmpRoot, "twinnel-validate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var linked, placed map[string]bool
	if o.geoDir != "" || o.geoCache != "" {
		if linked, placed, err = copyGeo(o.geoDir, o.geoCache, dir); err != nil {
			return nil, fmt.Errorf("geo files: %w", err)
		}
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := privfile.WriteFile(cfg, config); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	if o.runAs != nil {
		// Linked geo files stay the helper's (read-only): chowning a link
		// would hand the cached file itself to that user.
		if err := chownTree(dir, o.runAs.uid, o.runAs.gid, linked); err != nil {
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, mihomoBin, "-t", "-d", dir, "-f", cfg)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	if len(o.safePaths) > 0 {
		paths := o.safePaths
		if cur := os.Getenv("SAFE_PATHS"); cur != "" {
			paths = append([]string{cur}, paths...)
		}
		cmd.Env = append(os.Environ(), "SAFE_PATHS="+strings.Join(paths, string(os.PathListSeparator)))
	}
	if o.runAs != nil && o.runAs.prepare != nil {
		o.runAs.prepare(cmd)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("mihomo -t: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, fmt.Errorf("mihomo -t: %w", runErr)
	}
	v := parseTestOutput(out.String())
	v.OK = runErr == nil && len(v.Errors) == 0 && strings.Contains(v.Output, "test is successful")
	if v.OK && o.geoCache != "" && o.runAs == nil {
		keepDownloads(dir, o.geoCache, placed)
	}
	if !v.OK && len(v.Errors) == 0 {
		v.Errors = []string{lastLine(v.Output)}
	}
	return v, nil
}

var logLineRE = regexp.MustCompile(`level=(error|fatal|panic) msg="((?:[^"\\]|\\.)*)"`)

func parseTestOutput(s string) *Validation {
	v := &Validation{Output: s}
	for _, m := range logLineRE.FindAllStringSubmatch(s, -1) {
		msg := strings.ReplaceAll(m[2], `\"`, `"`)
		v.Errors = append(v.Errors, msg)
	}
	return v
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	if s == "" {
		return "mihomo -t failed without output"
	}
	return s
}

// isGeoFile reports whether name is a geo database mihomo reads.
func isGeoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mmdb", ".metadb", ".dat":
		return true
	}
	return false
}

// copyGeo puts the geo databases into dst: src's (when set), as hard
// links to cache's copies when cache is set and a link can be made, else
// copied; then any others cache holds (kept from downloads). It returns
// the names it linked and all it placed.
func copyGeo(src, cache, dst string) (linked, placed map[string]bool, err error) {
	linked, placed = map[string]bool{}, map[string]bool{}
	if src != "" {
		entries, err := os.ReadDir(src)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			if !isGeoFile(e.Name()) {
				continue
			}
			info, err := os.Stat(filepath.Join(src, e.Name()))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			placed[e.Name()] = true
			if cache != "" {
				if c, err := cachedGeo(filepath.Join(src, e.Name()), info, cache); err == nil {
					if os.Link(c, filepath.Join(dst, e.Name())) == nil {
						linked[e.Name()] = true
						continue
					}
				}
			}
			if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return nil, nil, err
			}
		}
	}
	if cache == "" {
		return linked, placed, nil
	}
	entries, _ := os.ReadDir(cache) // none yet: nothing kept
	for _, e := range entries {
		if !isGeoFile(e.Name()) || strings.HasPrefix(e.Name(), ".") || placed[e.Name()] || !e.Type().IsRegular() {
			continue
		}
		c := filepath.Join(cache, e.Name())
		if os.Link(c, filepath.Join(dst, e.Name())) == nil {
			linked[e.Name()] = true
		} else if err := copyFile(c, filepath.Join(dst, e.Name())); err != nil {
			continue // only a download saved: mihomo fetches it again
		}
		placed[e.Name()] = true
	}
	return linked, placed, nil
}

// keepDownloads copies into cache the geo databases mihomo downloaded
// into the work dir dir (those not placed there). Regular files only.
func keepDownloads(dir, cache string, placed map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !isGeoFile(e.Name()) || placed[e.Name()] {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		cachedGeo(filepath.Join(dir, e.Name()), info, cache)
	}
}

// cachedGeo returns cache's read-only copy of src (whose info is given),
// copying it first when the copy is missing or differs in size or time.
func cachedGeo(src string, info os.FileInfo, cache string) (string, error) {
	c := filepath.Join(cache, filepath.Base(src))
	if ci, err := os.Lstat(c); err == nil && ci.Mode().IsRegular() && ci.Size() == info.Size() && ci.ModTime().Equal(info.ModTime()) {
		return c, nil
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(cache, ".geo-")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	tmp.Close()
	os.Remove(tmpName) // copyFile makes it anew (O_EXCL)
	if err := copyFile(src, tmpName); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	// Best effort: a copy whose time did not take is copied again next
	// time, which is only slower.
	os.Chtimes(tmpName, info.ModTime(), info.ModTime())
	if runtime.GOOS != "windows" {
		// Read-only, so a mihomo run as another user can't change it.
		// Not on Windows, where the read-only attribute can refuse the
		// hard link itself; the size and time check covers it there.
		os.Chmod(tmpName, 0o444)
	}
	os.Chmod(c, 0o600) // a read-only file can't be replaced on Windows
	if err := os.Rename(tmpName, c); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return c, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// chownTree gives dir and the files directly in it to uid:gid.
// Names in skip are left alone.
func chownTree(dir string, uid, gid int, skip map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if skip[e.Name()] {
			continue
		}
		if err := os.Lchown(filepath.Join(dir, e.Name()), uid, gid); err != nil {
			return err
		}
	}
	return os.Lchown(dir, uid, gid)
}
