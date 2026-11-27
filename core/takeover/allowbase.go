package takeover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxBaseDepth bounds how deep under the live config's dir a picked base
// may sit.
const maxBaseDepth = 3

// AllowedBase reports whether a root helper may read path as the base of
// live when a client names it (「换一个文件…」): a regular file, not a
// symlink, that looks like a config, owned by root or by the live config's
// owner, and either in one of the dirs FindCandidates searches or under
// the live config's dir (at most maxBaseDepth levels down). The helper
// never reads an arbitrary file a client names.
func AllowedBase(live, path string) error {
	liveAbs, err := filepath.Abs(live)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if abs == liveAbs {
		return fmt.Errorf("the base cannot be the live config itself")
	}
	confDir := filepath.Dir(liveAbs)
	in := false
	for _, d := range searchDirs(confDir) {
		if filepath.Dir(abs) == d {
			in = true
		}
	}
	if rel, err := filepath.Rel(confDir, abs); !in && err == nil && !strings.HasPrefix(rel, "..") {
		in = strings.Count(rel, string(filepath.Separator)) < maxBaseDepth
	}
	if !in {
		return fmt.Errorf("%s is not next to the live config (%s)", path, confDir)
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > maxCandidateSize {
		return fmt.Errorf("%s is too large", path)
	}
	if lfi, err := os.Stat(liveAbs); err == nil && !sameOwnerOrRoot(fi, lfi) {
		return fmt.Errorf("%s belongs to another user", path)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	if !looksLikeConfig(b) {
		return fmt.Errorf("%s has no proxies section", path)
	}
	return nil
}
