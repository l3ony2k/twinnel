// Package winsec holds the helper's Windows security pieces: DPAPI sealing
// for the secret store (the Keychain's counterpart) and the state dir's
// access control list.
package winsec

import (
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/store"
)

// SecretsFile is the sealed secret store's name in the state dir.
const SecretsFile = "secrets.dpapi"

// StateDirSDDL is the state dir's DACL: LocalSystem and Administrators
// only, applied to the dir and inherited by everything in it, nothing
// inherited from %ProgramData% (whose default lets Users read). This
// replaces the Unix rule "root-owned, 0700".
const StateDirSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// Secrets is the helper's secret store on Windows: the same key/value file
// as store.FileSecrets, sealed with DPAPI in the helper's (LocalSystem's)
// scope.
func Secrets(stateDir string) *store.FileSecrets {
	return &store.FileSecrets{Path: filepath.Join(stateDir, SecretsFile), Seal: Protect, Unseal: Unprotect}
}
