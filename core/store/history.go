package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// History kinds.
const (
	HistoryApplied  = "applied"  // a config the helper composed and applied
	HistoryBackup   = "backup"   // the file found on disk before the helper replaced it
	HistoryRollback = "rollback" // an older entry applied again
)

// Reason codes of history entries the helper makes itself. A client words
// them in its language; Reason keeps an English text (or what the user
// typed) for the CLI and older clients.
const (
	ReasonBackup       = "backup-before-apply" // the file on disk before an apply replaced it
	ReasonApply        = "apply"               // an apply with no text from the user
	ReasonRollback     = "rollback"            // args: the history ID rolled back to
	ReasonAdopt        = "adopt"               // args: plan ID
	ReasonAdoptFailed  = "adopt-failed"        // args: plan ID; the live file written back
	ReasonTakeoverUndo = "takeover-undo"       // args: plan ID
	ReasonSecure       = "controller-secure"
	ReasonCtlMove      = "controller-move"      // args: the new controller address
	ReasonHarden       = "harden"               // args: plan ID
	ReasonSubUpdated   = "subscription-updated" // args: subscription ID
	ReasonTailnet      = "tailnet-changed"
	ReasonRestore      = "restore-layers" // args: the history ID the overrides came from
	ReasonCLI          = "cli-apply"      // twinnel compose --apply without --reason
	ReasonSetup        = "setup"          // the first apply of a fresh setup (core/setup)
)

// HistoryEntry is one line of history/index.json.
type HistoryEntry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Hash   string    `json:"hash"` // sha256 hex of the stored bytes
	Size   int       `json:"size"`
	Kind   string    `json:"kind"`
	Reason string    `json:"reason,omitempty"`
	// ReasonCode and ReasonArgs: the reason as a code (Reason… constants),
	// empty when Reason is the user's own text.
	ReasonCode string   `json:"reason_code,omitempty"`
	ReasonArgs []string `json:"reason_args,omitempty"`
	// BeforeTwinnel marks the copy of the config as it was before Twinnel first
	// wrote it: the first backup ever, or the one taken by takeover.
	BeforeTwinnel bool `json:"before_duet,omitempty"` // stored name, kept from before the rename
	// Layers: the overrides this config was composed from (applied and
	// rollback entries made since the manifest exists). nil for a backup
	// or an older entry.
	Layers *LayerManifest `json:"layers,omitempty"`
}

// HistoryMeta is what AddHistory records besides the bytes.
type HistoryMeta struct {
	Reason        string
	Code          string
	Args          []string
	BeforeTwinnel bool
	Layers        *LayerManifest
}

// LayerManifest is the set of overrides a config was composed from: the
// active subscription, the stored layers in order (content by hash, kept
// under history/layers/), and the managed layers' switches.
type LayerManifest struct {
	Subscription string          `json:"subscription,omitempty"`
	Layers       []ManifestLayer `json:"layers"`
	Coexist      bool            `json:"coexist,omitempty"`
	LeakGuard    []string        `json:"leak_guard,omitempty"`
}

// ManifestLayer is one stored layer in a manifest.
type ManifestLayer struct {
	Layer
	Hash string `json:"hash"` // sha256 of the content, a file under history/layers/
}

// Equal reports whether two manifests describe the same overrides.
func (m *LayerManifest) Equal(o *LayerManifest) bool {
	if m == nil || o == nil {
		return m == o
	}
	if m.Subscription != o.Subscription || m.Coexist != o.Coexist || !slices.Equal(m.LeakGuard, o.LeakGuard) || len(m.Layers) != len(o.Layers) {
		return false
	}
	for i := range m.Layers {
		a, b := m.Layers[i], o.Layers[i]
		if a.ID != b.ID || a.Enabled != b.Enabled || a.Hash != b.Hash || a.Stage != b.Stage || a.Format != b.Format {
			return false
		}
	}
	return true
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SaveLayerBlob keeps a layer's content under history/layers/<hash>.yaml
// for a manifest, and returns the hash.
func (s *Store) SaveLayerBlob(data []byte) (string, error) {
	h := Hash(data)
	p := s.Path("history", "layers", h+".yaml")
	if _, err := os.Lstat(p); err == nil {
		return h, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	return h, WriteFileAtomic(p, data, 0o600)
}

// ReadLayerBlob returns a layer content kept for a manifest.
func (s *Store) ReadLayerBlob(hash string) ([]byte, error) {
	if !hashRE.MatchString(hash) {
		return nil, fmt.Errorf("invalid layer hash %q", hash)
	}
	return os.ReadFile(s.Path("history", "layers", hash+".yaml"))
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
	return s.AddHistoryMeta(kind, HistoryMeta{Reason: reason}, data, at, keep)
}

// AddHistoryMeta is AddHistory with a reason code, the before-Twinnel mark
// and a layer manifest. Layer contents no entry refers to any more are
// removed with the pruned entries.
func (s *Store) AddHistoryMeta(kind string, meta HistoryMeta, data []byte, at time.Time, keep int) (HistoryEntry, error) {
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
	e := HistoryEntry{ID: id, Time: at, Hash: h, Size: len(data), Kind: kind, Reason: meta.Reason,
		ReasonCode: meta.Code, ReasonArgs: meta.Args, BeforeTwinnel: meta.BeforeTwinnel, Layers: meta.Layers}
	idx = append([]HistoryEntry{e}, idx...)
	if keep > 0 && len(idx) > keep {
		for _, old := range idx[keep:] {
			os.Remove(s.Path("history", old.ID+".yaml"))
		}
		idx = idx[:keep]
		s.pruneLayerBlobs(idx)
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

// pruneLayerBlobs removes kept layer contents no entry in idx refers to.
func (s *Store) pruneLayerBlobs(idx []HistoryEntry) {
	used := map[string]bool{}
	for _, e := range idx {
		if e.Layers != nil {
			for _, l := range e.Layers.Layers {
				used[l.Hash] = true
			}
		}
	}
	ents, err := os.ReadDir(s.Path("history", "layers"))
	if err != nil {
		return
	}
	for _, de := range ents {
		h, ok := strings.CutSuffix(de.Name(), ".yaml")
		if ok && hashRE.MatchString(h) && !used[h] {
			os.Remove(s.Path("history", "layers", de.Name()))
		}
	}
}

// FindHistory returns the entry with id from the index.
func (s *Store) FindHistory(id string) (HistoryEntry, bool, error) {
	idx, err := s.History()
	if err != nil {
		return HistoryEntry{}, false, err
	}
	for _, e := range idx {
		if e.ID == id {
			return e, true, nil
		}
	}
	return HistoryEntry{}, false, nil
}
