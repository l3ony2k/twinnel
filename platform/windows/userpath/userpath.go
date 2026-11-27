// Package userpath puts a folder on the Windows user's PATH and takes it
// off again: HKCU\Environment\Path, kept as REG_EXPAND_SZ when it holds
// %VARIABLES%, then a WM_SETTINGCHANGE "Environment" broadcast so that
// Explorer, and terminals started from it afterwards, see the change.
// Terminals already open keep their old PATH; Windows has no way to
// change another process's environment.
//
// The list logic here has no build tag (tested on every OS); the registry
// and the broadcast are in userpath_windows.go.
package userpath

import "strings"

// Split returns the entries of a PATH value, without empty ones.
func Split(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// same reports whether two folders are the same entry: case-insensitive,
// trailing backslashes ignored, after expand (nil: none).
func same(a, b string, expand func(string) string) bool {
	if expand != nil {
		a, b = expand(a), expand(b)
	}
	return strings.EqualFold(strings.TrimRight(a, `\/`), strings.TrimRight(b, `\/`))
}

// Contains reports whether dir is an entry of the PATH value v.
func Contains(v, dir string, expand func(string) string) bool {
	for _, p := range Split(v) {
		if same(p, dir, expand) {
			return true
		}
	}
	return false
}

// Add returns v with dir appended, and whether it changed (not when dir is
// already there). The other entries stay as they are, unexpanded.
func Add(v, dir string, expand func(string) string) (string, bool) {
	if Contains(v, dir, expand) {
		return v, false
	}
	return strings.Join(append(Split(v), dir), ";"), true
}

// Remove returns v without dir, and whether it changed.
func Remove(v, dir string, expand func(string) string) (string, bool) {
	parts := Split(v)
	keep := parts[:0:0]
	for _, p := range parts {
		if !same(p, dir, expand) {
			keep = append(keep, p)
		}
	}
	if len(keep) == len(parts) {
		return v, false
	}
	return strings.Join(keep, ";"), true
}
