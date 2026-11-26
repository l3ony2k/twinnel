package store

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// SettingsVersion is the current settings.json schema.
const SettingsVersion = 1

// Settings is settings.json. Paths in here are set when the helper is
// installed (or by editing the root-owned file); no RPC method changes them.
type Settings struct {
	Version int `json:"version"`

	Mihomo    MihomoSettings    `json:"mihomo"`
	Tailscale TailscaleSettings `json:"tailscale"`

	// LeakGuardGroups are proxy groups that must never use DIRECT.
	LeakGuardGroups []string `json:"leak_guard_groups,omitempty"`

	// Subscriptions; at most one is Active.
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
	// Layers in application order (stable-sorted by stage at compose time).
	Layers []Layer `json:"layers,omitempty"`

	HistoryKeep   int    `json:"history_keep"`   // composed configs kept in history/ (default 30)
	KernelChannel string `json:"kernel_channel"` // "stable" or "alpha"
	// AutoApply re-composes and applies after a subscription or tailnet
	// change. Default true.
	AutoApply *bool `json:"auto_apply,omitempty"`

	HelperLabel string `json:"helper_label,omitempty"`
}

// MihomoSettings locates the mihomo installation the helper manages.
type MihomoSettings struct {
	// Binary is an adopted external mihomo binary. Empty means the managed
	// kernel at kernels/mihomo/current/mihomo.
	Binary string `json:"binary,omitempty"`
	// ConfigDir is mihomo's -d dir (geo files, external-ui, providers).
	ConfigDir string `json:"config_dir"`
	// ConfigFile is the config the service runs with (-f). Default
	// <ConfigDir>/config.yaml.
	ConfigFile string `json:"config_file,omitempty"`
	// Controller is the external-controller address, e.g. 127.0.0.1:9090.
	// The secret lives in Secrets under SecretController.
	Controller string `json:"controller"`
	// MixedPort is used to download through mihomo; 0 reads mixed-port
	// from the running config.
	MixedPort    int    `json:"mixed_port,omitempty"`
	ServiceLabel string `json:"service_label,omitempty"`
	LogDir       string `json:"log_dir,omitempty"`
}

// TailscaleSettings controls the managed coexist layer.
type TailscaleSettings struct {
	Coexist bool `json:"coexist"`
	// Resolver for tailnet names in nameserver-policy. Empty means the
	// generator's default, Tailscale's MagicDNS resolver 100.100.100.100
	// (bound to the Tailscale interface when it is known). "system" is not
	// accepted: with mihomo as the system DNS it loops back into mihomo.
	Resolver string `json:"resolver,omitempty"`
	// Binary is the tailscale CLI; empty means search the usual places.
	Binary       string `json:"binary,omitempty"`
	PollSeconds  int    `json:"poll_seconds,omitempty"` // default 60
	ServiceLabel string `json:"service_label,omitempty"`
	DaemonBinary string `json:"daemon_binary,omitempty"` // tailscaled, for the service definition
}

