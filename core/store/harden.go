package store

import (
	"errors"
	"io/fs"
	"os"
	"sort"
)

// Harden plans live in <dir>/harden/<plan_id>/ (0700; files 0600):
// plan.json, plan.state.json and, once applied, a copy of the replaced
// service definition.

// HardenDir returns the dir of plan id (not created).
func (s *Store) HardenDir(id string) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	return s.Path("harden", id), nil
}

// CreateHardenDir creates the dir of a new plan. It fails if it exists.
func (s *Store) CreateHardenDir(id string) (string, error) {
	dir, err := s.HardenDir(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Path("harden"), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// HardenIDs lists plan ids, oldest first.
func (s *Store) HardenIDs() ([]string, error) {
	entries, err := os.ReadDir(s.Path("harden"))
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
