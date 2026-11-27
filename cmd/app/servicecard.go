package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 服务 card (service.status / start / stop / restart, install and
// uninstall for mihomo) and the geo database and external UI cards
// (geo.* / ui.*) of the 内核 page.

// serviceNames are the services the helper allows over RPC.
var serviceNames = []string{"mihomo", "tailscaled"}

type serviceView struct {
	status  map[string]rpc.ServiceStatus
	errs    map[string]string
	loading bool
	last    *rpc.ServiceOpResult // last start/stop/... result
}

type accessoryView struct {
	check *rpc.AccessoryCheck
	res   *rpc.AccessoryUpdateResult
	force bool
}

// loadServices reads service.status for every service (launchctl print in
// the helper, read-only). Main thread.
func (a *app) loadServices() {
	v := &a.hd.kern.svc
	if !a.hd.connected || v.loading {
		return
	}
	v.loading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		st := map[string]rpc.ServiceStatus{}
		errs := map[string]string{}
		for _, name := range serviceNames {
			var r rpc.ServiceStatus
			if err := hc.Call(ctx, rpc.MethodServiceStatus, rpc.ServiceParams{Name: name}, &r); err != nil {
				errs[name] = helperError(err)
				continue
			}
			st[name] = r
		}
		mygo.RunOnMain(func() {
			v.loading = false
			v.status, v.errs = st, errs
			a.invalidate()
		})
	}()
}

// serviceEvent reports whether a batch changes what the 服务 card shows.
func serviceEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		switch e.Type {
		case rpc.EventServiceChanged, rpc.EventComposeApplied, rpc.EventKernelChanged:
			return true
		}
	}
	return false
}

func (a *app) servicesCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.kern.svc
	listCard(c, listHead(c, iconService, T.Services, func() {
		if iconButton(c.Key("svc-refresh"), iconRefresh, T.Refresh, false).Disabled(v.loading).Clicked() {
			a.loadServices()
		}
	}), func() {
		if v.status == nil && len(v.errs) == 0 {
			listRow(c).Children(func() { ui.Text(c, T.Loading).TextColor(t.TextMuted) })
			return
		}
		for _, name := range serviceNames {
			listRow(c.Key("svc-" + name)).Children(func() { a.serviceRow(c, s, name) })
		}
		if r := v.last; r != nil {
			listRow(c).Children(func() { serviceOpResult(c, r) })
		}
	})
}

func (a *app) serviceRow(c *ui.Context, s status.Snapshot, name string) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.kern.svc
	w := a.helperWrites(s)
	st, ok := v.status[name]
	if !ok {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, t.TextMuted, 8)
			ui.Text(c, name).FontWeight(600)
			ui.Text(c, v.errs[name]).FontSize(plat.calloutSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Shrink(1)
		})
		return
	}
	state, col := serviceState(t, st)
	adopted := a.serviceAdopted(name)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Height(28).Children(func() { // one height with or without buttons
		dot(c, col, 8)
		ui.Text(c, name).FontWeight(600)
		ui.Text(c, state).FontSize(plat.calloutSize).TextColor(t.TextMuted)
		if adopted {
			tag(c, T.SvcAdopted).Tooltip(T.SvcAdoptedTip)
		}
		ui.Spacer(c)
		if !st.Installed {
			// Never offer duet's own service while the person's adopted
			// one fills the role.
			if name == "mihomo" && !adopted && a.opButton(c.Key("install"), iconOK, T.ServiceInstall, false, T.ServiceInstall+" "+name).Disabled(!w).Clicked() {
				a.confirm(fmt.Sprintf(T.ServiceInstallTitle, name), "", T.ServiceInstall, func() {
					a.serviceOp(rpc.MethodServiceInstall, name, T.ServiceInstall)
				})
			}
			return
		}
		if !st.Running && pushButton(c.Key("start"), T.ServiceStart).Disabled(!w).Clicked() {
			a.serviceOp(rpc.MethodServiceStart, name, T.ServiceStart)
		}
		if st.Running && pushButton(c.Key("restart"), T.ServiceRestart).Disabled(!w).Clicked() {
			a.confirm(fmt.Sprintf(T.ServiceRestartTitle, name), T.ServiceRestartBody, T.ServiceRestart, func() {
				a.serviceOp(rpc.MethodServiceRestart, name, T.ServiceRestart)
			})
		}
		if st.Running && pushButton(c.Key("stop"), T.ServiceStop).Disabled(!w).Clicked() {
			a.confirm(fmt.Sprintf(T.ServiceStopTitle, name), T.ServiceStopBody, T.ServiceStop, func() {
				a.serviceOp(rpc.MethodServiceStop, name, T.ServiceStop)
			})
		}
		// 卸载服务 is legitimate but not the way out of duet (停止使用
		// duet is): it sits in ⋯, styled as destructive. Not for an
		// adopted service, whose definition duet never changes.
		if name == "mihomo" && !adopted {
			ui.MenuButton(c.Key("more"), T.SvcMore, func(m *ui.Menu) {
				if m.Item(T.ServiceUninstall + "…").Danger().Disabled(!w).Chosen() {
					a.confirm(fmt.Sprintf(T.ServiceUninstallTitle, name), T.ServiceUninstallBody, T.ServiceUninstall, func() {
						a.serviceOp(rpc.MethodServiceUninstall, name, T.ServiceUninstall)
					})
				}
			})
		}
	})
	kv(c, T.ServiceLabel, orDash(st.Label))
	if st.PID > 0 {
		kv(c, "PID", fmt.Sprint(st.PID))
	}
	if st.LastExitCode != nil && *st.LastExitCode != 0 {
		kv(c, T.ServiceExit, fmt.Sprint(*st.LastExitCode))
	}
	if st.Program != "" {
		kv(c, T.ServiceProgram, st.Program)
	}
	if st.Definition != "" {
		kv(c, T.ServiceDefinition, st.Definition)
	}
	if st.Error != "" {
		errorNote(c, T.ErrorPrefix+st.Error)
	}
}

