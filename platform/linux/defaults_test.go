package linux

import (
	"errors"
	"os/user"
	"testing"

	"github.com/l3ony2k/duet/core/store"
)

func groups(names ...string) func(string) (*user.Group, error) {
	return func(n string) (*user.Group, error) {
		for _, x := range names {
			if x == n {
				return &user.Group{Name: n, Gid: "27"}, nil
			}
		}
		return nil, errors.New("unknown group")
	}
}

func TestSocketGroup(t *testing.T) {
	for _, c := range []struct {
		have   []string
		want   string
		exists bool
	}{
		{[]string{"sudo", "wheel"}, "sudo", true},
		{[]string{"wheel"}, "wheel", true},
		{[]string{"duet"}, "duet", true},
		{nil, "duet", false},
	} {
		g, ok := SocketGroup(groups(c.have...))
		if g != c.want || ok != c.exists {
			t.Errorf("%v: got %s %v, want %s %v", c.have, g, ok, c.want, c.exists)
		}
	}
}

func TestLinuxLabels(t *testing.T) {
	st := &store.Settings{}
	st.Defaults("/var/lib/duet")
	if !LinuxLabels(st) {
		t.Fatal("no change")
	}
	if st.Mihomo.ServiceLabel != MihomoUnit || st.Tailscale.ServiceLabel != TailscaledUnit || st.HelperLabel != HelperUnit {
		t.Fatalf("%+v", st)
	}
	if LinuxLabels(st) {
		t.Fatal("changed twice")
	}
	st.Mihomo.ServiceLabel = "mihomo"
	if LinuxLabels(st) || st.Mihomo.ServiceLabel != "mihomo" {
		t.Fatal("user label replaced")
	}
}
