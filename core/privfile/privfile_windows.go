//go:build windows

package privfile

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SDDL is the security descriptor a private file (dir: a private dir,
// whose ACEs its children inherit) gets: owned by the current user, a
// protected DACL (nothing inherited from the folder) granting full control
// to that user, LocalSystem and Administrators.
func SDDL(dir bool) (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return sddlFor(u.User.Sid.String(), dir), nil
}

func attrs(dir bool) (*windows.SecurityAttributes, error) {
	s, err := SDDL(dir)
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString(s)
	if err != nil {
		return nil, fmt.Errorf("security descriptor: %w", err)
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func create(path string) (*os.File, error) {
	sa, err := attrs(false)
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, sa,
		windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func mkdir(dir string) error {
	sa, err := attrs(true)
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p, sa); err != nil {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// SetSecurity replaces path's owner and DACL with those in sddl ("O:…D:P…"),
// protected from inheritance: for a file or dir that already exists (the
// helper's state dir, winsec.SecureStateDir). Setting an owner other than
// the caller needs an elevated token or LocalSystem.
func SetSecurity(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	owner, _, err := sd.Owner()
	if err == nil && owner != nil {
		info |= windows.OWNER_SECURITY_INFORMATION
	} else {
		owner = nil
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info, owner, nil, dacl, nil)
}

// Canonical renders sddl the way Describe does (well-known SIDs as their
// aliases, e.g. "SY", "LA"), so the two compare as strings.
func Canonical(sddl string) (string, error) {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}

// Describe is path's owner and DACL as SDDL (for tests and diagnostics).
// Windows may add the auto-inherited flags ("D:PAI"), which do not change
// who has access; Describe drops them.
func Describe(path string) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return strings.Replace(sd.String(), "D:PAI(", "D:P(", 1), nil
}
