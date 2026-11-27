package health

import "sort"

// Failure reasons and how each is fixed.
//
// A check can fail or warn for several reasons; each names the ones it
// found in Result.Reasons (Reason* codes, "<check>/<what>"). Every reason
// has an entry in the fix catalog (FixFor): a one-click fix (FixAuto,
// through the helper or the app), a guided fix (FixGuided: the place in
// the app where the user changes it), or none in Twinnel (FixManual: why,
// and what to do outside). TestEveryReasonHasAFix fails when a reason is
// added without one.

// Reason codes (Result.Reasons).
const (
	// Any check.
	ReasonReadFailed    = "read-failed"    // a system fact could not be read
	ReasonCheckTimeout  = "check-timeout"  // the check took too long
	ReasonCheckPanicked = "check-panicked" // the check itself broke

	// tailnet-route
	ReasonRouteNoIface    = "tailnet-route/no-interface"
	ReasonRouteLookup     = "tailnet-route/lookup-failed"
	ReasonRouteIntoMihomo = "tailnet-route/into-mihomo"
	ReasonRouteElsewhere  = "tailnet-route/elsewhere"

	// tun-route-exclude
	ReasonExcludeMissing = "tun-route-exclude/missing"

	// tailscale-direct-rules
	ReasonRulesGlobal    = "tailscale-direct-rules/global-mode"
	ReasonRulesNotDirect = "tailscale-direct-rules/not-direct"
	ReasonRulesShadowed  = "tailscale-direct-rules/shadowed"

	// magicdns
	ReasonMagicNoResolver = "magicdns/no-system-resolver"
	ReasonMagicSystem     = "magicdns/system-wrong"
	ReasonMagicMihomo     = "magicdns/mihomo-wrong"

	// peer-short-names
	ReasonNamesNone     = "peer-short-names/none"
	ReasonNamesOutdated = "peer-short-names/outdated"

	// tailnet-resolver-binding
	ReasonBindStale   = "tailnet-resolver-binding/stale"
	ReasonBindMissing = "tailnet-resolver-binding/missing"

	// fake-ip-filter-mode
	ReasonFakeIPWildcard = "fake-ip-filter-mode/wildcard-mode"

	// system-dns
	ReasonDNSNotMihomo = "system-dns/not-mihomo"
	ReasonDNSViaHijack = "system-dns/via-hijack"

	// vpn-conflict
	ReasonVPNOther      = "vpn-conflict/other-vpn"
	ReasonVPNTailscale  = "vpn-conflict/tailscale-ne"
	ReasonVPNExitNode   = "vpn-conflict/exit-node"
	ReasonVPNOtherTUN   = "vpn-conflict/other-tunnel"
	ReasonVPNNotDefault = "vpn-conflict/tun-not-default"

	// leak-guard
	ReasonLeakMode     = "leak-guard/mode"
	ReasonLeakNoGroup  = "leak-guard/no-group"
	ReasonLeakSelected = "leak-guard/selected-direct"
	ReasonLeakMayGo    = "leak-guard/may-go-direct"

	// leak-guard-egress
	ReasonEgressFailed = "leak-guard-egress/failed"
	ReasonEgressSame   = "leak-guard-egress/same-ip"

	// controller-security
	ReasonCtlExposed  = "controller-security/exposed"
	ReasonCtlNoSecret = "controller-security/no-secret"

	// controller-owner: another program's mihomo (Clash Party, Clash
	// Verge Rev…) holds the controller address. Quitting it is up to the
	// user (other-client); moving Twinnel's controller to a free port is
	// one click (port-shared).
	ReasonOwnerOtherClient = "controller-owner/other-client"
	ReasonOwnerPortShared  = "controller-owner/port-shared"
)

// FixKind is how a reason is fixed.
type FixKind string

const (
	// FixAuto: one click, through the helper (or the app's own
	// controller/Tailscale client), with a preview and, where possible,
	// an undo.
	FixAuto FixKind = "auto"
	// FixGuided: a place in the app where the user changes it.
	FixGuided FixKind = "guided"
	// FixManual: Twinnel cannot fix it; the text says why and what to do
	// outside (Open names a system setting to open, if any).
	FixManual FixKind = "manual"
)

// More fix action codes (FixHint.AutoFix, FixSpec.Action) besides the
// ones in accept.go.
const (
	FixControllerLoopback = "controller-loopback" // an override binding the controller to 127.0.0.1
	FixModeRule           = "mode-rule"           // switch mihomo back to rule mode
	FixRestartMihomo      = "restart-mihomo"
	FixRestartTailscaled  = "restart-tailscaled"
	FixRerun              = "rerun"           // run the checks again
	FixControllerPort     = "controller-port" // move Twinnel's controller to a free port (controller.move)
)

// Guided targets (FixSpec.Target): where in the app the user changes it.
const (
	TargetOverrides = "config/overrides"  // 订阅与覆写: the overrides list
	TargetLeakGuard = "config/leak-guard" // 订阅与覆写: leak guard's groups
	TargetProxies   = "proxies/group"     // 节点: the group named by FixHint.Param
)

// System places a manual fix can open (FixSpec.Open).
const (
	OpenVPNSettings     = "os/vpn"
	OpenNetworkSettings = "os/network"
)

// FixSpec is how one reason is fixed.
type FixSpec struct {
	Reason string  `json:"reason"`
	Kind   FixKind `json:"kind"`
	// Action is the one-click fix (FixAuto); also offered with a guided
	// or manual fix when there is one that helps (FixRerun).
	Action string `json:"action,omitempty"`
	// Target is where a guided fix takes the user.
	Target string `json:"target,omitempty"`
	// Open is a system setting a manual fix can open.
	Open string `json:"open,omitempty"`
}

