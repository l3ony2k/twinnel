package linux

import (
	"errors"
	"os/user"
	"reflect"
	"testing"
)

func fakeAccounts() Accounts {
	users := map[string]*user.User{
		"1000": {Uid: "1000", Gid: "1000", Username: "alice"},
		"1001": {Uid: "1001", Gid: "998", Username: "bob"}, // primary group wheel
	}
	groups := map[string]*user.Group{"wheel": {Gid: "998", Name: "wheel"}, "sudo": {Gid: "27", Name: "sudo"}}
	member := map[string][]string{"1000": {"1000", "27"}, "1001": {"998"}}
	return Accounts{
		LookupID: func(uid string) (*user.User, error) {
			if u, ok := users[uid]; ok {
				return u, nil
			}
			return nil, user.UnknownUserIdError(0)
		},
		LookupGroup: func(name string) (*user.Group, error) {
			if g, ok := groups[name]; ok {
				return g, nil
			}
			return nil, user.UnknownGroupError(name)
		},
		GroupIds: func(u *user.User) ([]string, error) { return member[u.Uid], nil },
	}
}

func TestInvokingUID(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if uid, ok := InvokingUID(get, 1000); uid != "1000" || !ok {
		t.Errorf("plain user: %q %v", uid, ok)
	}
	if _, ok := InvokingUID(get, 0); ok {
		t.Error("root alone has nobody to add")
	}
	env["SUDO_UID"] = "1001"
	if uid, ok := InvokingUID(get, 0); uid != "1001" || !ok {
		t.Errorf("sudo: %q %v", uid, ok)
	}
	env["PKEXEC_UID"] = "1000" // pkexec wins
	if uid, ok := InvokingUID(get, 0); uid != "1000" || !ok {
		t.Errorf("pkexec: %q %v", uid, ok)
	}
	env["PKEXEC_UID"] = "0"
	if _, ok := InvokingUID(get, 0); ok {
		t.Error("PKEXEC_UID=0 is root")
	}
	env["PKEXEC_UID"] = "x; rm -rf /"
	if _, ok := InvokingUID(get, 0); ok {
		t.Error("a non-numeric uid was accepted")
	}
}

func TestSocketAccessSteps(t *testing.T) {
	acc := fakeAccounts()
	for _, c := range []struct {
		uid, group string
		want       []string // usermod argv, nil for no step
	}{
		{"1000", "sudo", nil},  // supplementary member
		{"1001", "wheel", nil}, // primary group
		{"1000", "wheel", []string{Usermod, "-a", "-G", "wheel", "alice"}},
		{"1001", "twinnel", []string{Usermod, "-a", "-G", "twinnel", "bob"}}, // group created by the same plan
	} {
		steps, name, err := SocketAccessSteps(acc, c.uid, true, c.group)
		if err != nil {
			t.Fatal(err)
		}
		if c.want == nil {
			if len(steps) != 0 || name != "" {
				t.Errorf("%s in %s: %+v", c.uid, c.group, steps)
			}
			continue
		}
		if len(steps) != 1 || !reflect.DeepEqual(steps[0].Argv, c.want) {
			t.Errorf("%s in %s: %+v", c.uid, c.group, steps)
		}
	}
	if steps, _, _ := SocketAccessSteps(acc, "", false, "wheel"); steps != nil {
		t.Error("no invoking user, but a step")
	}
	if _, _, err := SocketAccessSteps(acc, "4242", true, "wheel"); err == nil || errors.Is(err, nil) {
		t.Error("an unknown uid gave no error")
	}
}

func TestSessionInGroup(t *testing.T) {
	if !SessionInGroup(998, 998, nil) || !SessionInGroup(998, 1000, []int{4, 998}) || SessionInGroup(998, 1000, []int{4, 27}) {
		t.Error("SessionInGroup")
	}
}

func TestSocketGroupGap(t *testing.T) {
	acc := fakeAccounts()
	byID := func(gid string) (*user.Group, error) {
		switch gid {
		case "998":
			return &user.Group{Gid: "998", Name: "wheel"}, nil
		case "27":
			return &user.Group{Gid: "27", Name: "sudo"}, nil
		}
		return nil, user.UnknownGroupIdError(gid)
	}
	// alice (1000) is listed in sudo (27), not in wheel (998).
	if g := SocketGroupGap(acc, byID, 998, 0o660, 1000, []int{1000, 27}, "1000"); g != (GroupGap{Group: "wheel"}) {
		t.Errorf("not in wheel: %+v", g)
	}
	// Listed in sudo, but this session started before: log in again.
	if g := SocketGroupGap(acc, byID, 27, 0o660, 1000, []int{1000}, "1000"); g != (GroupGap{Group: "sudo", Listed: true}) {
		t.Errorf("relogin: %+v", g)
	}
	if g := SocketGroupGap(acc, byID, 27, 0o660, 1000, []int{1000, 27}, "1000"); g.Group != "" {
		t.Errorf("session in the group: %+v", g)
	}
	if g := SocketGroupGap(acc, byID, 998, 0o666, 1000, nil, "1000"); g.Group != "" {
		t.Errorf("world-writable socket: %+v", g)
	}
	if g := SocketGroupGap(acc, byID, 4242, 0o660, 1000, nil, "1000"); g != (GroupGap{Group: "4242"}) {
		t.Errorf("unknown gid: %+v", g)
	}
}
