package rpc

import "github.com/l3ony2k/twinnel/core/autostart"

// Competing clients' autostarts (core/autostart): what will start again
// by itself after a takeover stopped it.
const (
	// MethodAutostartList lists them (read only).
	MethodAutostartList = "autostart.list"
	// MethodAutostartChange disables (or, as an undo, enables) entries
	// the helper can change; with Preview it only returns the steps.
	MethodAutostartChange = "autostart.change"
)

type AutostartList struct {
	// Supported: this platform has a reader.
	Supported bool              `json:"supported"`
	Entries   []autostart.Entry `json:"entries"`
	// Error is a partial read (a place that could not be read); the
	// entries found elsewhere are still listed.
	Error string `json:"error,omitempty"`
}

type AutostartChangeParams struct {
	IDs     []string `json:"ids"`
	Op      string   `json:"op"` // "disable" or "enable"
	Preview bool     `json:"preview,omitempty"`
}

type AutostartChangeResult struct {
	Steps   []string          `json:"steps"`
	DryRun  bool              `json:"dry_run,omitempty"` // preview, or a helper that is not root
	Changed []autostart.Entry `json:"changed,omitempty"`
	// Undo is the call that puts the change back.
	Undo *AutostartChangeParams `json:"undo,omitempty"`
}
