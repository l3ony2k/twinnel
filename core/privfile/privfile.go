// Package privfile writes files that hold secrets (node passwords,
// subscription URLs, the controller secret) so that only their owner can
// read them, on every platform: mode 0600 (directories 0700) on macOS and
// Linux; on Windows a protected DACL that grants the owner, LocalSystem
// and Administrators full control and inherits nothing from the folder
// (the Windows counterpart of 0600: SYSTEM and elevated Administrators are
// what root is on Unix, and can take any file anyway).
//
// The protection is in place when the file is created (the security
// descriptor is passed to CreateFile, the mode to open(2)), before any byte
// is written: there is no moment when the content is readable by others.
// os.WriteFile(…, 0o600) is not enough on Windows, where the mode is
// ignored and the file inherits the folder's ACL (in %TEMP% or a shared
// folder that can include other users).
//
// Every user-side write of secret-bearing content goes through here: the
// export zip, app.json, composed configs, takeover plans, the temp dirs
// that mihomo -t and the temporary proxy run in. Files the helper writes
// in its state dir are protected by that dir's ACL (winsec.SecureStateDir)
// and use core/store.
package privfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Create creates path as a new file (it must not exist) that only its
// owner can read and write, and opens it for writing.
func Create(path string) (*os.File, error) { return create(path) }

// WriteFile writes data to path atomically: a private temp file in the
// same dir, synced, then renamed over path. Readers see the old file or
// the new one, never part of it; an existing path is replaced (its old ACL
// with it). No temp file is left on an error.
func WriteFile(path string, data []byte) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := createTemp(dir, "."+base+".tmp-")
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
	// Best effort: make the rename itself durable (not possible on every
	// platform).
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Mkdir creates dir (its parent must exist) so that only its owner can
// list or enter it; on Windows what is created inside inherits that.
func Mkdir(dir string) error { return mkdir(dir) }

// MkdirAll is Mkdir for dir and every missing parent. Dirs that exist are
// left as they are.
func MkdirAll(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if fi.IsDir() {
			return nil
		}
		return &fs.PathError{Op: "mkdir", Path: dir, Err: fs.ErrExist}
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := MkdirAll(parent); err != nil {
			return err
		}
	}
	if err := mkdir(dir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// Secure makes an existing file or dir private, as WriteFile and Mkdir
// would have made it: for one written before Twinnel protected it, or by
// something else (a rename kept the folder's ACL on Windows). Idempotent:
// a path that is already private is left alone; a missing one is no
// error. A dir's private DACL on Windows reaches the children that
// inherit from it.
func Secure(path string) error {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return secure(path, fi.IsDir())
}

// MkdirTemp is os.MkdirTemp with a private dir: a new dir in dir (the
// system temp dir when "") whose name starts with pattern.
func MkdirTemp(dir, pattern string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	for range 10000 {
		p := filepath.Join(dir, pattern+randSuffix())
		err := mkdir(p)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", &fs.PathError{Op: "mkdirtemp", Path: filepath.Join(dir, pattern+"*"), Err: fs.ErrExist}
}

func createTemp(dir, prefix string) (*os.File, error) {
	for range 10000 {
		f, err := create(filepath.Join(dir, prefix+randSuffix()))
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "createtemp", Path: filepath.Join(dir, prefix+"*"), Err: fs.ErrExist}
}
