package rpc

// What duet put on this Mac and how to leave it: helper.inventory lists
// the helper's own pieces (read-only); takeover.undo puts the setup back
// as it was before takeover.adopt.
const (
	// MethodHelperInventory is read-only: what the helper installed and
	// keeps (its binary and service, the state dir by part, keychain item
	// names, a duet mihomo service). Never a secret value.
	MethodHelperInventory = "helper.inventory"
	// MethodTakeoverUndo writes the pre-takeover config back byte for
	// byte, reloads mihomo and removes what adopt imported. With DryRun it
	// only lists the steps.
	MethodTakeoverUndo = "takeover.undo"
)

// Inventory item kinds (helper.inventory). The app adds its own (the app
// bundle, its settings file, install backups, a user LaunchAgent).
const (
	InvHelperBinary    = "helper-binary"
	InvHelperService   = "helper-service"
	InvHelperSocket    = "helper-socket"
	InvStateDir        = "state-dir" // the whole dir; Size is its total
	InvSubscriptions   = "subscriptions"
	InvLayers          = "layers"
	InvHistory         = "history"
	InvTakeoverBackups = "takeover-backups"
	InvKernels         = "kernels"
	InvLogs            = "logs"
	InvSecrets         = "secrets"        // Names: the keychain keys
	InvMihomoService   = "duet-mihomo"    // duet's own mihomo LaunchDaemon (installed or hardened)
	InvAdoptedService  = "adopted-mihomo" // the person's mihomo service; kept, listed for completeness
)

// InventoryItem is one thing duet installed or keeps.
type InventoryItem struct {
	Kind  string   `json:"kind"`
	Path  string   `json:"path,omitempty"`
	Size  int64    `json:"size,omitempty"`  // bytes (dirs: the total)
	Count int      `json:"count,omitempty"` // items it holds, when that reads better than a size
	Names []string `json:"names,omitempty"` // secrets: the keys
	Label string   `json:"label,omitempty"` // a service label
}

// Inventory is the result of helper.inventory.
type Inventory struct {
	Items []InventoryItem `json:"items"`
	// Adopted is the takeover plan the running setup came from ("" when
	// duet never took over); takeover.undo works only then.
	Adopted string `json:"adopted,omitempty"`
	// Hardened: mihomo was moved to duet's own service; undo refuses
	// (uninstalling with the move back does it).
	Hardened bool `json:"hardened,omitempty"`
}

// TakeoverUndoParams: DryRun lists the steps without running them.
type TakeoverUndoParams struct {
	DryRun bool `json:"dry_run,omitempty"`
}

// Undo step codes (the app words them; Detail is the English text).
const (
	UndoRestoreConfig = "restore-config" // write the pre-takeover config back (Path)
	UndoReload        = "reload"         // mihomo reloads it; it keeps running
	UndoRestoreSecret = "restore-secret" // the stored controller secret goes back to the config's
	UndoRemoveSub     = "remove-subscription"
	UndoRemoveLayers  = "remove-layers"  // Count
	UndoDisableLayers = "disable-layers" // layers added after takeover: kept, turned off (Count)
	UndoKeepService   = "keep-service"   // the service definition was never changed (Path: its label)
	UndoKeepData      = "keep-data"      // history and plan backups stay
)

// Undo refusal codes (rpc.Error message starts with "code: ").
const (
	UndoNotAdopted = "not-adopted"
	UndoHardened   = "hardened"
	UndoNoBackup   = "no-backup"
)

// TakeoverUndoStep is one step of takeover.undo.
type TakeoverUndoStep struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Count  int    `json:"count,omitempty"`
	Status string `json:"status,omitempty"` // planned, ok, failed
	Detail string `json:"detail,omitempty"`
}

// TakeoverUndoResult reports takeover.undo.
type TakeoverUndoResult struct {
	PlanID string             `json:"plan_id"`
	DryRun bool               `json:"dry_run,omitempty"`
	OK     bool               `json:"ok"`
	Steps  []TakeoverUndoStep `json:"steps"`
	Error  string             `json:"error,omitempty"`
}

// PlanUndone: the plan was adopted and takeover.undo put the old config
// back.
const PlanUndone = "undone"

// Takeover effect items and actions: what adopt does to each existing
// piece (TakeoverPlan.Effects), so a client can show 现在 | 接管后.
const (
	EffMihomoService = "mihomo-service"
	EffMihomoBinary  = "mihomo-binary"
	EffConfig        = "config"
	EffSubscription  = "subscription"
	EffSecret        = "secret"
	EffTailscale     = "tailscale"

	ActKeep      = "keep"      // stays as it is
	ActRewrite   = "rewrite"   // duet writes it (after a backup)
	ActStore     = "store"     // copied into duet's secret store
	ActUnchanged = "unchanged" // not touched at all
	ActNone      = "none"      // there is none
	ActSkip      = "skip"      // found, but not imported (Detail says why)

	WarnReplaceable = "replaceable"       // binary: a non-root user can replace it
	WarnEditsLost   = "edits-overwritten" // config: direct edits are lost on the next apply
	WarnManyProfile = "many-profiles"     // subscription: more than one found
	WarnNoSecret    = "no-secret"         // secret: the controller has none
)

// TakeoverEffect is one row of the plan's 接管后 table.
type TakeoverEffect struct {
	Item    string `json:"item"`
	Action  string `json:"action"`
	Subject string `json:"subject,omitempty"` // label or path
	Warn    string `json:"warn,omitempty"`
	Count   int    `json:"count,omitempty"`
}
