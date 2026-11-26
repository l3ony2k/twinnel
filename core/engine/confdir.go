package engine

// Writing into mihomo's config dir as root.
//
// An adopted config dir is usually in the user's home
// (~/mihomo-migration/conf) and writable by that user. A root helper that
// writes there with plain path operations can be redirected: the user (or
// any process running as them) replaces a file with a symlink, or a
// directory with a symlink to /etc, and the helper writes a file of their
// choosing where only root may write. So every write into the config dir
// goes through an os.Root opened on the dir (no path, symlink or ".." can
// leave it), and the dir itself must belong to the expected owner: root
// with a root-owned path, or the user recorded at adopt time
// (settings mihomo.config_dir_uid) with a path only root and that user
// control.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/l3ony2k/duet/core/store"
)

// confDir is an opened config dir.
type confDir struct {
	*os.Root
	Path     string
	UID, GID int // owner of the dir
}

// Close closes the root.
func (d *confDir) Close() error { return d.Root.Close() }

// Rel turns an absolute path into a name relative to the dir; it fails
// for paths outside it.
func (d *confDir) Rel(p string) (string, error) {
	rel, err := filepath.Rel(d.Path, p)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%s is not inside %s", p, d.Path)
	}
	return rel, nil
}

// geteuid is replaceable in tests.
var geteuid = os.Geteuid

// errConfDir marks a refused config dir.
var errConfDir = errors.New("refusing to write into the config dir as root")

// openConfDir opens dir for the helper. expectUID is the uid that must own
// it (nil: root). Not running as root, nothing is checked (development).
func openConfDir(dir string, expectUID *int) (*confDir, error) {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("config dir %q is not absolute", dir)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	fi, err := r.Stat(".")
	if err != nil {
		r.Close()
		return nil, err
	}
	uid, gid, _ := statOwner(fi)
	d := &confDir{Root: r, Path: dir, UID: uid, GID: gid}
	if geteuid() != 0 {
		return d, nil
	}
	want := 0
	if expectUID != nil {
		want = *expectUID
	}
	if err := checkOwnedPath(dir, uid, want); err != nil {
		r.Close()
		return nil, err
	}
	return d, nil
}

// checkOwnedPath: the opened dir is owned by want, and every component of
// dir (as the kernel resolves it, prefix by prefix) is owned by root or
// want and not writable by others, so no third user can redirect it.
func checkOwnedPath(dir string, owner, want int) error {
	if owner != want {
		return fmt.Errorf("%w: %s is owned by uid %d, expected uid %d", errConfDir, dir, owner, want)
	}
	var prefixes []string
	for p := dir; ; p = filepath.Dir(p) {
		prefixes = append(prefixes, p)
		if p == filepath.Dir(p) {
			break
		}
	}
	for i := len(prefixes) - 1; i >= 0; i-- {
		p := prefixes[i]
		fi, err := lstat(p)
		if err != nil {
			return err
		}
		uid, gid, _ := statOwner(fi)
		if uid != 0 && uid != want {
			return fmt.Errorf("%w: %s is owned by uid %d", errConfDir, p, uid)
		}
		m := fi.Mode()
		if m.IsDir() && m&fs.ModeSticky == 0 && (m.Perm()&0o002 != 0 || (m.Perm()&0o020 != 0 && gid != 0)) {
			return fmt.Errorf("%w: %s is writable by group or others", errConfDir, p)
		}
	}
	return nil
}

var lstat = os.Lstat

// openConfigFileDir opens the dir of the config file (where config.yaml is
// written) with the owner rule of the settings.
func (e *Engine) openConfigFileDir(st *store.Settings) (*confDir, string, error) {
	d, err := openConfDir(filepath.Dir(st.Mihomo.ConfigFile), st.Mihomo.ConfigDirUID)
	if err != nil {
		return nil, "", err
	}
	return d, filepath.Base(st.Mihomo.ConfigFile), nil
}

// readConfigFile reads the config file without following a symlink out of
// its dir (a root helper must not be made to read /etc/master.passwd as
// "the config").
func (e *Engine) readConfigFile(st *store.Settings) ([]byte, error) {
	return readConfined(st.Mihomo.ConfigFile)
}

// readConfined reads p through an os.Root on its dir: symlinks that leave
// the dir are refused.
func readConfined(p string) ([]byte, error) {
	r, err := os.OpenRoot(filepath.Dir(p))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(filepath.Base(p))
}

// writeConfined writes data to name in d atomically (temp file created
// exclusively, then renamed over name) with mode. In a dir that belongs to
// a user the file is given to that user (keeping the group of the file it
// replaces, old, when that was the user's too), so a root helper never
// leaves a root-owned config in a home dir. The rename replaces a symlink
// at name instead of following it.
func writeConfined(d *confDir, name string, data []byte, mode fs.FileMode, old fs.FileInfo) error {
	tmp := fmt.Sprintf(".%s.tmp-%d", name, os.Getpid())
	if err := d.RemoveAll(tmp); err != nil {
		return err
	}
	f, err := d.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			d.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if geteuid() == 0 {
		uid, gid := d.UID, d.GID
		if old != nil {
			if u, g, ok := statOwner(old); ok && u == uid {
				gid = g
			}
		}
		if uid != 0 {
			// fchown on the open file: no path is involved.
			if err := f.Chown(uid, gid); err != nil {
				return err
			}
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := d.Rename(tmp, name); err != nil {
		return err
	}
	ok = true
	return nil
}
