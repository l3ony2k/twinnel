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

	MethodServiceStatus = "service.status"

	MethodEventsWait = "events.wait"
)

// Event types pushed through events.wait.
const (
	EventComposeApplied  = "compose.applied"
	EventComposeFailed   = "compose.failed"
	EventStateChanged    = "state.changed" // layers, subscriptions or settings edited
	EventSubUpdated      = "subscription.updated"
	EventSubFailed       = "subscription.failed"
	EventTailnetChanged  = "tailnet.changed"
	EventKernelChanged   = "kernel.changed"
	EventCheckChanged    = "check.changed" // health check result changed (published by the health runner)
	EventServiceChanged  = "service.changed"
	EventHelperStarted   = "helper.started"
	EventHelperStopping  = "helper.stopping"
	EventEventsTruncated = "events.truncated" // the client asked for events older than the buffer
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
	Coexist  bool      `json:"coexist"`
	Peers    []string  `json:"peers,omitempty"`
	Suffix   string    `json:"suffix,omitempty"`
	LastPoll time.Time `json:"last_poll,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// ---- compose ----

type ApplyParams struct {
	Reason string `json:"reason,omitempty"`
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
}

type ApplyResult struct {
	ComposeResult
	Reason     string    `json:"reason,omitempty"`
	Written    bool      `json:"written"`
	Reloaded   bool      `json:"reloaded"`
	RolledBack bool      `json:"rolled_back,omitempty"` // the new config failed after writing and the old one was restored
	HistoryID  string    `json:"history_id,omitempty"`
	Error      string    `json:"error,omitempty"`
	Time       time.Time `json:"time"`
	Duration   string    `json:"duration"`
}

// ---- history ----

type HistoryEntry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Hash   string    `json:"hash"`
	Size   int       `json:"size"`
	Kind   string    `json:"kind"` // "applied", "backup" (file found on disk), "rollback"
	Reason string    `json:"reason,omitempty"`
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
}

type SettingsUpdate struct {
	LeakGuardGroups   *[]string `json:"leak_guard_groups,omitempty"`
	TailscaleCoexist  *bool     `json:"tailscale_coexist,omitempty"`
	TailscaleResolver *string   `json:"tailscale_resolver,omitempty"`
	HistoryKeep       *int      `json:"history_keep,omitempty"`
	KernelChannel     *string   `json:"kernel_channel,omitempty"`
	AutoApply         *bool     `json:"auto_apply,omitempty"`
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
