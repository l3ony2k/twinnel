package rpc

import (
	"encoding/json"
	"time"
)

// Methods served by the helper. This is the whole allowlist: the server
// refuses any other name.
const (
	MethodStatus = "status.get"

	MethodComposePreview = "compose.preview"
	MethodComposeApply   = "compose.apply"

	MethodHistoryList     = "history.list"
	MethodHistoryRollback = "history.rollback"

	MethodLayerList       = "layer.list"
	MethodLayerGet        = "layer.get"
	MethodLayerPut        = "layer.put"
	MethodLayerDelete     = "layer.delete"
	MethodLayerSetEnabled = "layer.set_enabled"
	MethodLayerReorder    = "layer.reorder"
	MethodLayerRestore    = "layer.restore"

	MethodSubList     = "subscription.list"
	MethodSubPut      = "subscription.put"
	MethodSubDelete   = "subscription.delete"
	MethodSubActivate = "subscription.activate"
	MethodSubRefresh  = "subscription.refresh"

	MethodSettingsGet    = "settings.get"
	MethodSettingsUpdate = "settings.update"

	MethodTailnetRefresh = "tailnet.refresh"

	MethodKernelList     = "kernel.list"
	MethodKernelCheck    = "kernel.check"
	MethodKernelInstall  = "kernel.install"
	MethodKernelRollback = "kernel.rollback"

	MethodTailscaleInfo = "tailscale.info"

	MethodServiceStatus    = "service.status"
	MethodServiceStart     = "service.start"
	MethodServiceStop      = "service.stop"
	MethodServiceRestart   = "service.restart"
	MethodServiceInstall   = "service.install"   // mihomo only
	MethodServiceUninstall = "service.uninstall" // mihomo only

	MethodHealthLast   = "health.last"
	MethodHealthRun    = "health.run"
	MethodHealthEgress = "health.egress" // run the egress test once and keep its result

	MethodConfigGet = "config.get"

	MethodGeoCheck  = "geo.check"
	MethodGeoUpdate = "geo.update"
	MethodUICheck   = "ui.check"
	MethodUIUpdate  = "ui.update"

	MethodTakeoverPlan    = "takeover.plan"
	MethodTakeoverPlans   = "takeover.plans"
	MethodTakeoverDiscard = "takeover.discard"
	MethodTakeoverAdopt   = "takeover.adopt"

	MethodControllerSecure = "controller.secure"

	// Moving an adopted mihomo to the managed kernel, a root-owned config
	// dir and a Twinnel LaunchDaemon (security review finding 5).
	MethodHardenPlan  = "service.harden.plan"
	MethodHardenApply = "service.harden.apply"
	MethodHardenPlans = "service.harden.plans" // read: stored plans

	MethodEventsWait = "events.wait"
)

// Event types pushed through events.wait.
const (
	EventComposeApplied   = "compose.applied"
	EventComposeFailed    = "compose.failed"
	EventStateChanged     = "state.changed" // layers, subscriptions or settings edited
	EventSubUpdated       = "subscription.updated"
	EventSubFailed        = "subscription.failed"
	EventTailnetChanged   = "tailnet.changed"
	EventKernelChanged    = "kernel.changed"
	EventCheckChanged     = "check.changed"     // one health check changed status (data: health.Change)
	EventHealthUpdated    = "health.updated"    // a health run finished (data: HealthSummary)
	EventAccessoryChanged = "accessory.changed" // geo databases or the external UI were updated
	EventServiceChanged   = "service.changed"
	EventTakeoverChanged  = "takeover.changed" // a plan was made, discarded, adopted or rolled back
	EventHelperStarted    = "helper.started"
	EventHelperStopping   = "helper.stopping"
	EventEventsTruncated  = "events.truncated" // the client asked for events older than the buffer
)

// Empty is the params/result of methods without any.
type Empty struct{}

// ---- status ----

type Status struct {
	HelperVersion string    `json:"helper_version"`
	PID           int       `json:"pid"`
	UID           int       `json:"uid"`
	StateDir      string    `json:"state_dir"`
	StartedAt     time.Time `json:"started_at"`

	Mihomo             MihomoStatus   `json:"mihomo"`
	ActiveSubscription string         `json:"active_subscription,omitempty"`
	Layers             int            `json:"layers"`
	LastApply          *ApplyResult   `json:"last_apply,omitempty"`
	Tailnet            TailnetStatus  `json:"tailnet"`
	Kernel             KernelList     `json:"kernel"`
	Subscriptions      []Subscription `json:"subscriptions,omitempty"`
}

