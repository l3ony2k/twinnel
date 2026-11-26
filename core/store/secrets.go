package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"sync"
)

// Secret keys.
const (
	SecretController = "controller-secret"
)

// SubscriptionURLKey is the secret key of a subscription's URL.
func SubscriptionURLKey(id string) string { return "subscription-url/" + id }

// ErrNoSecret is returned by Secrets.Get for a missing key.
var ErrNoSecret = errors.New("secret not set")

// Secrets stores credentials (subscription URLs, the controller secret).
// The macOS implementation will use the System keychain; FileSecrets is for
// development and tests.
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// FileSecrets keeps secrets in a 0600 JSON file. Not for production: the
// requirements put credentials in the system keychain.
type FileSecrets struct {
	Path string
	mu   sync.Mutex
}

func (f *FileSecrets) load() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	return m, nil
}

func (f *FileSecrets) save(m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(f.Path, b, 0o600)
}

// Get implements Secrets.
func (f *FileSecrets) Get(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := m[key]
	if !ok {
		return "", ErrNoSecret
	}
	return v, nil
}

// Set implements Secrets.
func (f *FileSecrets) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = value
	return f.save(m)
}

// Delete implements Secrets.
func (f *FileSecrets) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[key]; !ok {
		return nil
	}
	delete(m, key)
	return f.save(m)
}

// MemSecrets is an in-memory Secrets for tests.
type MemSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *MemSecrets) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return "", ErrNoSecret
	}
	return v, nil
}

func (s *MemSecrets) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
	return nil
}

func (s *MemSecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// Keys lists the stored keys, sorted.
func (f *FileSecrets) Keys() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// MigrateSecrets copies every secret of a FileSecrets into dst (keeping a
// value dst already has) and then deletes the file. It returns the keys
// copied. If any copy fails the file is kept.
func MigrateSecrets(src *FileSecrets, dst Secrets) ([]string, error) {
	keys, err := src.Keys()
	if err != nil {
		return nil, err
	}
	var copied []string
	for _, k := range keys {
		if _, err := dst.Get(k); err == nil {
			continue
		} else if !errors.Is(err, ErrNoSecret) {
			return copied, fmt.Errorf("migrate %s: %w", k, err)
		}
		v, err := src.Get(k)
		if err != nil {
			return copied, err
		}
		if err := dst.Set(k, v); err != nil {
			return copied, fmt.Errorf("migrate %s: %w", k, err)
		}
		copied = append(copied, k)
	}
	if err := os.Remove(src.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return copied, err
	}
	return copied, nil
}
