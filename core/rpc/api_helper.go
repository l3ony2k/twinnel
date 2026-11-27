package rpc

// The helper's own lifecycle: what `twinnel helper uninstall` and the app's
// 后台助手 section need to know, and a restart without a password.
const (
	// MethodHelperInfo is read-only: the helper's version, where its log
	// and data are, and what uninstalling it must take care of (an
	// adopted or hardened mihomo service, keychain items by name).
	MethodHelperInfo = "helper.info"
	// MethodHelperRestart makes the helper exit after answering; launchd
	// (KeepAlive) or systemd (Restart=always) starts it again. mihomo and
	// tailscaled are not touched. A development helper just stops.
	MethodHelperRestart = "helper.restart"
)

// HelperInfo is the result of helper.info. It never holds a secret: only
// the names of the keychain items.
type HelperInfo struct {
	Version  string `json:"version"`
	StateDir string `json:"state_dir"`
	LogFile  string `json:"log_file,omitempty"`
	Label    string `json:"label,omitempty"` // the helper's own service label

	// Mihomo is the mihomo service the helper manages.
	Mihomo HelperMihomoService `json:"mihomo"`
	// Hardened is set after `twinnel helper harden apply`: mihomo runs from
	// Twinnel's LaunchDaemon, and Old is the service it replaced.
	Hardened *HelperHardened `json:"hardened,omitempty"`
	// SecretKeys are the keys in the secret store (keychain item service
	// names are "twinnel." + key).
	SecretKeys []string `json:"secret_keys,omitempty"`
	// Subscriptions and Layers count the stored items (what --purge
	// deletes).
	Subscriptions int `json:"subscriptions"`
	Layers        int `json:"layers"`
	History       int `json:"history"`
}

type HelperMihomoService struct {
	Label     string `json:"label"`
	Adopted   bool   `json:"adopted,omitempty"`   // a plist Twinnel found, not its own
	Installed bool   `json:"installed,omitempty"` // its definition file exists
	// Controller is mihomo's external-controller address.
	Controller string `json:"controller,omitempty"`
}

type HelperHardened struct {
	PlanID   string `json:"plan_id"`
	OldLabel string `json:"old_label"`
	OldPlist string `json:"old_plist"`
}

type HelperRestartResult struct {
	PID int `json:"pid"` // the process that is about to exit
}