type MihomoStatus struct {
	Binary     string `json:"binary"`
	ConfigPath string `json:"config_path"`
	Controller string `json:"controller"`
	Reachable  bool   `json:"reachable"`
	Version    string `json:"version,omitempty"` // from the controller's /version
	Error      string `json:"error,omitempty"`
}

type TailnetStatus struct {
	Coexist bool     `json:"coexist"`
	Peers   []string `json:"peers,omitempty"`
	Suffix  string   `json:"suffix,omitempty"`
	// Interface carries 100.100.100.100 (e.g. utun9); the coexist layer
	// binds the tailnet resolver to it.
	Interface string    `json:"interface,omitempty"`
	LastPoll  time.Time `json:"last_poll,omitzero"`
	Error     string    `json:"error,omitempty"`
}

// ---- compose ----

type ApplyParams struct {
	Reason string `json:"reason,omitempty"`
	// ReasonCode and ReasonArgs: the reason as a code (core/store
	// Reason… constants) for a client to word; Reason is then the English
	// text. Empty: Reason is the user's own words (or empty).
	ReasonCode string   `json:"reason_code,omitempty"`
	ReasonArgs []string `json:"reason_args,omitempty"`
	// Force reloads even when the composed config equals the file on disk.
	Force bool `json:"force,omitempty"`
}

type ComposeResult struct {
	Applied        []string `json:"applied"`
	Warnings       []string `json:"warnings,omitempty"`
	Hash           string   `json:"hash"` // sha256 of the composed config, without the controller secret
	Size           int      `json:"size"`
	Valid          bool     `json:"valid"`
	ValidateErrors []string `json:"validate_errors,omitempty"`
	Changed        bool     `json:"changed"` // differs from the config file on disk
	// Messages are Warnings as codes, in the same order, for a client to
	// word in its language (core/msg). A warning the helper has no code
	// for has Code "" and only Text.
	Messages []Msg `json:"messages,omitempty"`
	// Culprits: when mihomo -t rejected the config, the overrides (or
	// the subscription, Layer "") that hold a name its errors quote.
	Culprits []Culprit `json:"culprits,omitempty"`
}

// Culprit is where a name mihomo -t complained about comes from.
type Culprit struct {
	Name  string `json:"name"`
	Layer string `json:"layer,omitempty"` // an override's ID; "" for the subscription
	Title string `json:"title,omitempty"` // the override's name
}

// Msg is a text the helper hands to a client as a code with arguments,
// plus its English wording (Text) for the CLI and for codes a client does
// not know.
type Msg struct {
	Code string   `json:"code,omitempty"`
	Args []string `json:"args,omitempty"`
	Text string   `json:"text"`
}

type ApplyResult struct {
	ComposeResult
	Reason string `json:"reason,omitempty"`
	// ReasonCode and ReasonArgs: Reason as a code (core/store Reason…).
	ReasonCode string   `json:"reason_code,omitempty"`
	ReasonArgs []string `json:"reason_args,omitempty"`
	Written    bool     `json:"written"`
	Reloaded   bool     `json:"reloaded"`
	RolledBack bool     `json:"rolled_back,omitempty"` // the new config failed after writing and the old one was restored
	HistoryID  string   `json:"history_id,omitempty"`
	Error      string   `json:"error,omitempty"`
	// ErrorMsg is Error as a code (core/msg), when the helper has one.
	ErrorMsg *Msg      `json:"error_msg,omitempty"`
	Time     time.Time `json:"time"`
	Duration string    `json:"duration"`
	// Impact: what the hot reload did to the running mihomo (set when a
	// previous config file existed to compare with).
	Impact *ReloadImpact `json:"impact,omitempty"`
}

