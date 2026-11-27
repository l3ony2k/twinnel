// Package legacy holds the names Twinnel had before it was renamed (it was
// "duet" until 2026-11), so a new install can find an old one, migrate it
// and clean it up. Nothing here is used for new installs. Remove after the
// release that follows the rename, together with the migration code that
// uses it (cmd/cli/helper_migrate*.go, the app's prefs move).
package legacy

import (
	"os"
	"sort"
	"strings"
)

// Name is the project's old name.
const Name = "duet"

// macOS install of the old name.
const (
	HelperLabel = "com.l3ony2k.duet.helper"
	MihomoLabel = "com.l3ony2k.duet.mihomo" // `duet helper harden`
	StateDir    = "/Library/Application Support/duet"
	Socket      = "/var/run/duet/helper.sock"
	HelperTools = "/Library/PrivilegedHelperTools/" + HelperLabel
	HelperPlist = "/Library/LaunchDaemons/" + HelperLabel + ".plist"
	AppBundle   = "duet.app"
	// AppSupportDir is the app's prefs dir name under the user's
	// Application Support (app.json, backups).
	AppSupportDir = "duet"
	// Keychain items were generic passwords with this account and
	// service "duet.<key>".
	KeychainService = "duet"
	KeychainAccount = "duet"
	// B64Marker marks keychain values stored base64-encoded. It is a
	// stored format, so Twinnel keeps writing and reading it unchanged.
	B64Marker = "duet-b64:"
	// CLI and helper binary names.
	CLI    = "duet"
	Helper = "duet-helper"
)

// EnvPrefix and NewEnvPrefix: environment variables were DUET_*; they are
// TWINNEL_* now, and DUET_* is read for one release as a deprecated alias.
const (
	EnvPrefix    = "DUET_"
	NewEnvPrefix = "TWINNEL_"
)

// ApplyEnvAliases copies each DUET_X of the environment to TWINNEL_X when
// TWINNEL_X is not set, so code reading TWINNEL_X also honours the old
// name (and child processes inherit it). It returns the old names it
// copied, sorted, for a deprecation notice.
func ApplyEnvAliases() []string {
	return applyEnvAliases(os.Environ(), os.LookupEnv, os.Setenv)
}

func applyEnvAliases(environ []string, lookup func(string) (string, bool), set func(string, string) error) []string {
	var copied []string
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, EnvPrefix) || len(k) == len(EnvPrefix) {
			continue
		}
		nk := NewEnvPrefix + strings.TrimPrefix(k, EnvPrefix)
		if _, set2 := lookup(nk); set2 {
			continue
		}
		if set(nk, v) == nil {
			copied = append(copied, k)
		}
	}
	sort.Strings(copied)
	return copied
}

// EnvNotice is the one-line deprecation notice for the names
// ApplyEnvAliases copied ("" when none).
func EnvNotice(copied []string) string {
	if len(copied) == 0 {
		return ""
	}
	return "deprecated environment variables " + strings.Join(copied, ", ") +
		": use the " + NewEnvPrefix + "* names (the " + EnvPrefix + "* ones stop working in the next release)"
}
