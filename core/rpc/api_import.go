package rpc

import (
	"fmt"
	"log/slog"
)

// Moving a setup to another machine.
//
// config.export returns the export zip: manifest.json, config.yaml (the
// running config without the controller secret), overrides/<id>.yaml and,
// with subscriptions, their URLs (secrets) and last downloads. The person
// asked for this file; the helper only hands it to authorized peers, who
// can read the same secrets through controller.credentials and the store.
//
// config.import stores what a file brings: subscriptions (URLs into the
// secret store) and the person's override layers with their names, order
// and on/off state. Twinnel's generated layers (Tailscale coexist, leak
// guard, takeover's base layers) are never imported: they are rebuilt on
// the new machine. Nothing is applied: compose.apply afterwards.
const (
	MethodConfigExport = "config.export"
	MethodConfigImport = "config.import"
)

// ConfigExportParams are config.export's params.
type ConfigExportParams struct {
	// NoSubscriptions leaves subscriptions (and their URLs) out.
	NoSubscriptions bool `json:"no_subscriptions,omitempty"`
}

// ConfigExport is config.export's result. Zip holds secrets: never log it.
type ConfigExport struct {
	Zip           []byte `json:"zip"` // base64 in JSON
	Layers        int    `json:"layers"`
	Subscriptions int    `json:"subscriptions"`
	// NoContent: the downloads did not fit in one RPC message and were
	// left out; the new machine downloads them again from the URLs.
	NoContent bool `json:"no_content,omitempty"`
}

func (c ConfigExport) String() string {
	return fmt.Sprintf("{zip:%d bytes layers:%d subscriptions:%d}", len(c.Zip), c.Layers, c.Subscriptions)
}

// GoString keeps %#v from printing the zip.
func (c ConfigExport) GoString() string { return c.String() }

func (c ConfigExport) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("zip_bytes", len(c.Zip)), slog.Int("layers", c.Layers), slog.Int("subscriptions", c.Subscriptions))
}

// CodeConflict: config.import found conflicts the params do not resolve.
const CodeConflict = -32006

// Conflict resolutions for config.import.
const (
	ImportAsk     = "ask"     // default: a conflict stops the import (dry_run shows them)
	ImportReplace = "replace" // overwrite the local item (it keeps its place in the order)
	ImportSkip    = "skip"    // keep the local item
	ImportRename  = "rename"  // import under a free id (<id>-2, …)
)

// ConfigImportParams are config.import's params.
type ConfigImportParams struct {
	// Data is the file: a Twinnel export zip (with or without a manifest),
	// subscription URLs one per line, a Clash Verge Rev / Clash Party
	// override YAML, or a whole config (becomes a subscription). At most
	// 11 MiB. base64 in JSON.
	Data []byte `json:"data"`
	// Name is the file's name: names a single override; optional.
	Name   string `json:"name,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
	// OnConflict resolves every conflict without its own entry in Resolve:
	// ask (default), replace, skip or rename.
	OnConflict string `json:"on_conflict,omitempty"`
	// Resolve resolves single conflicts: "layer:<id>" or
	// "subscription:<id>" (the id in the file) → replace, skip or rename.
	Resolve map[string]string `json:"resolve,omitempty"`
}

func (p ConfigImportParams) String() string {
	return fmt.Sprintf("{data:%d bytes name:%q dry_run:%v on_conflict:%s resolve:%v}", len(p.Data), p.Name, p.DryRun, p.OnConflict, p.Resolve)
}

// GoString keeps %#v from printing the file.
func (p ConfigImportParams) GoString() string { return p.String() }

// Import item kinds and actions.
const (
	ImportKindLayer        = "layer"
	ImportKindSubscription = "subscription"

	ImportAdd       = "add"       // new here
	ImportUnchanged = "unchanged" // the same item is already here
	ImportConflict  = "conflict"  // the id is taken by a different item; needs a resolution
	ImportGenerated = "generated" // a Twinnel-made layer: rebuilt here, not imported
	ImportInvalid   = "invalid"   // the content does not parse; not imported
	// and ImportReplace, ImportSkip, ImportRename once resolved
)

// Import warnings (codes; clients word them).
const (
	ImportWarnSecrets         = "secrets"          // the file holds subscription URLs or node credentials
	ImportWarnNoSubscriptions = "no_subscriptions" // nothing to compose from yet: add a subscription
	ImportWarnPlainHTTP       = "plain_http"       // a subscription URL is http://
	ImportWarnNoContent       = "no_content"       // a subscription has a URL only: refresh downloads it
)

// ImportItem is one layer or subscription in the file and what happens to
// it. URLs are never in it.
type ImportItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"` // in the file
	Name   string `json:"name"`
	Action string `json:"action"`
	// NewID is the id stored here (rename: the free id; otherwise ID).
	NewID string `json:"new_id,omitempty"`
	// Reason: why invalid (the parse error) or generated.
	Reason string `json:"reason,omitempty"`
	// Existing is the local item's name on a conflict.
	Existing string `json:"existing,omitempty"`

	// Layers.
	Stage   string `json:"stage,omitempty"`
	Format  string `json:"format,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Size    int    `json:"size,omitempty"`

	// Subscriptions.
	Active          bool `json:"active,omitempty"`
	HasURL          bool `json:"has_url,omitempty"`
	HasContent      bool `json:"has_content,omitempty"`
	IntervalMinutes int  `json:"interval_minutes,omitempty"`
}

// ConfigImportResult is config.import's result: the plan (dry_run, or a
// refusal with unresolved conflicts, as the error's data) or what was
// stored.
type ConfigImportResult struct {
	DryRun   bool         `json:"dry_run"`
	Source   string       `json:"source"`            // twinnel, twinnel-legacy, url-list, override, config
	Version  int          `json:"version,omitempty"` // the manifest's; 0 without one
	App      string       `json:"app,omitempty"`     // the Twinnel version that exported it
	Items    []ImportItem `json:"items"`
	Warnings []string     `json:"warnings,omitempty"`
	// Conflicts counts items still in conflict after the params'
	// resolutions. Without dry_run, any makes the call fail with
	// CodeConflict and this result as the error's data.
	Conflicts int `json:"conflicts"`
	// Stored counts the layers and subscriptions written.
	Stored int `json:"stored"`
	// Activated is the subscription made active (none was active here).
	Activated string `json:"activated,omitempty"`
}
