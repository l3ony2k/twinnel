// Package sysdns points the system's DNS at mihomo and puts it back.
//
// A fresh setup makes mihomo the resolver of every active network service
// (dns.listen 127.0.0.1:53): Point records what each service used before
// in a backup file, then sets 127.0.0.1; Restore writes the recorded
// values back and removes the backup. The backup is written once and only
// grows (a service that appears later is added), so pointing twice never
// loses the original values. The platform part (networksetup on macOS,
// netsh and the registry on Windows) is behind Platform; tests use a fake.
package sysdns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Service is one network service and its DNS servers; no servers means
// the servers come from DHCP / the network ("automatic").
type Service struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

// Platform reads and sets the DNS of network services.
type Platform interface {
	// Services lists the services DNS is managed on (macOS: enabled
	// services with a hardware port), with their manual servers.
	Services(ctx context.Context) ([]Service, error)
	// Set sets the manual servers of a service; none means automatic.
	Set(ctx context.Context, name string, servers []string) error
}

// Backup is what the services used before Point.
type Backup struct {
	At       time.Time `json:"at"`
	Services []Service `json:"services"`
}

// Status is the current state.
type Status struct {
	Services []Service `json:"services"`
	Target   []string  `json:"target,omitempty"`
	// Pointed: every service uses exactly Target.
	Pointed bool    `json:"pointed"`
	Backup  *Backup `json:"backup,omitempty"`
	// Changed lists the services Point or Restore set.
	Changed []string `json:"changed,omitempty"`
}

// Manager points and restores, keeping the backup at Path.
type Manager struct {
	P    Platform
	Path string
	Now  func() time.Time
}

// ErrNoBackup: Restore found nothing to restore.
var ErrNoBackup = errors.New("no DNS backup: the system DNS was not changed by Twinnel")

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// ReadBackup returns the backup, nil when there is none.
func (m *Manager) ReadBackup() (*Backup, error) {
	b, err := os.ReadFile(m.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var bk Backup
	if err := json.Unmarshal(b, &bk); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(m.Path), err)
	}
	return &bk, nil
}

func (m *Manager) writeBackup(bk *Backup) error {
	b, err := json.MarshalIndent(bk, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.Path)
}

// Status reads the services and the backup; target is what Pointed
// compares with (nil: not compared).
func (m *Manager) Status(ctx context.Context, target []string) (Status, error) {
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	st := Status{Services: svcs, Target: target, Backup: bk}
	st.Pointed = len(target) > 0 && len(svcs) > 0
	for _, s := range svcs {
		if !slices.Equal(s.Servers, target) {
			st.Pointed = false
		}
	}
	return st, nil
}

// Point sets every service to target, recording the earlier values first.
func (m *Manager) Point(ctx context.Context, target []string) (Status, error) {
	if len(target) == 0 {
		return Status{}, errors.New("no DNS servers to point at")
	}
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	if len(svcs) == 0 {
		return Status{}, errors.New("no active network service")
	}
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	if bk == nil {
		bk = &Backup{At: m.now()}
	}
	grew := false
	for _, s := range svcs {
		if !slices.ContainsFunc(bk.Services, func(b Service) bool { return b.Name == s.Name }) {
			bk.Services = append(bk.Services, Service{Name: s.Name, Servers: slices.Clone(s.Servers)})
			grew = true
		}
	}
	if grew {
		// Written before anything changes: a failure half way can always
		// be restored.
		if err := m.writeBackup(bk); err != nil {
			return Status{}, err
		}
	}
	var changed []string
	for _, s := range svcs {
		if slices.Equal(s.Servers, target) {
			continue
		}
		if err := m.P.Set(ctx, s.Name, target); err != nil {
			return Status{Changed: changed}, fmt.Errorf("set DNS of %s: %w", s.Name, err)
		}
		changed = append(changed, s.Name)
	}
	st, err := m.Status(ctx, target)
	st.Changed = changed
	return st, err
}

// Restore writes the backup's values back (to the services that still
// exist) and removes the backup.
func (m *Manager) Restore(ctx context.Context) (Status, error) {
	bk, err := m.ReadBackup()
	if err != nil {
		return Status{}, err
	}
	if bk == nil {
		return Status{}, ErrNoBackup
	}
	svcs, err := m.P.Services(ctx)
	if err != nil {
		return Status{}, err
	}
	var changed []string
	for _, b := range bk.Services {
		i := slices.IndexFunc(svcs, func(s Service) bool { return s.Name == b.Name })
		if i < 0 || slices.Equal(svcs[i].Servers, b.Servers) {
			continue // gone, or already as before
		}
		if err := m.P.Set(ctx, b.Name, b.Servers); err != nil {
			return Status{Changed: changed}, fmt.Errorf("restore DNS of %s: %w", b.Name, err)
		}
		changed = append(changed, b.Name)
	}
	if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Status{Changed: changed}, err
	}
	st, err := m.Status(ctx, nil)
	st.Changed = changed
	return st, err
}
