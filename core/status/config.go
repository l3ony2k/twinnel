package status

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/l3ony2k/twinnel/core/privfile"
)

// Config is the app's own settings. Until the Keychain lands, the
// controller secret may come from the environment (preferred) or from this
// file, which only its owner can read (core/privfile).
type Config struct {
	Controller   string `json:"controller"`
	Secret       string `json:"secret,omitempty"`
	PrimaryGroup string `json:"primaryGroup,omitempty"`
	TestURL      string `json:"testURL,omitempty"`
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
	// DailyUsed is set the first time the user does an everyday operation
	// in Twinnel (node, mode, TUN, Tailscale, exit node, delay test): 关于 →
	// 入门's last step reads it.
	DailyUsed bool `json:"dailyUsed,omitempty"`
	// Dock is when Twinnel has a Dock icon (macOS): "" while the main window
	// is open, DockAlways, DockNever (a menu bar app only).
	Dock string `json:"dock,omitempty"`

	// From the environment only, never saved. AllowWrites is on unless
	// TWINNEL_READ_ONLY=1: a deny switch for development and test runs
	// (shots, agents), not a product setting. What a control can change
	// follows from the state of what it acts on (Snapshot.Can).
	AllowWrites   bool `json:"-"`
	SecretFromEnv bool `json:"-"`
}

// Environment variables read by LoadConfig.
const (
	EnvController = "TWINNEL_CONTROLLER"
	EnvSecret     = "TWINNEL_MIHOMO_SECRET"
	// EnvReadOnly=1 keeps every write off (development and test runs).
	EnvReadOnly = "TWINNEL_READ_ONLY"
)

// TraySpeedOneLine is Config.TraySpeedLayout for one line ("↓ 512K ↑ 3M");
// the default ("", or the older "two") is two stacked lines.
const TraySpeedOneLine = "one"

// TraySpeedTwoLines is the explicit two-line value (older configs).
const TraySpeedTwoLines = "two"

// Config.Dock values; "" is the default, a Dock icon while the main
// window is open.
const (
	DockAlways = "always"
	DockNever  = "never"
)

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

// SaveConfig writes c to path atomically, readable by its owner only
// (core/privfile: 0600, an owner-only DACL on Windows); it may hold the
// controller secret. A secret that came from the environment is not
// written.
func SaveConfig(path string, c Config) error {
	if c.SecretFromEnv {
		old, _ := LoadConfig(path, func(string) string { return "" })
		c.Secret = old.Secret
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := privfile.MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	return privfile.WriteFile(path, append(b, '\n'))
}

// SecureAppFiles makes the app's prefs folder (app.json's dir) and
// app.json private, as SaveConfig writes them. One written before that,
// or moved in from the old app's folder, keeps the folder's ACL on Windows
// (readable by other local accounts) until its next write; the app runs
// this once when it starts. Idempotent, and a missing file is no error.
func SecureAppFiles(cfgPath string) error {
	if cfgPath == "" {
		return nil
	}
	return errors.Join(privfile.Secure(filepath.Dir(cfgPath)), privfile.Secure(cfgPath))
}

// AppConfigPath is the app's settings file (app.json) under the user's
// config directory; "" when there is none. The CLI reads it as the
// fallback for the controller when the helper is not reachable.
func AppConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "twinnel", "app.json")
}

// WithHelperCredentials returns c with the controller address and secret
// the helper reported (controller.credentials). The helper knows the
// secret mihomo actually loaded, so it wins over what app.json or the
// environment hold (app.json goes stale after controller.secure); an empty
// address keeps c's. One exception, for development: TWINNEL_CONTROLLER
// pins the controller (its secret comes from the environment too), so a
// dev or shot run against a scratch helper never reaches the live mihomo
// through the helper's default settings.
//
// The app and every CLI command that talks to mihomo go through this, so
// they always agree on where mihomo is and how to authenticate.
func (c Config) WithHelperCredentials(controller, secret string, getenv func(string) string) Config {
	if strings.TrimSpace(getenv(EnvController)) != "" {
		return c
	}
	if controller != "" {
		c.Controller = controller
	}
	c.Secret = secret
	return c
}
