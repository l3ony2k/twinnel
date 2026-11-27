// Package store is the helper's state directory:
//
//	<dir>/settings.json            paths, controller, layer/subscription metadata
//	<dir>/subscriptions/<id>.yaml  raw subscription content
//	<dir>/layers/<id>.yaml         override layers
//	<dir>/history/<ts>-<hash>.yaml composed configs (controller secret stripped)
//	<dir>/history/index.json
//	<dir>/kernels/                 managed mihomo kernels (see core/kernel)
//	<dir>/tailnet.json             last tailnet info used for the coexist layer
//	<dir>/secrets.json             FileSecrets only (development / tests)
//
// The default dir is /Library/Application Support/twinnel on macOS. Files that
// can hold node credentials are 0600 in 0700 directories.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// Store gives typed access to the state dir. Safe for concurrent use.
type Store struct {
	Dir string
	mu  sync.Mutex // guards settings.json read-modify-write
}

// Open creates the layout under dir if needed.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty dir")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	if err := checkStateDir(abs); err != nil {
		return nil, err
	}
	for _, sub := range []string{"subscriptions", "layers", "history", "kernels"} {
		p := filepath.Join(abs, sub)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return nil, err
		}
		// MkdirAll keeps the mode of a dir that already exists: these hold
		// node credentials, so tighten them every start.
		if err := os.Chmod(p, 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{Dir: abs}, nil
}

// geteuid is replaceable in tests.
var geteuid = os.Geteuid

// checkStateDir refuses, for a root helper, a state dir that is a symlink,
// is not owned by root, or is writable by group or others: whoever could
// write there could replace settings.json (which names the binaries the
// helper runs) or the kernels.
func checkStateDir(dir string) error {
	if geteuid() != 0 {
		return nil
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("state dir %s must be a directory, not a symlink", dir)
	}
	if uid, ok := ownerUID(fi); ok && uid != 0 {
		return fmt.Errorf("state dir %s is owned by uid %d, not root", dir, uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("state dir %s is writable by group or others (%v)", dir, fi.Mode().Perm())
	}
	return nil
}

// Path joins elements onto the state dir.
func (s *Store) Path(elem ...string) string {
	return filepath.Join(append([]string{s.Dir}, elem...)...)
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidID reports whether id can name a layer or subscription. IDs become
// file names, so this is also the path-traversal guard.
func ValidID(id string) bool { return idRE.MatchString(id) }

func checkID(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid id %q (want [a-z0-9][a-z0-9_-]{0,63})", id)
	}
	return nil
}

// ErrNotExist is returned for a missing layer, subscription or history entry.
var ErrNotExist = fs.ErrNotExist

// ---- layers and subscriptions ----

// ReadLayer returns the content of layers/<id>.yaml.
func (s *Store) ReadLayer(id string) ([]byte, error) { return s.readItem("layers", id) }

// WriteLayer stores layers/<id>.yaml atomically.
func (s *Store) WriteLayer(id string, data []byte) error { return s.writeItem("layers", id, data) }

// DeleteLayer removes layers/<id>.yaml (no error if missing).
func (s *Store) DeleteLayer(id string) error { return s.deleteItem("layers", id) }

// ReadSubscription returns the raw content of subscriptions/<id>.yaml.
func (s *Store) ReadSubscription(id string) ([]byte, error) { return s.readItem("subscriptions", id) }

// WriteSubscription stores subscriptions/<id>.yaml atomically.
func (s *Store) WriteSubscription(id string, data []byte) error {
	return s.writeItem("subscriptions", id, data)
}

// DeleteSubscription removes subscriptions/<id>.yaml (no error if missing).
func (s *Store) DeleteSubscription(id string) error { return s.deleteItem("subscriptions", id) }

func (s *Store) readItem(kind, id string) ([]byte, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	return os.ReadFile(s.Path(kind, id+".yaml"))
}

func (s *Store) writeItem(kind, id string, data []byte) error {
	if err := checkID(id); err != nil {
		return err
	}
	return WriteFileAtomic(s.Path(kind, id+".yaml"), data, 0o600)
}

func (s *Store) deleteItem(kind, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	err := os.Remove(s.Path(kind, id+".yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ---- small JSON state files ----

// ReadJSON decodes <dir>/<name> into v. A missing file leaves v unchanged
// and returns ErrNotExist.
func (s *Store) ReadJSON(name string, v any) error {
	b, err := os.ReadFile(s.Path(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteJSON writes v to <dir>/<name> atomically (0600).
func (s *Store) WriteJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.Path(name), append(b, '\n'), 0o600)
}

// WriteFileAtomic writes data to a temp file in the same dir, syncs it and
// renames it over path, so readers see the old or the new file, never a
// partial one.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
