package svchost

import (
	"os"
	"sync"
)

// LogFile appends to a file and, once it would grow past Max bytes, moves
// it to <path>.1 (replacing the older copy) and starts a new one, so the
// program's output, its startup included, is kept without bound on disk.
// The host writes the child's stdout and stderr and its own lines here.
type LogFile struct {
	Path string
	Max  int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenLog opens (creating) path for appending.
func OpenLog(path string, max int64) (*LogFile, error) {
	l := &LogFile{Path: path, Max: max}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *LogFile) open() error {
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, fi.Size()
	return nil
}

// Write appends b, rotating first when b would take the file past Max.
// A failed rotation keeps writing to the current file.
func (l *LogFile) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	if l.Max > 0 && l.size > 0 && l.size+int64(len(b)) > l.Max {
		l.rotate()
	}
	n, err := l.f.Write(b)
	l.size += int64(n)
	return n, err
}

func (l *LogFile) rotate() {
	l.f.Close()
	l.f = nil
	_ = os.Remove(l.Path + ".1") // Windows: Rename does not replace
	_ = os.Rename(l.Path, l.Path+".1")
	if err := l.open(); err != nil {
		// Could not start a new file: go on without output rather than
		// stop the service; the next Write tries again.
		l.f, l.size = nil, 0
	}
}

// Close closes the file.
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
