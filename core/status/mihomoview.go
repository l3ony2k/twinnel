package status

import (
	"strconv"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/i18n"
)

// MihomoPhase is the state of Twinnel's mihomo, as every surface shows it
// (the 总览 card, the flyout, the sidebar's mark, the menu bar icon, the
// CLI's status line). It is derived in one place, MihomoView, so that no
// surface reads Mihomo.Reachable and shows another program's mihomo as
// Twinnel's (Windows QA round 2, E).
type MihomoPhase int

const (
	// MihomoDown: the controller does not answer (or nothing has
	// answered yet).
	MihomoDown MihomoPhase = iota
	// MihomoRunning: Twinnel's mihomo answers; Mihomo's values are its own.
	MihomoRunning
	// MihomoAuth: the controller answers but refuses the secret.
	MihomoAuth
	// MihomoForeign: another program's mihomo holds the controller
	// address (the helper's preflight, status.get mihomo.owner). What
	// the controller says is that program's: its version, ports, TUN and
	// traffic are not Twinnel's, and nothing may act on it.
	MihomoForeign
)

// String is the phase's machine name (JSON, logs, tests).
func (p MihomoPhase) String() string {
	switch p {
	case MihomoRunning:
		return "running"
	case MihomoAuth:
		return "unauthorized"
	case MihomoForeign:
		return "foreign"
	}
	return "down"
}

// MarshalText encodes the phase by its machine name.
func (p MihomoPhase) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// MihomoView is the one view of Twinnel's mihomo that every surface
// renders.
type MihomoView struct {
	Phase MihomoPhase
	// Owner is the program holding the controller (MihomoForeign only).
	Owner *foreign.Owner
	// Err is what the controller said when it failed (down, auth).
	Err string
}

// MihomoView derives the view of Twinnel's mihomo from s. A foreign owner
// wins over everything the controller says: it answers, but not for
// Twinnel.
func (s Snapshot) MihomoView() MihomoView {
	m := s.Mihomo
	switch {
	case s.ForeignController() != nil:
		return MihomoView{Phase: MihomoForeign, Owner: s.ForeignController()}
	case m.Unauthorized:
		return MihomoView{Phase: MihomoAuth, Err: m.Err}
	case m.Reachable:
		return MihomoView{Phase: MihomoRunning}
	}
	return MihomoView{Phase: MihomoDown, Err: m.Err}
}

// MihomoLive reports whether the controller answers for Twinnel's own
// mihomo: only then are Mihomo's values (version, ports, TUN, groups,
// traffic, connections) Twinnel's to show or act on.
func (s Snapshot) MihomoLive() bool { return s.MihomoView().Live() }

// Live reports whether the view is Twinnel's mihomo running.
func (v MihomoView) Live() bool { return v.Phase == MihomoRunning }

// Foreign reports whether another program's mihomo holds the controller.
func (v MihomoView) Foreign() bool { return v.Phase == MihomoForeign && v.Owner != nil }

// Text is the state in a word or a few, in the UI language: 运行中, 无法连接,
// Clash Party 占用控制端口.
func (v MihomoView) Text() string {
	T := i18n.Cur()
	switch v.Phase {
	case MihomoRunning:
		return T.Running
	case MihomoForeign:
		return T.MihomoForeignState(v.ClientName())
	}
	return T.Unreachable
}

// ClientName is the foreign program's product name, "" when unknown.
func (v MihomoView) ClientName() string {
	if v.Owner == nil {
		return ""
	}
	return v.Owner.Client.Name()
}

// Detail is the foreign process for a second line or a tooltip:
// "pid 4242 · /Applications/Clash Party.app/…/mihomo" (as much as is
// known); for the other phases the controller's error.
func (v MihomoView) Detail() string {
	if v.Phase != MihomoForeign {
		return v.Err
	}
	o := v.Owner
	if o == nil {
		return ""
	}
	var d string
	if o.PID > 0 {
		d = "pid " + strconv.Itoa(o.PID)
	}
	exe := o.Exe
	if exe == "" {
		exe = o.WorkDir
	}
	if exe != "" {
		if d != "" {
			d += " · "
		}
		d += exe
	}
	return d
}
