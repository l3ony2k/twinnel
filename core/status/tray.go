package status

import (
	"strings"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
)

// TrayIcon names a menu bar icon variant (assets/icons/tray/<name>.png).
// The icon is a monochrome template: states differ by shape only, never by
// color (see assets/icons/src/duet.mjs).
type TrayIcon string

const (
	TrayOK       TrayIcon = "duet"          // both rings whole
	TrayWarn     TrayIcon = "duet-warn"     // "!" in the notch
	TrayError    TrayIcon = "duet-error"    // "x" in the notch
	TrayUnknown  TrayIcon = "duet-unknown"  // both rings dashed: connecting / not known yet
	TrayTSDown   TrayIcon = "duet-tsdown"   // Tailscale's ring broken: Tailscale down, mihomo fine
	TrayMode     TrayIcon = "duet-mode"     // "→" in the notch: mihomo is in direct or global mode
	TrayNoHelper TrayIcon = "duet-nohelper" // hollow ring in the notch: no helper connection
)

// TrayIconFor picks the menu bar icon for a snapshot. One icon at a time,
// the most important first:
//
//  1. unknown: nothing answered yet (still loading)
//  2. error:   mihomo is unreachable or refuses the secret (traffic is at risk)
//  3. tsdown:  mihomo is fine but Tailscale is unreachable or not running
//  4. error:   any other red reason (a failing health check, ...)
//  5. warn:    any yellow reason
//  6. mode:    mihomo runs in direct or global mode instead of rule
//  7. nohelper: the helper is not installed, not running or refuses us
//  8. ok
func TrayIconFor(s Snapshot) TrayIcon {
	m := s.Mihomo
	switch {
	case !s.Loaded && s.Level == LevelUnknown:
		return TrayUnknown
	case m.Unauthorized || !m.Reachable && m.Err != "":
		return TrayError
	case m.Reachable && tailscaleDown(s.Tailscale):
		return TrayTSDown
	case s.Level == LevelError:
		return TrayError
	case s.Level == LevelWarn:
		return TrayWarn
	case m.Reachable && m.Config.Mode != "" && !strings.EqualFold(string(m.Config.Mode), string(mihomo.ModeRule)):
		return TrayMode
	case s.Helper.State != HelperUnknown && s.Helper.State != HelperConnected:
		return TrayNoHelper
	case s.Level == LevelUnknown:
		return TrayUnknown
	}
	return TrayOK
}

// tailscaleDown: installed but unreachable, or reachable and not running.
func tailscaleDown(t Tailscale) bool {
	if !t.Reachable {
		return t.Err != "" && t.Install.Kind != tailscale.InstallUnknown
	}
	return !t.State.Running()
}

// TrayToolTip is the menu bar icon's tooltip: the app, the overall state
// and what makes it so (出现故障 · Tailscale 需要登录), which reads as the
// legend of the icon's shape.
func TrayToolTip(s Snapshot) string {
	T := i18n.Cur()
	tip := T.AppName + T.TrayTipSep + s.Level.Text()
	if s.Level == LevelWarn || s.Level == LevelError {
		for _, r := range s.Reasons {
			if r.Level == s.Level {
				return tip + T.TrayTipSep + r.Text
			}
		}
	}
	return tip
}
