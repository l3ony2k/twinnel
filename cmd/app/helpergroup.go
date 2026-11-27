package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// On Linux the helper's socket is for one group (sudo or wheel, else
// twinnel). A person outside it gets "permission denied" (HelperDenied),
// which said "not running" before. The app tells the two cases apart:
// not in the group (加入 runs `twinnel helper upgrade`, which adds them,
// with one password dialog), or added but this login started before
// (log out and back in).

// groupGap is why this session cannot open the helper's socket for want
// of its group: group "" when that is not the reason.
type groupGap struct {
	group  string
	listed bool // the group database lists the user: only a new login is missing
}

// groupGapProbe reads the gap for a socket (helper_linux.go); nil where
// sockets are not guarded by a group this way.
var groupGapProbe func(socket string) groupGap

// The last probe, for a few seconds: it stats the socket and reads
// /etc/group, too much for every frame. Main thread only.
var (
	gapMemo   groupGap
	gapSocket string
	gapAt     time.Time
)

const gapFresh = 3 * time.Second

// helperGroupGap is the gap while the helper refuses this user; none in
// shot mode (it would read the machine) or in any other state.
func (a *app) helperGroupGap(h status.Helper) groupGap {
	if groupGapProbe == nil || a.shotMode || h.State != status.HelperDenied {
		return groupGap{}
	}
	if h.Socket != gapSocket || time.Since(gapAt) > gapFresh {
		gapMemo, gapSocket, gapAt = groupGapProbe(h.Socket), h.Socket, time.Now()
	}
	return gapMemo
}

// helperDownText is the sidebar's and the panel's line for a helper that
// is installed but not reachable.
func (a *app) helperDownText(s status.Snapshot) string {
	T := i18n.T
	switch g := a.helperGroupGap(s.Helper); {
	case g.group == "":
		return T.HelperDownRow
	case g.listed:
		return T.HelperGroupRowRelogin
	default:
		return i18n.F(T.HelperGroupRowMissing, g.group)
	}
}

// helperGroupRow is 设置 → 后台助手's row for the gap: what is wrong and,
// while the user is not in the group, 加入.
func (a *app) helperGroupRow(c *ui.Context, s status.Snapshot) {
	g := a.helperGroupGap(s.Helper)
	if g.group == "" {
		return
	}
	T := i18n.T
	kvWith(c, "", func() {
		if g.listed {
			ui.Text(c, i18n.F(T.HelperGroupRelogin, g.group)).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
			return
		}
		ui.Text(c, i18n.F(T.HelperGroupMissing, g.group)).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
		if busyButton(c.Key("hm-group"), T.HelperGroupFix, true, a.hm.running == T.HelperGroupFix, !a.helperOpsEnabled(s)).Clicked() {
			a.fixHelperGroup()
		}
	})
}

// fixHelperGroup runs `twinnel helper upgrade --yes`: with the helper
// current it only adds the user to the socket group (one password
// dialog); with an older one it updates it too.
func (a *app) fixHelperGroup() {
	T := i18n.T
	a.helperCLIOp(T.HelperGroupFix, T.DoneHelperGroup, upgradeArgs("--yes"), func() { gapAt = time.Time{} })
}
