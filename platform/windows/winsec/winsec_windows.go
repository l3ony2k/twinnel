//go:build windows

package winsec

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy ties the blobs to Twinnel: another program running as
// LocalSystem cannot unseal them by accident (it is not a secret).
var entropy = []byte("twinnel-helper-secrets-v1")

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func takeBlob(out *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

// Protect seals b with CryptProtectData in the current user's scope (for
// the helper: LocalSystem), without UI.
func Protect(b []byte) ([]byte, error) {
	var out windows.DataBlob
	name, _ := windows.UTF16PtrFromString("Twinnel secrets")
	if err := windows.CryptProtectData(blob(b), name, blob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	return takeBlob(&out), nil
}

// Unprotect opens a blob made by Protect.
func Unprotect(b []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, blob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	return takeBlob(&out), nil
}

// SecureStateDir replaces the state dir's DACL with StateDirSDDL (and makes
// Administrators its owner), so files the helper writes there are not
// readable by other users. Needs to run elevated or as LocalSystem.
func SecureStateDir(dir string) error {
	sd, err := windows.SecurityDescriptorFromString(StateDirSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
		admins, nil, dacl, nil)
}