// ReloadImpact says what a hot reload (PUT /configs) of mihomo does to
// traffic, judged by comparing the old and the new config the way mihomo
// v1.19 does (see docs/reload-behavior.md).
//
// Every hot reload has a short pause (milliseconds): connections opened
// during it are closed at once and UDP packets are dropped. Open
// connections, DNS, fake-ip mappings and selected nodes are kept. The fields
// below list what goes beyond that.
type ReloadImpact struct {
	// Disruptive: something below interrupts the network or resets open
	// connections. The UI shows "会短暂中断网络".
	Disruptive bool `json:"disruptive"`
	// TUNRebuild: the tun section (or the fake-ip range it takes its
	// address from) differs, so mihomo closes the TUN device and creates it
	// again: routes are removed and re-added and every flow through the TUN
	// is cut.
	TUNRebuild bool `json:"tun_rebuild,omitempty"`
	// DNSListener: dns.listen (or dns.enable) changed, so the DNS server is
	// closed and bound again.
	DNSListener bool `json:"dns_listener,omitempty"`
	// Listeners: inbound settings that changed (port, mixed-port,
	// allow-lan, ...). They are applied only with force=true, which
	// re-creates the changed listeners.
	Listeners []string `json:"listeners,omitempty"`
	// Force: the reload was (or will be) sent with force=true.
	Force bool `json:"force"`
	// ProviderConnsReset: proxy providers whose open connections mihomo
	// closes on every reload.
	ProviderConnsReset []string `json:"provider_conns_reset,omitempty"`
	// SelectionReset: profile.store-selected is off, so the select groups
	// go back to their first entry.
	SelectionReset bool `json:"selection_reset,omitempty"`
	// RestartNeeded: settings a hot reload does not apply (the external
	// controller and its secret); they take effect when mihomo restarts.
	RestartNeeded []string `json:"restart_needed,omitempty"`
	// TUNWhy: what made the TUN device be rebuilt: "tun" (the tun
	// section) and/or "fake-ip-range".
	TUNWhy []string `json:"tun_why,omitempty"`
	// Unknown: the configs could not be compared; the worst is assumed.
	Unknown bool `json:"unknown,omitempty"`
	// Notes explain the above in English (the fields are the codes).
	Notes []string `json:"notes,omitempty"`
}

// ---- history ----

type HistoryEntry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Hash   string    `json:"hash"`
	Size   int       `json:"size"`
	Kind   string    `json:"kind"` // "applied", "backup" (file found on disk), "rollback"
	Reason string    `json:"reason,omitempty"`
	// ReasonCode and ReasonArgs: Reason as a code (core/store Reason…),
	// empty when Reason is the user's own text.
	ReasonCode string   `json:"reason_code,omitempty"`
	ReasonArgs []string `json:"reason_args,omitempty"`
	// BeforeTwinnel: the config as it was before Twinnel first wrote it.
	BeforeTwinnel bool `json:"before_duet,omitempty"` // stored name, kept from before the rename
	// Layers: the overrides the config was composed from; nil for a
	// backup or an entry older than the manifest.
	Layers *LayerManifest `json:"layers,omitempty"`
}

// LayerManifest is the set of overrides a config was composed from.
type LayerManifest struct {
	Subscription string          `json:"subscription,omitempty"`
	Layers       []ManifestLayer `json:"layers"`
	Coexist      bool            `json:"coexist,omitempty"`
	LeakGuard    []string        `json:"leak_guard,omitempty"`
}

// ManifestLayer is one stored layer of a manifest.
type ManifestLayer struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Stage    string   `json:"stage"`
	Format   string   `json:"format"`
	Enabled  bool     `json:"enabled"`
	Hash     string   `json:"hash"`
	NameCode string   `json:"name_code,omitempty"`
	NameArgs []string `json:"name_args,omitempty"`
}

// LayerRestoreParams sets the overrides back to a history entry's
// manifest: every layer (IDs empty), or only the layers in IDs.
type LayerRestoreParams struct {
	HistoryID string   `json:"history_id"`
	IDs       []string `json:"ids,omitempty"`
}

// LayerRestoreResult lists what layer.restore changed.
type LayerRestoreResult struct {
	Restored []string `json:"restored"`          // layer IDs written back
	Removed  []string `json:"removed,omitempty"` // layers not in the manifest, deleted
}

type IDParams struct {
	ID string `json:"id"`
}

// ---- layers ----

type Layer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Stage   string `json:"stage"`  // "base" or "user"; managed layers show "coexist"/"guard"
	Format  string `json:"format"` // "merge" (Clash Party syntax) or "verge" (Clash Verge Rev Merge)
	Enabled bool   `json:"enabled"`
	Managed bool   `json:"managed,omitempty"` // generated by the helper; content is read-only
	Size    int    `json:"size"`
	Hash    string `json:"hash,omitempty"` // sha256 of a stored layer's content
	// NameCode and NameArgs name a generated Name (a managed layer, or one
	// adopted by takeover and not renamed since) so a client can word it in
	// its own language (core/i18n TakeoverLayerName). Name stays English.
	NameCode string   `json:"name_code,omitempty"`
	NameArgs []string `json:"name_args,omitempty"`
}

type LayerContent struct {
	Layer
	Content string `json:"content"`
}

type LayerPutParams struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Stage   string `json:"stage,omitempty"`   // default "user"
	Format  string `json:"format,omitempty"`  // default "merge"
	Enabled *bool  `json:"enabled,omitempty"` // default true for new layers, unchanged otherwise
	Content string `json:"content"`
}

