package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
