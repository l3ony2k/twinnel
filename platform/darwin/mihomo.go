//go:build darwin

package darwin

import "github.com/l3ony2k/duet/platform/common"

// The parsers below are platform-neutral and live in platform/common; these
// names stay so existing callers keep working.
var (
	// ParseTailscaleStatus parses `tailscale status --json`.
	ParseTailscaleStatus = common.ParseTailscaleStatus
	// ParseMihomoConfigs parses GET /configs.
	ParseMihomoConfigs = common.ParseMihomoConfigs
	// ParseMihomoProxies parses GET /proxies.
	ParseMihomoProxies = common.ParseMihomoProxies
	// ParseMihomoRules parses GET /rules.
	ParseMihomoRules = common.ParseMihomoRules
	// ParseMihomoVersion parses GET /version.
	ParseMihomoVersion = common.ParseMihomoVersion
	// ParseMihomoConfigFile parses a mihomo config file; the returned secret
	// is only for authenticating to the controller.
	ParseMihomoConfigFile = common.ParseMihomoConfigFile
	// ParseRuleLine parses one rule from a config file.
	ParseRuleLine = common.ParseRuleLine
)
