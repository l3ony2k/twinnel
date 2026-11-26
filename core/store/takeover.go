package store

import (
	"errors"
	"io/fs"
	"os"
	"sort"
)

// Takeover plans live in <dir>/takeover/<plan_id>/ (0700; files 0600): the
// files core/takeover writes plus the helper's plan.state.json and, once
// adopted, live-backup.yaml.

// TakeoverDir returns the dir of plan id (not created). The id is checked,
// so it can't escape the state dir.
func (s *Store) TakeoverDir(id string) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	return s.Path("takeover", id), nil
}

// CreateTakeoverDir creates the dir of a new plan. It fails if it exists.
func (s *Store) CreateTakeoverDir(id string) (string, error) {
	dir, err := s.TakeoverDir(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Path("takeover"), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// TakeoverIDs lists plan ids, oldest first (ids start with a timestamp).
func (s *Store) TakeoverIDs() ([]string, error) {
	entries, err := os.ReadDir(s.Path("takeover"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && ValidID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// RemoveTakeoverDir deletes a plan dir.
func (s *Store) RemoveTakeoverDir(id string) error {
	dir, err := s.TakeoverDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