type LayerEnableParams struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type ReorderParams struct {
	IDs []string `json:"ids"`
}

// ---- subscriptions ----

type Subscription struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Active          bool      `json:"active"`
	HasURL          bool      `json:"has_url"` // the URL itself is a secret and never leaves the helper
	IntervalMinutes int       `json:"interval_minutes"`
	LastFetch       time.Time `json:"last_fetch,omitzero"`
	LastChange      time.Time `json:"last_change,omitzero"`
	Hash            string    `json:"hash,omitempty"`
	Size            int       `json:"size"`
	LastError       string    `json:"last_error,omitempty"`
	// Warnings are set by subscription.put only (e.g. a plain-http URL).
	Warnings []string `json:"warnings,omitempty"`
}

type SubPutParams struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	URL             string `json:"url,omitempty"`              // http(s); empty keeps the stored URL
	IntervalMinutes *int   `json:"interval_minutes,omitempty"` // 0 = manual only
	// Content sets the raw subscription directly (no URL, or an offline
	// import). Empty keeps the stored content.
	Content string `json:"content,omitempty"`
}

type SubRefreshResult struct {
	Changed bool         `json:"changed"`
	Hash    string       `json:"hash"`
	Via     string       `json:"via"` // "proxy" or "direct"
	Apply   *ApplyResult `json:"apply,omitempty"`
}

// ---- settings (only fields that are not paths) ----

type Settings struct {
	LeakGuardGroups   []string `json:"leak_guard_groups"`
	TailscaleCoexist  bool     `json:"tailscale_coexist"`
	TailscaleResolver string   `json:"tailscale_resolver,omitempty"`
	HistoryKeep       int      `json:"history_keep"`
	KernelChannel     string   `json:"kernel_channel"` // "stable" or "alpha"
	AutoApply         bool     `json:"auto_apply"`     // re-compose and apply after subscription/tailnet changes
	// HealthEgress: the egress test runs with every health run (定期检测).
	HealthEgress bool `json:"health_egress"`
	// Drift: mihomo runs a history entry the overrides do not compose
	// (after a rollback); automatic re-compose waits until an apply.
	Drift *Drift `json:"drift,omitempty"`
	// Agent: 设置 → 本地 Agent (MCP and its tiers).
	Agent AgentSettings `json:"agent"`
}

// Drift says mihomo runs a rolled-back history entry, not the overrides.
type Drift struct {
	Since     time.Time `json:"since"`
	HistoryID string    `json:"history_id"`
	Manifest  bool      `json:"manifest,omitempty"` // layer.restore can set the overrides back to it
}

type SettingsUpdate struct {
	LeakGuardGroups   *[]string `json:"leak_guard_groups,omitempty"`
	TailscaleCoexist  *bool     `json:"tailscale_coexist,omitempty"`
	TailscaleResolver *string   `json:"tailscale_resolver,omitempty"`
	HistoryKeep       *int      `json:"history_keep,omitempty"`
	KernelChannel     *string   `json:"kernel_channel,omitempty"`
	AutoApply         *bool     `json:"auto_apply,omitempty"`
	HealthEgress      *bool     `json:"health_egress,omitempty"`
	// Agent changes the local agent switches. `twinnel mcp` never sends it:
	// an agent cannot widen its own tiers.
	Agent *AgentSettingsUpdate `json:"agent,omitempty"`
}

// ---- tailnet ----

type TailnetRefreshResult struct {
	Changed bool          `json:"changed"`
	Tailnet TailnetStatus `json:"tailnet"`
	Apply   *ApplyResult  `json:"apply,omitempty"`
}

// ---- kernels ----

type KernelVersion struct {
	Version  string    `json:"version"`
	Path     string    `json:"path"`
	Current  bool      `json:"current,omitempty"`
	Previous bool      `json:"previous,omitempty"`
	Time     time.Time `json:"time,omitzero"`
}

type KernelList struct {
	// Active is the binary the helper uses for mihomo -t and the service:
	// the managed "current" kernel, or the adopted external binary.
	Active    string          `json:"active"`
	External  bool            `json:"external,omitempty"` // Active is an adopted binary outside the state dir
	Current   string          `json:"current,omitempty"`
	Previous  string          `json:"previous,omitempty"`
	Installed []KernelVersion `json:"installed,omitempty"`
}

type KernelCheckParams struct {
	Channel string `json:"channel,omitempty"` // "stable" (default) or "alpha"
}

