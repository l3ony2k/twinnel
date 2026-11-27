//go:build windows

package userpath

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const envKey = `Environment`

// read returns the user's PATH as stored (unexpanded) and its type.
func read() (string, uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", registry.EXPAND_SZ, nil
	}
	return v, typ, err
}

func write(v string, typ uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if typ == registry.EXPAND_SZ || strings.Contains(v, "%") {
		err = k.SetExpandStringValue("Path", v)
	} else {
		err = k.SetStringValue("Path", v)
	}
	if err != nil {
		return err
	}
	Broadcast()
	return nil
}

func expand(s string) string {
	if x, err := registry.ExpandString(s); err == nil {
		return x
	}
	return s
}

// Has reports whether dir is on the user's PATH.
func Has(dir string) (bool, error) {
	v, _, err := read()
	if err != nil {
		return false, err
	}
	return Contains(v, dir, expand), nil
}

// AddDir appends dir to the user's PATH; changed is false when it was
// already there (the broadcast is sent either way).
func AddDir(dir string) (changed bool, err error) {
	v, typ, err := read()
	if err != nil {
		return false, err
	}
	nv, changed := Add(v, dir, expand)
	if !changed {
		Broadcast()
		return false, nil
	}
	return true, write(nv, typ)
}

// RemoveDir takes dir off the user's PATH.
func RemoveDir(dir string) (changed bool, err error) {
	v, typ, err := read()
	if err != nil {
		return false, err
	}
	nv, changed := Remove(v, dir, expand)
	if !changed {
		return false, nil
	}
	return true, write(nv, typ)
}

var procSendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// Broadcast tells every top-level window that the environment changed
// (WM_SETTINGCHANGE, lParam "Environment"), as the System Properties
// dialog does. Explorer reloads it: programs started from then on get the
// new PATH. Windows that do not answer within 5 s are skipped.
func Broadcast() {
	const (
		hwndBroadcast       = 0xffff
		wmSettingChange     = 0x001A
		smtoAbortIfHung     = 0x0002
		timeoutMilliseconds = 5000
	)
	env, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, timeoutMilliseconds, uintptr(unsafe.Pointer(&result)))
}
