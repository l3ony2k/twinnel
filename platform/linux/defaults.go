package linux

import (
	"os/user"

	"github.com/l3ony2k/twinnel/core/store"
)

// Install locations and unit names of Twinnel on Linux. The macOS values live
// in core/store and cmd/cli; these replace them where the code runs on
// Linux.
const (
	// StateDir holds settings, layers, history, kernels and secrets.json.
	StateDir = "/var/lib/twinnel"
	// HelperBin is the installed twinnel-helper binary.
	HelperBin = "/usr/local/libexec/twinnel-helper"
	// HelperUnit, MihomoUnit and TailscaledUnit are systemd unit names
	// (without ".service"). tailscaled's is the one distro packages and
	// Tailscale's own repository install.
	HelperUnit     = "twinnel-helper"
	MihomoUnit     = "twinnel-mihomo"
	TailscaledUnit = "tailscaled"
	// TwinnelGroup is created when the distro has neither sudo nor wheel.
	TwinnelGroup = "twinnel"
)

// SocketGroupCandidates are tried in order by SocketGroup: the group whose
// members may use sudo on Debian/Ubuntu ("sudo") and on Fedora/Arch/openSUSE
// ("wheel"), the closest match to macOS's "admin".
var SocketGroupCandidates = []string{"sudo", "wheel"}

// SocketGroup picks the group allowed to use the helper's socket: the first
// of SocketGroupCandidates that exists, else TwinnelGroup (which then has to
// be created; exists reports whether it already is). lookup is
// user.LookupGroup when nil.
func SocketGroup(lookup func(string) (*user.Group, error)) (group string, exists bool) {
	if lookup == nil {
		lookup = user.LookupGroup
	}
	for _, g := range SocketGroupCandidates {
		if _, err := lookup(g); err == nil {
			return g, true
		}
	}
	_, err := lookup(TwinnelGroup)
	return TwinnelGroup, err == nil
}

// LinuxLabels replaces the macOS default service labels in st with the
// Linux unit names. Labels the user set are kept. It reports whether
// anything changed.
func LinuxLabels(st *store.Settings) bool {
	changed := false
	for _, f := range []struct {
		p        *string
		mac, lin string
	}{
		{&st.Mihomo.ServiceLabel, store.DefaultMihomoLabel, MihomoUnit},
		{&st.Tailscale.ServiceLabel, store.DefaultTailscaleLabel, TailscaledUnit},
		{&st.HelperLabel, store.DefaultHelperLabel, HelperUnit},
	} {
		if *f.p == "" || *f.p == f.mac {
			*f.p = f.lin
			changed = true
		}
	}
	return changed
}