type KernelRelease struct {
	Channel         string    `json:"channel"`
	Version         string    `json:"version"`
	Tag             string    `json:"tag"`
	Asset           string    `json:"asset"`
	Size            int64     `json:"size"`
	Digest          string    `json:"digest,omitempty"` // "sha256:..."
	Published       time.Time `json:"published,omitzero"`
	Current         string    `json:"current,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
}

type KernelInstallParams struct {
	Channel string `json:"channel,omitempty"`
	// Version, if set, must equal the version the release check finds;
	// it guards against installing something other than what the user saw.
	Version string `json:"version,omitempty"`
}

type TailscaleInfo struct {
	Installed       bool   `json:"installed"`
	Path            string `json:"path,omitempty"`
	Version         string `json:"version,omitempty"`
	InstallKind     string `json:"install_kind,omitempty"` // "brew", "standalone", "appstore", "static", "unknown"
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

// ---- services ----

type ServiceParams struct {
	Name string `json:"name"` // "mihomo", "tailscaled" or "helper"
}

type ServiceStatus struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Installed    bool   `json:"installed"` // definition file exists
	Loaded       bool   `json:"loaded"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	LastExitCode *int   `json:"last_exit_code,omitempty"`
	Runs         int    `json:"runs,omitempty"`
	Program      string `json:"program,omitempty"`
	Definition   string `json:"definition,omitempty"` // path of the plist / unit
	Error        string `json:"error,omitempty"`
}

// ---- events ----

type Event struct {
	Seq  uint64          `json:"seq"`
	Time time.Time       `json:"time"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type EventsWaitParams struct {
	// After is the last Seq the client has seen; 0 returns everything still
	// buffered. Use Next from the previous result.
	After uint64 `json:"after"`
	// TimeoutMS bounds the wait (default 25000, max 120000). 0 events and
	// the same Next come back when it expires.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

type EventsWaitResult struct {
	Events []Event `json:"events"`
	Next   uint64  `json:"next"`
}

// ServiceOpResult is the result of service.start|stop|restart|install|uninstall.
type ServiceOpResult struct {
	Name  string   `json:"name"`
	Label string   `json:"label"`
	Op    string   `json:"op"`
	Steps []string `json:"steps"` // the plan, one shell-like line per step
	// DryRun is true when the helper is not running as root: the plan was
	// computed but nothing was run (development helpers).
	DryRun bool          `json:"dry_run,omitempty"`
	Status ServiceStatus `json:"status"` // after the operation
}

// ---- health ----

// HealthRunParams are the params of health.run.
type HealthRunParams struct {
	Reason string `json:"reason,omitempty"` // default "manual"
	// Lang is the language to word the report in (core/i18n tag, e.g.
	// "en"); the helper keeps it for its scheduled runs. "" keeps the
	// language the helper uses now.
	Lang string `json:"lang,omitempty"`
	// IPs asks for the egress test's unmasked addresses in Egress (the
	// app shows them; reports, the CLI and MCP never ask).
	IPs bool `json:"ips,omitempty"`
}

// HealthEgressParams are the params of health.egress: the egress test
// (leak-guard-egress) runs once now, whether or not it runs with every
// health run; the helper keeps its result, with its time, in later
// reports until it is stale. The result is the new report.
type HealthEgressParams struct {
	Lang string `json:"lang,omitempty"`
	// IPs asks for the egress test's unmasked addresses in Egress (the
	// app shows them; reports, the CLI and MCP never ask).
	IPs bool `json:"ips,omitempty"`
}

// HealthLastParams are the params of health.last. A Lang other than the
// last report's makes the helper run the checks again in it (an event
// health.updated follows); the call still returns the report it has.
type HealthLastParams struct {
	Lang string `json:"lang,omitempty"`
	// IPs asks for the egress test's unmasked addresses in Egress (the
	// app shows them; reports, the CLI and MCP never ask).
	IPs bool `json:"ips,omitempty"`
}

// HealthReport is the result of health.last and health.run. Report is a
// core/health.Report in JSON form (decode it with encoding/json into
// health.Report); nil before the first run.
type HealthReport struct {
	Report json.RawMessage `json:"report,omitempty"`
	Reason string          `json:"reason,omitempty"`
	At     time.Time       `json:"at,omitzero"`
	// Egress is the egress check's unmasked addresses, only when the
	// params asked (IPs) and the check measured any.
	Egress *EgressIPs `json:"egress,omitempty"`
}

// EgressIPs are the public addresses the egress test saw (core/health
// EgressIPs): through mihomo, by which node, and directly. "" = not
// measured.
type EgressIPs struct {
	Proxied string `json:"proxied,omitempty"`
	Direct  string `json:"direct,omitempty"`
	Node    string `json:"node,omitempty"`
}

// HealthSummary is the data of health.updated.
type HealthSummary struct {
	Reason  string         `json:"reason"`
	Overall string         `json:"overall"`
	Counts  map[string]int `json:"counts"`
	At      time.Time      `json:"at"`
}

// ---- config text ----

// Config sources of config.get.
const (
	ConfigLive    = "live"    // the config file on disk
	ConfigPreview = "preview" // what compose.apply would write now
	ConfigHistory = "history" // a history entry (needs ID)
)

type ConfigGetParams struct {
	Source string `json:"source"`
	ID     string `json:"id,omitempty"`
}

// ConfigText is a config with the top-level secret removed (the form history
// keeps), so texts from all sources compare cleanly.
type ConfigText struct {
	Source  string `json:"source"`
	ID      string `json:"id,omitempty"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
	Size    int    `json:"size"`
}

