package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"time"
)

// History kinds.
const (
	HistoryApplied  = "applied"  // a config the helper composed and applied
	HistoryBackup   = "backup"   // the file found on disk before the helper replaced it
	HistoryRollback = "rollback" // an older entry applied again
)

// HistoryEntry is one line of history/index.json.
type HistoryEntry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Hash   string    `json:"hash"` // sha256 hex of the stored bytes
	Size   int       `json:"size"`
	Kind   string    `json:"kind"`
	Reason string    `json:"reason,omitempty"`
}

var historyIDRE = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}(-\d+)?$`)

// Hash is the sha256 hex digest used throughout the history.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

const historyIndex = "history/index.json"

// History lists entries, newest first.
func (s *Store) History() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.historyLocked()
}

func (s *Store) historyLocked() ([]HistoryEntry, error) {
	var idx []HistoryEntry
	if err := s.ReadJSON(historyIndex, &idx); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("history index: %w", err)
	}
	sort.SliceStable(idx, func(i, j int) bool { return idx[i].Time.After(idx[j].Time) })
	return idx, nil
}

// AddHistory stores data as a new entry and prunes the oldest entries
// beyond keep (keep <= 0 means no pruning).
func (s *Store) AddHistory(kind, reason string, data []byte, at time.Time, keep int) (HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.historyLocked()
	if err != nil {
		return HistoryEntry{}, err
	}
	at = at.UTC().Truncate(time.Second)
	h := Hash(data)
	base := at.Format("20060102T150405Z") + "-" + h[:8]
	id := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(s.Path("history", id+".yaml")); errors.Is(err, fs.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
	if err := WriteFileAtomic(s.Path("history", id+".yaml"), data, 0o600); err != nil {
		return HistoryEntry{}, err
	}
	e := HistoryEntry{ID: id, Time: at, Hash: h, Size: len(data), Kind: kind, Reason: reason}
	idx = append([]HistoryEntry{e}, idx...)
	if keep > 0 && len(idx) > keep {
		for _, old := range idx[keep:] {
			os.Remove(s.Path("history", old.ID+".yaml"))
		}
		idx = idx[:keep]
	}
	if err := s.WriteJSON(historyIndex, idx); err != nil {
		return HistoryEntry{}, err
	}
	return e, nil
}

// ReadHistory returns the stored bytes of an entry.
func (s *Store) ReadHistory(id string) ([]byte, HistoryEntry, error) {
	if !historyIDRE.MatchString(id) {
		return nil, HistoryEntry{}, fmt.Errorf("invalid history id %q", id)
	}
	s.mu.Lock()
	idx, err := s.historyLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, HistoryEntry{}, err
	}
	for _, e := range idx {
		if e.ID == id {
			b, err := os.ReadFile(s.Path("history", id+".yaml"))
			return b, e, err
		}
	}
	return nil, HistoryEntry{}, fmt.Errorf("history %q: %w", id, ErrNotExist)
}

// LatestHistory returns the newest entry, if any.
func (s *Store) LatestHistory() (HistoryEntry, bool, error) {
	idx, err := s.History()
	if err != nil || len(idx) == 0 {
		return HistoryEntry{}, false, err
	}
	return idx[0], true, nil
}
