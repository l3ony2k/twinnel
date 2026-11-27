//go:build windows

package privfile

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func userSID(t *testing.T) string {
	t.Helper()
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return u.User.Sid.String()
}

// assertPrivate checks the SDDL Windows reports: owned by this user, a
// protected DACL, no inherited ACE, nothing but the user, SYSTEM and
// Administrators.
func assertPrivate(t *testing.T, path string, dir bool) {
	t.Helper()
	got, err := Describe(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Canonical(sddlFor(userSID(t), dir))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("%s:\n got  %s\n want %s", path, got, want)
	}
}

func assertInherited(t *testing.T, path string) {
	t.Helper()
	got, err := Describe(path)
	if err != nil {
		t.Fatal(err)
	}
	aces := regexp.MustCompile(`\([^)]*\)`)
	want := len(aces.FindAllString(sddlFor(userSID(t), true), -1))
	have := aces.FindAllString(got, -1)
	if len(have) != want {
		t.Fatalf("%s: %s, want only the private dir's %d ACEs", path, got, want)
	}
	for _, a := range have {
		if !strings.HasPrefix(a, "(A;ID;FA;;;") {
			t.Errorf("%s: ACE %s is not the private dir's, inherited", path, a)
		}
	}
}
