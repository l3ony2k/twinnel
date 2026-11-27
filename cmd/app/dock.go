package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// The Dock icon (macOS). Twinnel is a menu bar app; by default it also shows
// in the Dock (and in ⌘Tab) while its main window is open, as Rectangle or
// Docker Desktop do. 设置 → 通用 → 在程序坞中显示 can make that always or
// never. The bundle keeps LSUIElement, so a launch never flashes a Dock
// icon; the policy is set in code. Other systems: the taskbar follows the
// window there, nothing to do.

// dockPolicy is the activation policy for the setting (Config.Dock) and
// whether a main window is open. The flyout alone is not a main window.
func dockPolicy(setting string, windowOpen bool) mygo.ActivationPolicy {
	switch setting {
	case status.DockAlways:
		return mygo.ActivationPolicyRegular
	case status.DockNever:
		return mygo.ActivationPolicyAccessory
	}
	if windowOpen {
		return mygo.ActivationPolicyRegular
	}
	return mygo.ActivationPolicyAccessory
}

// dockManaged reports whether this run sets the policy: macOS, not shots
// (shot runs stay accessory and never activate).
func (a *app) dockManaged() bool {
	return plat.dock && !a.shotMode
}

// initDock sets the launch policy before Run, so 始终 starts with a Dock
// icon without activating the app.
func (a *app) initDock() {
	p := mygo.ActivationPolicyAccessory
	if a.dockManaged() {
		p = dockPolicy(a.cfg.Dock, false)
	}
	mygo.App.SetActivationPolicy(p)
	a.dockPol = p
}

// readyDock wires the Dock icon once the app runs: a click on it (or
// opening Twinnel.app again) opens the main window, and its menu.
func (a *app) readyDock() {
	if !a.dockManaged() {
		return
	}
	mygo.App.OnActivate(func(bool) { a.openMain() })
	a.setDockMenu()
}

// setDockMenu is the Dock icon's menu above the standard items (退出
// there quits Twinnel, as 退出 Twinnel does; the helper keeps running).
func (a *app) setDockMenu() {
	if !a.dockManaged() {
		return
	}
	mygo.App.Dock.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: i18n.T.DockOpen, Click: func(*mygo.MenuItem, *mygo.Window) { a.openMain() }},
	}))
}

// syncDock applies the policy for the setting and the main window. Call
// it before a main window shows (so it gets its Dock icon and ⌘Tab) and
// after the last one closed. Main thread.
func (a *app) syncDock() { a.syncDockFor(a.win != nil) }

// syncDockFor is syncDock as if a main window were open or not: openMain
// switches before it creates the window.
func (a *app) syncDockFor(windowOpen bool) {
	if !a.dockManaged() {
		return
	}
	p := dockPolicy(a.cfg.Dock, windowOpen)
	if p == a.dockPol {
		return
	}
	a.dockPol = p
	mygo.App.SetActivationPolicy(p)
	// Back to a menu bar app while Twinnel is in front with no window (its
	// window just closed): hand the front to the app before it, so no
	// stale Twinnel menu bar stays. Not while the flyout is open: it has the
	// keys; not with the window open (从不 chosen in it).
	if p == mygo.ActivationPolicyAccessory && a.win == nil && a.panel == nil && mygo.App.IsActive() {
		mygo.App.Hide()
	}
}

// unhideApp undoes the Hide of syncDock before Twinnel shows a window.
func (a *app) unhideApp() {
	if a.dockManaged() && mygo.App.IsHidden() {
		mygo.App.Show()
	}
}

// dockChoices are the setting's options, in the order shown, with their
// Config.Dock values.
func dockChoices() (labels, values []string) {
	T := i18n.T
	return []string{T.DockWhenOpen, T.DockAlways, T.DockNever}, []string{"", status.DockAlways, status.DockNever}
}

// dockRow is 设置 → 通用 → 在程序坞中显示 (macOS).
func (a *app) dockRow(c *ui.Context) {
	if !plat.dock {
		return
	}
	T := i18n.T
	labels, values := dockChoices()
	sel := labels[0]
	for i, v := range values {
		if v == a.cfg.Dock {
			sel = labels[i]
		}
	}
	settingsRow(c, T.ShowInDock, "", nil, func() {
		if ui.Select(c.Key("dock"), &sel, labels).Label(T.ShowInDock).Changed() {
			for i, l := range labels {
				if l == sel {
					a.setDock(values[i])
				}
			}
		}
	})
}

// setDock saves the setting and applies it at once. Main thread.
func (a *app) setDock(v string) {
	cfg := a.cfg
	cfg.Dock = v
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		a.settings.msg, a.settings.failed, a.settings.mihomoErr = i18n.T.SaveFailed+err.Error(), true, false
		return
	}
	a.cfg = cfg
	a.syncDock()
	if a.win != nil && a.dockPol == mygo.ActivationPolicyRegular {
		// Regular activates the app; keep the window the one in front.
		a.win.Focus()
	}
}
