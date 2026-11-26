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
	card(c, T.Services, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.ServicesHint).FontSize(12).TextColor(t.TextMuted).Grow(1).Wrap()
			if iconButton(c.Key("svc-refresh"), iconRefresh, T.Refresh, false).Disabled(v.loading).Clicked() {
				a.loadServices()
			}
		})
		if v.status == nil && len(v.errs) == 0 {
			ui.Text(c, T.Loading).TextColor(t.TextMuted)
			return
		}
		for _, name := range serviceNames {
			ui.Column(c.Key("svc-"+name)).Gap(6).Padding(10, 12).Radius(8).Background(t.Background).Border(1, t.Border).Children(func() {
				a.serviceRow(c, s, name)
			})
		}
		if r := v.last; r != nil {
			serviceOpResult(c, r)
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
			ui.Text(c, name).FontSize(13).Bold()
			ui.Text(c, v.errs[name]).FontSize(12).TextColor(t.Danger).Wrap().Shrink(1)
		})
		return
	}
	state, col := serviceState(t, st)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, name).FontSize(13).Bold()
		ui.Text(c, state).FontSize(12).TextColor(t.TextMuted)
		ui.Spacer(c)
		if !st.Installed {
			if name == "mihomo" && iconButton(c.Key("install"), iconOK, T.ServiceInstall, false).Disabled(!w).Clicked() {
				a.confirm(fmt.Sprintf(T.ServiceInstallTitle, name), T.ServiceInstallBody, T.ServiceInstall, func() {
					a.serviceOp(rpc.MethodServiceInstall, name, T.ServiceInstall)
				})
			}
			return
		}
		if !st.Running && ui.Button(c.Key("start"), T.ServiceStart).Disabled(!w).Clicked() {
			a.serviceOp(rpc.MethodServiceStart, name, T.ServiceStart)
		}
		if st.Running && ui.Button(c.Key("restart"), T.ServiceRestart).Disabled(!w).Clicked() {
			a.confirm(fmt.Sprintf(T.ServiceRestartTitle, name), T.ServiceRestartBody, T.ServiceRestart, func() {
				a.serviceOp(rpc.MethodServiceRestart, name, T.ServiceRestart)
			})
		}
		if st.Running && ui.Button(c.Key("stop"), T.ServiceStop).Disabled(!w).Clicked() {
			a.confirm(fmt.Sprintf(T.ServiceStopTitle, name), T.ServiceStopBody, T.ServiceStop, func() {
				a.serviceOp(rpc.MethodServiceStop, name, T.ServiceStop)
			})
		}
		if name == "mihomo" && ui.Button(c.Key("uninstall"), T.ServiceUninstall).Disabled(!w).Clicked() {
			a.confirm(fmt.Sprintf(T.ServiceUninstallTitle, name),
				fmt.Sprintf(T.ServiceUninstallBody, st.Label, orDash(st.Definition)), T.ServiceUninstall, func() {
					a.serviceOp(rpc.MethodServiceUninstall, name, T.ServiceUninstall)
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
		ui.Text(c, T.ErrorPrefix+st.Error).FontSize(12).TextColor(t.Danger).Wrap().Selectable()
	}
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
		ui.Text(c, head).FontSize(12).Bold()
		if r.DryRun {
			ui.Text(c, T.ServiceDryRunHint).FontSize(12).TextColor(t.Warning).Wrap()
		}
		for i, step := range r.Steps {
			ui.Text(c.Key(fmt.Sprint("step", i)), step).Font("monospace").FontSize(11).TextColor(t.TextMuted).Wrap().Selectable()
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
	title, hint   string
	check, update string
	view          func(a *app) *accessoryView
}

func geoKind() accessoryKind {
	return accessoryKind{
		title: i18n.T.GeoTitle, hint: i18n.T.GeoHint,
		check: rpc.MethodGeoCheck, update: rpc.MethodGeoUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.geo },
	}
}

func uiKind() accessoryKind {
	return accessoryKind{
		title: i18n.T.UITitle, hint: i18n.T.UIHint,
		check: rpc.MethodUICheck, update: rpc.MethodUIUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.ui },
	}
}

func (a *app) accessoryCard(c *ui.Context, s status.Snapshot, k accessoryKind) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := k.view(a)
	w := a.helperWrites(s)
	card(c, k.title, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, k.hint).FontSize(12).TextColor(t.TextMuted).Grow(1).Wrap()
			if iconButton(c.Key("check"), iconRefresh, T.KernelCheck, false).Disabled(d.busy != "").Clicked() {
				a.accessoryCheck(k)
			}
		})
		if ck := v.check; ck != nil {
			if ck.Dir != "" {
				kv(c, T.AccessoryDir, ck.Dir)
			}
			updates := 0
			for i, f := range ck.Files {
				if f.UpdateAvailable || !f.Installed {
					updates++
				}
				ui.Column(c.Key(fmt.Sprint("f", i))).Gap(3).Padding(8, 12).Radius(8).Background(t.Background).Border(1, t.Border).Children(func() {
					accessoryFile(c, f)
				})
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Switch(c.Key("force"), &v.force).Label(T.AccessoryForce)
				ui.Text(c, T.AccessoryForce).FontSize(12)
				ui.Spacer(c)
				label := T.AccessoryUpdate
				if updates == 0 && !v.force {
					label = T.AccessoryUpToDate
				}
				if iconButton(c.Key("update"), iconOK, label, updates > 0 || v.force).Disabled(!w || (updates == 0 && !v.force)).Clicked() {
					a.accessoryUpdate(k, v.force)
				}
			})
		}
		if r := v.res; r != nil {
			a.accessoryResult(c, s, r)
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
		ui.Text(c, f.Name).FontSize(13).Bold().Width(70).Shrink(0)
		ui.Text(c, state).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
		if f.Verifiable {
			ui.Badge(c, T.AccessoryVerifiable)
		} else {
			ui.Badge(c, T.AccessoryUnverifiable)
		}
		ui.Spacer(c)
		ui.Text(c, filepath.Base(f.File)).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).Selectable()
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
		ui.Text(c, T.ErrorPrefix+f.Error).FontSize(12).TextColor(t.Danger).Wrap().Selectable()
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
			ui.Text(c, fmt.Sprintf(T.AccessoryUpdated, strings.Join(r.Updated, ", "))).FontSize(12).TextColor(t.Success).Wrap()
		} else if r.Error == "" {
			ui.Text(c, T.AccessoryNothing).FontSize(12).TextColor(t.TextMuted)
		}
		if len(r.Skipped) > 0 {
			ui.Text(c, fmt.Sprintf(T.AccessorySkipped, strings.Join(r.Skipped, ", "))).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		if r.RolledBack {
			ui.Text(c, T.AccessoryRolledBack).FontSize(12).TextColor(t.Warning).Wrap()
		}
		if r.Error != "" {
			ui.Text(c, T.ErrorPrefix+r.Error).FontSize(12).TextColor(t.Danger).Wrap().Selectable()
		}
		if r.ReloadNeeded {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.AccessoryReloadNeeded).FontSize(12).TextColor(t.Warning).Grow(1).Wrap()
				if iconButton(c.Key("apply"), iconRefresh, T.AccessoryApply, true).Disabled(!a.helperWrites(s)).Clicked() {
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