// Subscription is the metadata of one subscription. The URL is a secret
// (Secrets key SubscriptionURLKey(id)).
type Subscription struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Active          bool      `json:"active,omitempty"`
	IntervalMinutes int       `json:"interval_minutes,omitempty"`
	LastFetch       time.Time `json:"last_fetch,omitzero"`
	LastChange      time.Time `json:"last_change,omitzero"`
	Hash            string    `json:"hash,omitempty"`
	Size            int       `json:"size,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

// Layer is the metadata of one stored override layer.
type Layer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Stage   string `json:"stage"`  // "base" or "user"
	Format  string `json:"format"` // "merge" or "verge"
	Enabled bool   `json:"enabled"`
}

// Layer stages and formats.
const (
	StageBase   = "base"
	StageUser   = "user"
	FormatMerge = "merge"
	FormatVerge = "verge"
)

// Default labels.
const (
	DefaultMihomoLabel    = "com.l3ony2k.duet.mihomo"
	DefaultTailscaleLabel = "com.tailscale.tailscaled"
	DefaultHelperLabel    = "com.l3ony2k.duet.helper"
)

// Defaults fills zero fields.
func (st *Settings) Defaults(stateDir string) {
	if st.Version == 0 {
		st.Version = SettingsVersion
	}
	if st.Mihomo.ConfigDir == "" {
		st.Mihomo.ConfigDir = filepath.Join(stateDir, "mihomo")
	}
	if st.Mihomo.ConfigFile == "" {
		st.Mihomo.ConfigFile = filepath.Join(st.Mihomo.ConfigDir, "config.yaml")
	}
	if st.Mihomo.Controller == "" {
		st.Mihomo.Controller = "127.0.0.1:9090"
	}
	if st.Mihomo.ServiceLabel == "" {
		st.Mihomo.ServiceLabel = DefaultMihomoLabel
	}
	if strings.EqualFold(strings.TrimSpace(st.Tailscale.Resolver), "system") {
		// The old default; it loops back into mihomo. Use the generator's.
		st.Tailscale.Resolver = ""
	}
	if st.Tailscale.PollSeconds <= 0 {
		st.Tailscale.PollSeconds = 60
	}
	if st.Tailscale.ServiceLabel == "" {
		st.Tailscale.ServiceLabel = DefaultTailscaleLabel
	}
	if st.HistoryKeep <= 0 {
		st.HistoryKeep = 30
	}
	if st.KernelChannel == "" {
		st.KernelChannel = "stable"
	}
	if st.AutoApply == nil {
		t := true
		st.AutoApply = &t
	}
	if st.HelperLabel == "" {
		st.HelperLabel = DefaultHelperLabel
	}
	for i := range st.Layers {
		if st.Layers[i].Stage == "" {
			st.Layers[i].Stage = StageUser
		}
		if st.Layers[i].Format == "" {
			st.Layers[i].Format = FormatMerge
		}
	}
}

// Check reports structural problems (bad ids, duplicates, two active
// subscriptions, unknown stage/format, relative paths).
func (st *Settings) Check() error {
	seen := map[string]bool{}
	for _, l := range st.Layers {
		if err := checkID(l.ID); err != nil {
			return fmt.Errorf("layer: %w", err)
		}
		if seen[l.ID] {
			return fmt.Errorf("duplicate layer %q", l.ID)
		}
		seen[l.ID] = true
		if l.Stage != StageBase && l.Stage != StageUser {
			return fmt.Errorf("layer %q: unknown stage %q", l.ID, l.Stage)
		}
		if l.Format != FormatMerge && l.Format != FormatVerge {
			return fmt.Errorf("layer %q: unknown format %q", l.ID, l.Format)
		}
	}
	seen = map[string]bool{}
	active := 0
	for _, s := range st.Subscriptions {
		if err := checkID(s.ID); err != nil {
			return fmt.Errorf("subscription: %w", err)
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate subscription %q", s.ID)
		}
		seen[s.ID] = true
		if s.Active {
			active++
		}
	}
	if active > 1 {
		return errors.New("more than one active subscription")
	}
	for name, p := range map[string]string{
		"mihomo.binary": st.Mihomo.Binary, "mihomo.config_dir": st.Mihomo.ConfigDir,
		"mihomo.config_file": st.Mihomo.ConfigFile, "tailscale.binary": st.Tailscale.Binary,
	} {
		if p != "" && !filepath.IsAbs(p) {
			return fmt.Errorf("%s must be an absolute path: %q", name, p)
		}
	}
	if st.KernelChannel != "stable" && st.KernelChannel != "alpha" {
		return fmt.Errorf("unknown kernel channel %q", st.KernelChannel)
	}
	return nil
}

// ActiveSubscription returns the active subscription, if any.
func (st *Settings) ActiveSubscription() (Subscription, bool) {
	for _, s := range st.Subscriptions {
		if s.Active {
			return s, true
		}
	}
	return Subscription{}, false
}

// LayerIndex returns the index of layer id, or -1.
func (st *Settings) LayerIndex(id string) int {
	return slices.IndexFunc(st.Layers, func(l Layer) bool { return l.ID == id })
}

// SubscriptionIndex returns the index of subscription id, or -1.
func (st *Settings) SubscriptionIndex(id string) int {
	return slices.IndexFunc(st.Subscriptions, func(s Subscription) bool { return s.ID == id })
}

// Settings loads settings.json with defaults applied. A missing file gives
// the defaults.
func (s *Store) Settings() (*Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (*Settings, error) {
	st := &Settings{}
	if err := s.ReadJSON("settings.json", st); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	st.Defaults(s.Dir)
	if err := st.Check(); err != nil {
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	return st, nil
}

// SaveSettings validates and writes settings.json.
func (s *Store) SaveSettings(st *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(st)
}

func (s *Store) saveLocked(st *Settings) error {
	st.Defaults(s.Dir)
	if err := st.Check(); err != nil {
		return err
	}
	return s.WriteJSON("settings.json", st)
}

// UpdateSettings loads, edits and saves settings.json under the store lock.
// If fn returns an error nothing is saved.
func (s *Store) UpdateSettings(fn func(st *Settings) error) (*Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if err := fn(st); err != nil {
		return nil, err
	}
	if err := s.saveLocked(st); err != nil {
		return nil, err
	}
	return st, nil
}