// serviceAdopted: the helper runs this service under a definition it
// adopted (takeover), so it only starts and stops it.
func (a *app) serviceAdopted(name string) bool {
	i := a.hm.info
	return name == "mihomo" && i != nil && i.Mihomo.Adopted && i.Hardened == nil
}

// serviceState is the one-word state and its dot color.
func serviceState(t *ui.Theme, st rpc.ServiceStatus) (string, ui.Color) {
	T := i18n.T
	switch {
	case !st.Installed:
		return T.ServiceNotInstalled, t.TextMuted
	case st.Running:
		return T.ServiceRunning, t.Success
	case st.Loaded:
		return T.ServiceLoaded, t.Warning
	default:
		return T.ServiceStopped, t.Warning
	}
}

func serviceOpResult(c *ui.Context, r *rpc.ServiceOpResult) {
	t := c.Theme()
	T := i18n.T
	ui.Column(c).Gap(4).Children(func() {
		head := fmt.Sprintf(T.ServiceOpHead, r.Op, r.Name)
		if r.DryRun {
			head += " · " + T.ServiceDryRun
		}
		h := ui.Text(c, head).FontWeight(600)
		if r.DryRun {
			h.Tooltip(T.ServiceDryRunHint)
		}
		for i, step := range r.Steps {
			ui.Text(c.Key(fmt.Sprint("step", i)), step).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
		}
	})
}

func (a *app) serviceOp(method, name, label string) {
	v := &a.hd.kern.svc
	var r rpc.ServiceOpResult
	a.helperOp(label+" "+name, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, method, rpc.ServiceParams{Name: name}, &r)
	}, func(err error) {
		v.last = nil
		if err == nil {
			v.last = &r
			if v.status != nil {
				v.status[name] = r.Status
			}
		}
		a.loadServices()
	})
}

// --- geo databases and external UI ---

type accessoryKind struct {
	title         string
	icon          *ui.SVG
	check, update string
	view          func(a *app) *accessoryView
}

func geoKind() accessoryKind {
	return accessoryKind{
		title: i18n.T.GeoTitle, icon: iconGeo,
		check: rpc.MethodGeoCheck, update: rpc.MethodGeoUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.geo },
	}
}

func uiKind() accessoryKind {
	return accessoryKind{
		title: i18n.T.UITitle, icon: iconDashboard,
		check: rpc.MethodUICheck, update: rpc.MethodUIUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.ui },
	}
}

func (a *app) accessoryCard(c *ui.Context, s status.Snapshot, k accessoryKind) {
	T := i18n.T
	d := &a.hd
	v := k.view(a)
	w := a.helperWrites(s)
	listCard(c, listHead(c, k.icon, k.title, func() {
		if a.opButton(c.Key("check"), iconRefresh, T.KernelCheck, false, k.title+" · "+T.KernelCheck).Disabled(d.busy != "").Clicked() {
			a.accessoryCheck(k)
		}
	}), func() {
		listRow(c).Children(func() {
			if ck := v.check; ck != nil && ck.Dir != "" {
				kv(c, T.AccessoryDir, ck.Dir)
			} else if ck == nil {
				small(c, T.AccessoryUnchecked) // state, before the first check
			}
		})
		if ck := v.check; ck != nil {
			updates := 0
			for i, f := range ck.Files {
				if f.UpdateAvailable || !f.Installed {
					updates++
				}
				listRow(c.Key(fmt.Sprint("f", i))).Children(func() { accessoryFile(c, f) })
			}
			listRow(c).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Switch(c.Key("force"), &v.force).Label(T.AccessoryForce)
					ui.Text(c, T.AccessoryForce).FontSize(plat.calloutSize)
					ui.Spacer(c)
					label := T.AccessoryUpdate
					if updates == 0 && !v.force {
						label = T.AccessoryUpToDate
					}
					if a.opButton(c.Key("update"), iconOK, label, updates > 0 || v.force, k.title+" · "+T.AccessoryUpdate).Disabled(!w || (updates == 0 && !v.force)).Clicked() {
						a.accessoryUpdate(k, v.force)
					}
				})
			})
		}
		if r := v.res; r != nil {
			listRow(c).Children(func() { a.accessoryResult(c, s, r) })
		}
	})
}