// ---- accessories (geo databases, external UI) ----

// AccessoryFile is one geo database or the UI bundle.
type AccessoryFile struct {
	Name       string `json:"name"` // "geoip", "geosite", "mmdb", "asn", "ui"
	File       string `json:"file"` // path in the config dir
	URL        string `json:"url"`
	Installed  bool   `json:"installed"`
	Size       int64  `json:"size,omitempty"`   // local size
	SHA256     string `json:"sha256,omitempty"` // local sha256 (files) / installed tag (ui)
	Remote     string `json:"remote,omitempty"` // remote sha256 or tag, when known
	RemoteSize int64  `json:"remote_size,omitempty"`
	// Verifiable is true when the remote publishes a digest the download
	// is checked against.
	Verifiable      bool   `json:"verifiable"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

type AccessoryCheck struct {
	Dir   string          `json:"dir"`
	Files []AccessoryFile `json:"files"`
}

type AccessoryUpdateParams struct {
	// Force downloads even files that look up to date.
	Force bool `json:"force,omitempty"`
}

type AccessoryUpdateResult struct {
	Updated []string        `json:"updated"`
	Skipped []string        `json:"skipped,omitempty"`
	Files   []AccessoryFile `json:"files"`
	// RolledBack is true when a later file failed and every file already
	// replaced in this run was put back.
	RolledBack bool `json:"rolled_back,omitempty"`
	// ReloadNeeded: mihomo reads geo files / serves the UI from disk; a
	// geo change takes effect after compose.apply with force or a restart.
	ReloadNeeded bool   `json:"reload_needed,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ---- takeover ----

// TakeoverPlanParams asks the helper to inspect the running setup and
// write a plan under <state>/takeover/<plan_id>/. Base may only name one of
// the base candidates the helper found itself (default: the best one).
type TakeoverPlanParams struct {
	Base string `json:"base,omitempty"`
}

// TakeoverLayer is one layer the plan would import (contents stay in the
// helper).
type TakeoverLayer struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Stage string   `json:"stage"`
	Keys  []string `json:"keys,omitempty"`
	Rules int      `json:"rules,omitempty"`
	// NameCode and NameArgs name a generated Name so a client can word it
	// in its own language (core/i18n TakeoverLayerName); empty for a name
	// taken from the person's own config. Name stays the English text.
	NameCode string   `json:"name_code,omitempty"`
	NameArgs []string `json:"name_args,omitempty"`
}

// TakeoverChange is a behavior difference between live and composed.
type TakeoverChange struct {
	Area       string `json:"area"`
	What       string `json:"what"`
	Deliberate bool   `json:"deliberate"`
	Why        string `json:"why,omitempty"`
	// Code/Args and WhyCode/WhyArgs name What and Why so a client can word
	// them in its own language (core/i18n TakeoverChange, TakeoverWhy).
	// What and Why stay the English texts (the CLI shows them).
	Code    string   `json:"code,omitempty"`
	Args    []string `json:"args,omitempty"`
	WhyCode string   `json:"why_code,omitempty"`
	WhyArgs []string `json:"why_args,omitempty"`
	// Keys are the entries a counts change touched (samples for the UI).
	Keys []string `json:"keys,omitempty"`
}

// Plan states.
const (
	PlanReady      = "ready"       // made, not adopted
	PlanAdopted    = "adopted"     // adopted; the plan dir holds the live backup
	PlanRolledBack = "rolled_back" // adopt ran and was rolled back
	PlanFailed     = "failed"      // adopt failed before or during apply; nothing changed
)

