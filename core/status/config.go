package status

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config is the app's own settings. Until the Keychain lands, the
// controller secret may come from the environment (preferred) or from this
// file, which is written 0600.
type Config struct {
	Controller      string `json:"controller"`
	Secret          string `json:"secret,omitempty"`
	PrimaryGroup    string `json:"primaryGroup,omitempty"`
	TestURL         string `json:"testURL,omitempty"`
	MihomoBinary    string `json:"mihomoBinary,omitempty"`
	MihomoConfigDir string `json:"mihomoConfigDir,omitempty"`
	// NotificationsOff turns desktop notifications off (on by default).
	NotificationsOff bool `json:"notificationsOff,omitempty"`
	// Language is the UI language (a core/i18n tag such as "en"); ""
	// follows the system's preferred languages.
	Language string `json:"language,omitempty"`

	// From the environment only, never saved.
	AllowWrites   bool `json:"-"`
	SecretFromEnv bool `json:"-"`
}

// Environment variables read by LoadConfig.
const (
	EnvController  = "DUET_CONTROLLER"
	EnvSecret      = "DUET_MIHOMO_SECRET"
	EnvAllowWrites = "DUET_ALLOW_WRITES"
)

// DefaultPrimaryGroup is the group shown in the panel when none is set.
const DefaultPrimaryGroup = "PROXY"

// LoadConfig reads path (a missing file is not an error), then applies the
// environment (getenv, os.Getenv in production).
func LoadConfig(path string, getenv func(string) string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if err := json.Unmarshal(b, &c); err != nil {
			return c, err
		}
	}
	if v := strings.TrimSpace(getenv(EnvController)); v != "" {
		c.Controller = v
	}
	if v := getenv(EnvSecret); v != "" {
		c.Secret = v
		c.SecretFromEnv = true
	}
	c.AllowWrites = getenv(EnvAllowWrites) == "1"
	if c.Controller == "" {
		c.Controller = "127.0.0.1:9090"
	}
	if c.PrimaryGroup == "" {
		c.PrimaryGroup = DefaultPrimaryGroup
	}
	return c, nil
}

// SaveConfig writes c to path with mode 0600. A secret that came from the
// environment is not written.
func SaveConfig(path string, c Config) error {
	if c.SecretFromEnv {
		old, _ := LoadConfig(path, func(string) string { return "" })
		c.Secret = old.Secret
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
