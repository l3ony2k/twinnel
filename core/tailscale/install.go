package tailscale

// InstallKind says what installed Tailscale. It matters for health checks:
// the open-source tailscaled uses its own utun, the macOS app uses a
// NetworkExtension (the system VPN slot).
type InstallKind string

const (
	InstallUnknown       InstallKind = "unknown"
	InstallTailscaled    InstallKind = "tailscaled"     // open-source daemon: brew, static binary, package
	InstallMacStandalone InstallKind = "mac-standalone" // macOS app from pkgs.tailscale.com (System Extension)
	InstallMacAppStore   InstallKind = "mac-appstore"   // macOS App Store app (Network Extension)
	InstallWindows       InstallKind = "windows"        // the official Windows client: service "Tailscale", wintun adapter
)

// WindowsDaemon is where the official Windows client installs tailscaled
// (under %ProgramFiles%, which is C:\Program Files on every stock system).
const WindowsDaemon = `C:\Program Files\Tailscale\tailscaled.exe`

// Install is what DetectInstall found.
type Install struct {
	Kind InstallKind
	// Source refines Kind: "brew", "binary" or "" (tailscaled); the app's
	// path for the macOS app.
	Source string
	// Daemon is the tailscaled binary found, if any.
	Daemon string
	// UsesSystemVPN is true for installs that take the system VPN slot.
	UsesSystemVPN bool
}

// Probe answers the questions DetectInstall asks about the file system.
// The platform layer provides it (os.Stat in production, a map in tests).
type Probe func(path string) bool

// knownPaths lists where installs leave traces, per GOOS. These are
// public, well-known locations, not user data.
var knownPaths = map[string]struct {
	socket  []string // tailscaled's LocalAPI socket
	daemons []struct{ path, source string }
	apps    []string
}{
	"darwin": {
		socket: []string{"/var/run/tailscaled.socket"},
		daemons: []struct{ path, source string }{
			{"/opt/homebrew/bin/tailscaled", "brew"},
			{"/usr/local/bin/tailscaled", "brew-or-binary"},
			{"/usr/local/sbin/tailscaled", "binary"},
		},
		apps: []string{"/Applications/Tailscale.app"},
	},
	"linux": {
		socket: []string{"/var/run/tailscale/tailscaled.sock", "/run/tailscale/tailscaled.sock"},
		daemons: []struct{ path, source string }{
			{"/usr/sbin/tailscaled", "package"},
			{"/usr/bin/tailscaled", "package"},
			{"/usr/local/bin/tailscaled", "binary"},
		},
	},
}

// DetectInstall guesses the install kind on goos from file existence.
// A tailscaled socket wins over an app bundle: when both exist, the
// daemon is what the LocalAPI client talks to.
func DetectInstall(goos string, exists Probe) Install {
	if goos == "windows" {
		// No system VPN slot on Windows: Tailscale's wintun adapter and
		// mihomo's live side by side; only routes and DNS can conflict.
		if exists(WindowsDaemon) {
			return Install{Kind: InstallWindows, Source: "official", Daemon: WindowsDaemon}
		}
		return Install{Kind: InstallUnknown}
	}
	kp, ok := knownPaths[goos]
	if !ok {
		return Install{Kind: InstallUnknown}
	}
	var daemon, source string
	for _, d := range kp.daemons {
		if exists(d.path) {
			daemon, source = d.path, d.source
			break
		}
	}
	for _, s := range kp.socket {
		if exists(s) {
			return Install{Kind: InstallTailscaled, Source: source, Daemon: daemon}
		}
	}
	for _, app := range kp.apps {
		if exists(app) {
			kind := InstallMacStandalone
			if exists(app + "/Contents/_MASReceipt/receipt") {
				kind = InstallMacAppStore
			}
			return Install{Kind: kind, Source: app, Daemon: daemon, UsesSystemVPN: true}
		}
	}
	if daemon != "" { // installed but not running
		return Install{Kind: InstallTailscaled, Source: source, Daemon: daemon}
	}
	return Install{Kind: InstallUnknown}
}
