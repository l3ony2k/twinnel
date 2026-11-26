package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MaxConfigFileSize caps confined reads: no mihomo config comes near it.
const MaxConfigFileSize = 64 << 20

// ReadConfined reads the file at path through an os.Root opened on its
// directory, so a symlink that leads out of that directory (say to
// /etc/master.passwd) is refused. Code running as root uses it for files in
// a user's config dir. Files over MaxConfigFileSize, and anything that is
// not a regular file, are refused too.
func ReadConfined(path string) ([]byte, error) {
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	if fi.Size() > MaxConfigFileSize {
		return nil, fmt.Errorf("%s: too large (%d bytes)", path, fi.Size())
	}
	return io.ReadAll(io.LimitReader(f, MaxConfigFileSize+1))
}
