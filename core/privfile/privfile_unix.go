//go:build !windows

package privfile

import "os"

func create(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	// The umask can only take bits away; this also covers a file system
	// that ignores the mode passed to open(2).
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return f, nil
}

func mkdir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}
