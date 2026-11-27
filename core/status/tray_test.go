package status

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

func TestTrayIconFor(t *testing.T) {
	good := func() Snapshot {
		s := Snapshot{Loaded: true}
		s.Mihomo.Reachable = true
		s.Mihomo.Config.Mode = mihomo.ModeRule
		s.Mihomo.Config.Tun.Enable = true
		s.Tailscale.Reachable = true
		s.Tailscale.Install.Kind = tailscale.InstallTailscaled
		s.Tailscale.State.BackendState = "Running"
		s.Helper.State = HelperConnected
		Evaluate(&s)
		return s
	}
	cases := []struct {
		name string
		edit func(s *Snapshot)
		want TrayIcon
	}{
		{"ok", func(s *Snapshot) {}, TrayOK},
		{"loading", func(s *Snapshot) { *s = Snapshot{} }, TrayUnknown},
		{"mihomo down", func(s *Snapshot) { s.Mihomo = Mihomo{Err: "refused"} }, TrayError},
		{"mihomo down and tailscale down", func(s *Snapshot) {
			s.Mihomo = Mihomo{Err: "refused"}
			s.Tailscale.State.BackendState = "Stopped"
		}, TrayError},
		{"tailscale stopped", func(s *Snapshot) { s.Tailscale.State.BackendState = "Stopped" }, TrayTSDown},
		{"tailscale unreachable", func(s *Snapshot) { s.Tailscale.Reachable, s.Tailscale.Err = false, "no socket" }, TrayTSDown},
		{"tailscale not installed", func(s *Snapshot) {
			s.Tailscale = Tailscale{Err: "no socket", Install: tailscale.Install{Kind: tailscale.InstallUnknown}}
		}, TrayWarn},
		{"tun off", func(s *Snapshot) { s.Mihomo.Config.Tun.Enable = false }, TrayWarn},
		{"global mode", func(s *Snapshot) { s.Mihomo.Config.Mode = mihomo.ModeGlobal }, TrayMode},
		{"direct mode, capitalized", func(s *Snapshot) { s.Mihomo.Config.Mode = "Direct" }, TrayMode},
		{"mode beats helper", func(s *Snapshot) {
			s.Mihomo.Config.Mode = mihomo.ModeDirect
			s.Helper.State = HelperNotInstalled
		}, TrayMode},
		{"no helper", func(s *Snapshot) { s.Helper.State = HelperNotInstalled }, TrayNoHelper},
		{"helper not running", func(s *Snapshot) { s.Helper.State = HelperNotRunning }, TrayNoHelper},
		{"helper still checking", func(s *Snapshot) { s.Helper.State = HelperUnknown }, TrayOK},
	}
	for _, c := range cases {
		s := good()
		c.edit(&s)
		Evaluate(&s)
		if got := TrayIconFor(s); got != c.want {
			t.Errorf("%s: %s, want %s (level %v, reasons %+v)", c.name, got, c.want, s.Level, s.Reasons)
		}
	}
}

func TestTrayToolTip(t *testing.T) {
	s := Snapshot{Level: LevelError, Reasons: []Reason{{Level: LevelWarn, Text: "w"}, {Level: LevelError, Text: "e"}}}
	if got := TrayToolTip(s); !strings.HasSuffix(got, " · e") {
		t.Errorf("tooltip %q", got)
	}
	if got := TrayToolTip(Snapshot{Level: LevelOK}); strings.Count(got, " · ") != 1 {
		t.Errorf("ok tooltip %q", got)
	}
}