// fixCatalog: every reason and its fix. Platform-specific entries are
// keyed "reason@platform" and win over the plain one.
var fixCatalog = map[string]FixSpec{
	ReasonReadFailed:    {Kind: FixAuto, Action: FixRerun},
	ReasonCheckTimeout:  {Kind: FixAuto, Action: FixRerun},
	ReasonCheckPanicked: {Kind: FixManual, Action: FixRerun},

	ReasonRouteNoIface:    {Kind: FixAuto, Action: FixRestartTailscaled},
	ReasonRouteLookup:     {Kind: FixAuto, Action: FixRerun},
	ReasonRouteIntoMihomo: {Kind: FixAuto, Action: FixCoexist},
	ReasonRouteElsewhere:  {Kind: FixAuto, Action: FixRestartTailscaled},

	ReasonExcludeMissing: {Kind: FixAuto, Action: FixCoexist},

	ReasonRulesGlobal:    {Kind: FixAuto, Action: FixModeRule},
	ReasonRulesNotDirect: {Kind: FixAuto, Action: FixCoexist},
	ReasonRulesShadowed:  {Kind: FixAuto, Action: FixCoexist},

	ReasonMagicNoResolver: {Kind: FixManual, Open: OpenNetworkSettings},
	ReasonMagicSystem:     {Kind: FixAuto, Action: FixCoexist},
	ReasonMagicMihomo:     {Kind: FixAuto, Action: FixCoexist},

	ReasonNamesNone:     {Kind: FixAuto, Action: FixCoexist},
	ReasonNamesOutdated: {Kind: FixAuto, Action: FixCoexist},

	ReasonBindStale:   {Kind: FixAuto, Action: FixCoexist},
	ReasonBindMissing: {Kind: FixAuto, Action: FixCoexist},

	ReasonFakeIPWildcard: {Kind: FixGuided, Target: TargetOverrides},

	ReasonDNSNotMihomo: {Kind: FixAuto, Action: FixSystemDNS},
	ReasonDNSViaHijack: {Kind: FixAuto, Action: FixSystemDNS},
	// The helper cannot set the system DNS on Linux yet (no
	// engine.SystemDNS there): the text says how.
	ReasonDNSNotMihomo + "@" + Linux: {Kind: FixManual, Open: OpenNetworkSettings},
	ReasonDNSViaHijack + "@" + Linux: {Kind: FixManual, Open: OpenNetworkSettings},

	ReasonVPNOther:      {Kind: FixManual, Open: OpenVPNSettings},
	ReasonVPNTailscale:  {Kind: FixManual, Open: OpenVPNSettings},
	ReasonVPNExitNode:   {Kind: FixAuto, Action: FixExitNodeOff},
	ReasonVPNOtherTUN:   {Kind: FixManual, Open: OpenVPNSettings},
	ReasonVPNNotDefault: {Kind: FixAuto, Action: FixRestartMihomo},

	ReasonLeakMode:     {Kind: FixAuto, Action: FixModeRule},
	ReasonLeakNoGroup:  {Kind: FixGuided, Target: TargetLeakGuard},
	ReasonLeakSelected: {Kind: FixAuto, Action: FixLeakGuard},
	ReasonLeakMayGo:    {Kind: FixAuto, Action: FixLeakGuard},

	ReasonEgressFailed: {Kind: FixAuto, Action: FixEgressProbe},
	ReasonEgressSame:   {Kind: FixGuided, Target: TargetProxies},

	ReasonCtlExposed:  {Kind: FixAuto, Action: FixControllerLoopback},
	ReasonCtlNoSecret: {Kind: FixAuto, Action: FixController},

	ReasonOwnerOtherClient: {Kind: FixManual, Action: FixRerun},
	ReasonOwnerPortShared:  {Kind: FixAuto, Action: FixControllerPort},
}

// FixFor returns how reason is fixed on platform.
func FixFor(reason, platform string) (FixSpec, bool) {
	f, ok := fixCatalog[reason+"@"+platform]
	if !ok {
		f, ok = fixCatalog[reason]
	}
	f.Reason = reason
	return f, ok
}

// Reasons returns every reason code in the catalog, without platform
// variants.
func Reasons() []string {
	var out []string
	for k := range fixCatalog {
		if !hasPlatform(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func hasPlatform(k string) bool {
	for i := 0; i < len(k); i++ {
		if k[i] == '@' {
			return true
		}
	}
	return false
}

// because adds reason to r's reasons once.
func (r *Result) because(reason string) {
	for _, x := range r.Reasons {
		if x == reason {
			return
		}
	}
	r.Reasons = append(r.Reasons, reason)
}

// withReason returns r with reason added.
func withReason(r Result, reason string) Result {
	r.because(reason)
	return r
}

// causeReason is the reason of each vpn-conflict cause.
var causeReason = map[string]string{
	CauseExitNode: ReasonVPNExitNode,
	CauseOtherVPN: ReasonVPNOther,
	CauseTSNE:     ReasonVPNTailscale,
	CauseOtherTUN: ReasonVPNOtherTUN,
	CauseRoute:    ReasonVPNNotDefault,
}

// removeReason returns rs without reason.
func removeReason(rs []string, reason string) []string {
	var out []string
	for _, r := range rs {
		if r != reason {
			out = append(out, r)
		}
	}
	return out
}