// TakeoverPlan is a plan summary. It holds no credentials or subscription
// URLs. Summary and SemDiff are the plan's own redacted texts (omitted in
// takeover.plans).
type TakeoverPlan struct {
	PlanID    string    `json:"plan_id"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"`
	// ConfirmHash must be passed back to takeover.adopt: it proves the
	// client showed this exact plan.
	ConfirmHash string `json:"confirm_hash"`

	LiveConfig    string `json:"live_config"`
	LiveSHA256    string `json:"live_sha256"`
	Base          string `json:"base,omitempty"`
	MihomoService string `json:"mihomo_service,omitempty"`
	MihomoBinary  string `json:"mihomo_binary,omitempty"`
	MihomoVersion string `json:"mihomo_version,omitempty"`
	Controller    string `json:"controller,omitempty"`
	SecretSet     bool   `json:"secret_set"`
	TailscaleSvc  string `json:"tailscale_service,omitempty"`
	// SubscriptionURL: a profile index next to the setup names the
	// subscription URL; adopt stores it in the secret store.
	SubscriptionURL bool `json:"subscription_url"`
	// Candidates are the possible bases the helper found (one Chosen).
	Candidates []TakeoverCandidate `json:"candidates,omitempty"`

	Layers      []TakeoverLayer  `json:"layers"`
	Changes     []TakeoverChange `json:"changes,omitempty"`
	Equivalent  bool             `json:"equivalent"`
	Validated   bool             `json:"validated"` // mihomo -t ran and passed
	ValidateErr []string         `json:"validate_errors,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	SemDiff     string           `json:"semdiff,omitempty"`
	// Impact of the adopt's reload on the running mihomo (live config →
	// composed config).
	Impact *ReloadImpact `json:"impact,omitempty"`
	// Effects say what adopt does to each existing piece (service,
	// binary, config file, subscription, secret, Tailscale).
	Effects []TakeoverEffect `json:"effects,omitempty"`

	AdoptedAt time.Time `json:"adopted_at,omitzero"`
	// Backup is the exact copy of the live config taken before adopt.
	Backup string `json:"backup,omitempty"`
}

// TakeoverPlansParams: with PlanID, only that plan is returned, with its
// Summary and SemDiff texts.
type TakeoverPlansParams struct {
	PlanID string `json:"plan_id,omitempty"`
}

// TakeoverDiscardParams removes a plan dir. An adopted plan holds the live
// backup and is kept unless Force.
type TakeoverDiscardParams struct {
	PlanID string `json:"plan_id"`
	Force  bool   `json:"force,omitempty"`
}

// AdoptParams selects a takeover plan the helper generated itself (via
// core/takeover); the client never passes paths.
type AdoptParams struct {
	PlanID      string `json:"plan_id"`
	ConfirmHash string `json:"confirm_hash"`
	// SkipHealth adopts without the before/after health comparison (and so
	// without automatic rollback on a worse result). For helpers without
	// health checks.
	SkipHealth bool `json:"skip_health,omitempty"`
}

// AdoptStep is one step of takeover.adopt.
type AdoptStep struct {
	Name string `json:"name"`
	// Code is Name as a stable code (verify, backup, import,
	// health-before, apply, health-after, rollback, restore, undo-import)
	// for a client to word; Detail stays English.
	Code   string `json:"code,omitempty"`
	Status string `json:"status"` // ok, failed, skipped, warn
	Detail string `json:"detail,omitempty"`
}

// AdoptCheck is one health check result around an adopt.
type AdoptCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

// AdoptResult reports a takeover.adopt run. OK is false when adopt
// failed or was rolled back; Error says why. A refusal before any change
// is an RPC error instead.
type AdoptResult struct {
	PlanID string       `json:"plan_id"`
	OK     bool         `json:"ok"`
	Steps  []AdoptStep  `json:"steps"`
	Apply  *ApplyResult `json:"apply,omitempty"`
	// HistoryID is the applied config in history; BackupHistoryID the
	// live file as it was (secret stripped). Backup is the exact copy.
	HistoryID       string `json:"history_id,omitempty"`
	BackupHistoryID string `json:"backup_history_id,omitempty"`
	Backup          string `json:"backup,omitempty"`

	HealthBefore        []AdoptCheck `json:"health_before,omitempty"`
	HealthAfter         []AdoptCheck `json:"health_after,omitempty"`
	HealthAfterRollback []AdoptCheck `json:"health_after_rollback,omitempty"`
	// Worse lists checks that got worse after the apply.
	Worse      []string `json:"worse,omitempty"`
	RolledBack bool     `json:"rolled_back"`
	Warnings   []string `json:"warnings,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// ControllerSecureResult reports controller.secure: a new controller
// secret was generated, kept in the secret store and applied. The secret
// itself is never returned.
type ControllerSecureResult struct {
	Replaced bool         `json:"replaced"` // a secret existed before
	Apply    *ApplyResult `json:"apply,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
}

// ---- service.harden ----

// Harden plan states (besides PlanReady, PlanRolledBack, PlanFailed).
const PlanApplied = "applied"

type HardenPlanParams struct {
	// KernelVersion of the managed kernel to run (a stable release such as
	// v1.19.34). Default: the version that runs now.
	KernelVersion string `json:"kernel_version,omitempty"`
	// NewSecret generates a new controller secret that the restarted
	// mihomo starts with. Without it the stored secret (if any) is used,
	// which also activates one controller.secure wrote but mihomo has not
	// loaded yet.
	NewSecret bool `json:"new_secret,omitempty"`
}

type HardenPlansParams struct {
	PlanID string `json:"plan_id,omitempty"` // empty: all, newest first
}

type HardenApplyParams struct {
	PlanID      string `json:"plan_id"`
	ConfirmHash string `json:"confirm_hash"`
	// SkipHealth skips the before/after health comparison (only /version
	// is verified).
	SkipHealth bool `json:"skip_health,omitempty"`
}

// HardenService is one side of the move.
type HardenService struct {
	Label      string `json:"label"`
	Plist      string `json:"plist"`
	Binary     string `json:"binary"`
	Version    string `json:"version,omitempty"`
	ConfigDir  string `json:"config_dir"`
	ConfigFile string `json:"config_file"`
	// Owner of the config dir ("root" or "uid 501").
	Owner string `json:"owner,omitempty"`
}

type HardenKernel struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"` // already in the managed kernel dir
	Asset     string `json:"asset,omitempty"`
	Size      int64  `json:"size,omitempty"` // download size when not installed
	Binary    string `json:"binary"`
}

// HardenFile is a file or dir copied into the new config dir.
type HardenFile struct {
	Path string `json:"path"` // relative to the config dir
	Size int64  `json:"size"`
	Dir  bool   `json:"dir,omitempty"`
}

// HardenStep is one step of the plan. Network marks the steps during
// which mihomo is not running.
type HardenStep struct {
	Phase   string `json:"phase"` // prepare, cut, verify, finish, rollback
	Text    string `json:"text"`
	Network bool   `json:"network_down,omitempty"`
}

type HardenDowntime struct {
	MinSeconds int    `json:"min_seconds"`
	MaxSeconds int    `json:"max_seconds"`
	Note       string `json:"note"`
}

// HardenPlan holds no secrets.
type HardenPlan struct {
	PlanID      string    `json:"plan_id"`
	CreatedAt   time.Time `json:"created_at"`
	State       string    `json:"state"`
	ConfirmHash string    `json:"confirm_hash"`

	From   HardenService `json:"from"`
	To     HardenService `json:"to"`
	Kernel HardenKernel  `json:"kernel"`

	Copy      []HardenFile `json:"copy"`
	CopyBytes int64        `json:"copy_bytes"`
	Skipped   []string     `json:"skipped,omitempty"`
	// ConfigChanges are the differences of the new config from the
	// running one (paths relocated, secret).
	ConfigChanges []string `json:"config_changes,omitempty"`
	// Secret: "keep" (no change), "activate" (the stored secret differs
	// from the running one and takes effect), "new" or "none".
	Secret   string         `json:"secret"`
	Plist    string         `json:"plist"`
	Steps    []HardenStep   `json:"steps"`
	Downtime HardenDowntime `json:"downtime"`
	Warnings []string       `json:"warnings,omitempty"`
	// DryRun: this helper does not run as root; apply only reports the
	// launchctl steps.
	DryRun bool `json:"dry_run"`
}

type HardenResult struct {
	PlanID     string      `json:"plan_id"`
	OK         bool        `json:"ok"`
	DryRun     bool        `json:"dry_run"`
	RolledBack bool        `json:"rolled_back"`
	Steps      []AdoptStep `json:"steps"`
	// DowntimeMS is measured from the bootout of the old service until the
	// new (or, after a rollback, the old) controller answered.
	DowntimeMS   int64        `json:"downtime_ms,omitempty"`
	HealthBefore []AdoptCheck `json:"health_before,omitempty"`
	HealthAfter  []AdoptCheck `json:"health_after,omitempty"`
	Worse        []string     `json:"worse,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
	Error        string       `json:"error,omitempty"`
}