func accessoryFile(c *ui.Context, f rpc.AccessoryFile) {
	t := c.Theme()
	T := i18n.T
	state, col := T.AccessoryCurrent, t.Success
	switch {
	case f.Error != "":
		state, col = T.AccessoryError, t.Danger
	case !f.Installed:
		state, col = T.AccessoryMissing, t.Accent
	case f.UpdateAvailable:
		state, col = T.UpdateAvailable, t.Accent
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, f.Name).FontWeight(600).SingleLine().Shrink(0)
		ui.Text(c, state).FontSize(plat.calloutSize).TextColor(t.TextMuted).SingleLine().Shrink(0)
		if f.Verifiable {
			tag(c, T.AccessoryVerifiable)
		} else {
			tag(c, T.AccessoryUnverifiable)
		}
		ui.Spacer(c)
		ui.Text(c, filepath.Base(f.File)).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Shrink(1).Selectable().Tooltip(f.File)
	})
	local := T.AccessoryMissing
	if f.Installed {
		local = shortDigest(f.SHA256)
		if f.Size > 0 {
			local += " · " + status.Bytes(f.Size)
		}
	}
	kv(c, T.AccessoryLocal, local)
	if f.Remote != "" {
		remote := shortDigest(f.Remote)
		if f.RemoteSize > 0 {
			remote += " · " + status.Bytes(f.RemoteSize)
		}
		kv(c, T.AccessoryRemote, remote)
	}
	if f.Error != "" {
		errorNote(c, T.ErrorPrefix+f.Error)
	}
}

// shortDigest keeps a sha256 readable: "sha256:" dropped, first 16 hex.
// Tags (the UI's installed version) pass through.
func shortDigest(s string) string {
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" {
		return s[:16] + "…"
	}
	return s
}

func (a *app) accessoryResult(c *ui.Context, s status.Snapshot, r *rpc.AccessoryUpdateResult) {
	t := c.Theme()
	T := i18n.T
	ui.Column(c).Gap(4).Children(func() {
		if len(r.Updated) > 0 {
			ui.Text(c, fmt.Sprintf(T.AccessoryUpdated, strings.Join(r.Updated, ", "))).FontSize(plat.calloutSize).TextColor(pal(t).okText).Wrap().LineHeight(lineText)
		} else if r.Error == "" {
			ui.Text(c, T.AccessoryNothing).FontSize(plat.calloutSize).TextColor(t.TextMuted)
		}
		if len(r.Skipped) > 0 {
			ui.Text(c, fmt.Sprintf(T.AccessorySkipped, strings.Join(r.Skipped, ", "))).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
		}
		if r.RolledBack {
			ui.Text(c, T.AccessoryRolledBack).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Wrap().LineHeight(lineText)
		}
		if r.Error != "" {
			errorNote(c, T.ErrorPrefix+r.Error)
		}
		if r.ReloadNeeded {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.AccessoryReloadNeeded).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Grow(1).Wrap().LineHeight(lineText)
				if a.opButton(c.Key("apply"), iconRefresh, T.AccessoryApply, true, T.AccessoryApply).Disabled(!a.helperWrites(s)).Clicked() {
					a.applyForce(T.AccessoryApplyReason, func() { r.ReloadNeeded = false })
				}
			})
		}
	})
}

// applyForce runs compose.apply with force (reload even when the file is
// unchanged), e.g. so mihomo reads new geo databases.
func (a *app) applyForce(reason string, ok func()) {
	T := i18n.T
	var res rpc.ApplyResult
	a.helperOp(T.AccessoryApply, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		if err := hc.Call(ctx, rpc.MethodComposeApply, rpc.ApplyParams{Reason: reason, Force: true}, &res); err != nil {
			return err
		}
		if res.Error != "" {
			return errors.New(res.Error)
		}
		return nil
	}, func(err error) {
		if err == nil && ok != nil {
			ok()
		}
	})
}

func (a *app) accessoryCheck(k accessoryKind) {
	var r rpc.AccessoryCheck
	a.helperOp(k.title+" · "+i18n.T.KernelCheck, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, k.check, nil, &r)
	}, func(err error) {
		v := k.view(a)
		if err == nil {
			v.check = &r
		}
	})
}

func (a *app) accessoryUpdate(k accessoryKind, force bool) {
	var r rpc.AccessoryUpdateResult
	a.helperOp(k.title+" · "+i18n.T.AccessoryUpdate, 10*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, k.update, rpc.AccessoryUpdateParams{Force: force}, &r)
	}, func(err error) {
		v := k.view(a)
		v.res = nil
		if err == nil {
			v.res = &r
			if len(r.Files) > 0 && v.check != nil {
				v.check.Files = r.Files
			}
			v.force = false
		}
	})
}
