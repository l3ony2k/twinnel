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
	// MihomoBinary and MihomoConfigDir are kept so older app.json files
	// load; nothing reads them (the helper has its own).
	MihomoBinary    string `json:"mihomoBinary,omitempty"`
	MihomoConfigDir string `json:"mihomoConfigDir,omitempty"`
	// NotificationsOff turns desktop notifications off (on by default).
	NotificationsOff bool `json:"notificationsOff,omitempty"`
	// TraySpeedOff hides the traffic rate next to the menu bar icon (shown
	// by default); TraySpeedLayout is how it shows: "" (default) upload
	// over download in a small font, TraySpeedOneLine "↓ 512K ↑ 3M".
	TraySpeedOff    bool   `json:"traySpeedOff,omitempty"`
	TraySpeedLayout string `json:"traySpeedLayout,omitempty"`
	// Language is the UI language (a core/i18n tag such as "en"); ""
	// follows the system's preferred languages.
	Language string `json:"language,omitempty"`
	// ExitNodeAccepted is the Tailscale exit node the user chose to use
	// while mihomo's TUN is on: its conflict shows as accepted, not as a
	// failure. Cleared when the exit node changes or turns off.
	ExitNodeAccepted string `json:"exitNodeAccepted,omitempty"`
	// Sidebar is the main window's sidebar: "" expanded, "icons" a rail
	// of icons, "hidden"; SidebarWidth its expanded width in points, 0 for
	// the default (the platform's, wider in some languages).
	Sidebar      string  `json:"sidebar,omitempty"`
	SidebarWidth float32 `json:"sidebarWidth,omitempty"`

	// From the environment only, never saved. AllowWrites is on unless
	// DUET_READ_ONLY=1: a deny switch for development and test runs
	// (shots, agents), not a product setting. What a control can change
	// follows from the state of what it acts on (Snapshot.Can).
	AllowWrites   bool `json:"-"`
	SecretFromEnv bool `json:"-"`
}

// Environment variables read by LoadConfig.
const (
	EnvController = "DUET_CONTROLLER"
	EnvSecret     = "DUET_MIHOMO_SECRET"
	// EnvReadOnly=1 keeps every write off (development and test runs).
	EnvReadOnly = "DUET_READ_ONLY"
)

// TraySpeedOneLine is Config.TraySpeedLayout for one line ("↓ 512K ↑ 3M");
// the default ("", or the older "two") is two stacked lines.
const TraySpeedOneLine = "one"

// TraySpeedTwoLines is the explicit two-line value (older configs).
const TraySpeedTwoLines = "two"

// TraySpeedTwo reports whether the menu bar speed shows on two lines.
func (c Config) TraySpeedTwo() bool { return c.TraySpeedLayout != TraySpeedOneLine }

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
	c.AllowWrites = getenv(EnvReadOnly) != "1"
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
