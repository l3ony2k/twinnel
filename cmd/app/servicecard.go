package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// The 服务 card (service.status / start / stop / restart, install and
// uninstall for mihomo) of the 内核 page. The GEO databases and the
// dashboard are in components.go.

// serviceNames are the services the helper allows over RPC.
var serviceNames = []string{"mihomo", "tailscaled"}

type serviceView struct {
	status  map[string]rpc.ServiceStatus
	errs    map[string]string
	loading bool
	last    *rpc.ServiceOpResult // last start/stop/... result
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
	listCard(c, listHead(c, nil, T.Services, func() {
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
			ui.Text(c, name).FontWeight(500)
			ui.Text(c, v.errs[name]).FontSize(plat.calloutSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Shrink(1)
		})
		return
	}
	state, col := serviceState(t, st)
	adopted := a.serviceAdopted(name)
	// mihomo runs, but not from a service Twinnel knows (the person's own
	// LaunchDaemon before takeover, or a terminal): it is running, and
	// Twinnel offers no service of its own over it (walkthrough 2, N11).
	own := name == "mihomo" && !st.Installed && !adopted && s.Mihomo.Reachable
	if own {
		state, col = T.SvcOwnRunning, t.Success
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Height(28).Children(func() { // one height with or without buttons
		dot(c, col, 8)
		ui.Text(c, name).FontWeight(500)
		ui.Text(c, state).FontSize(plat.calloutSize).TextColor(t.TextMuted)
		if adopted {
			tag(c, T.SvcAdopted).Tooltip(T.SvcAdoptedTip)
		}
		ui.Spacer(c)
		if !st.Installed {
			// Never offer Twinnel's own service while the person's adopted
			// one (or their own running mihomo) fills the role.
			if name == "mihomo" && !adopted && !own && a.opButton(c.Key("install"), iconOK, T.ServiceInstall, false, T.ServiceInstall+" "+name).Disabled(!w).Clicked() {
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
		// 卸载服务 is legitimate but not the way out of Twinnel (停止使用
		// Twinnel is): it sits in ⋯, styled as destructive. Not for an
		// adopted service, whose definition Twinnel never changes.
		if name == "mihomo" && !adopted {
			pushMenu(c.Key("more"), T.SvcMore, func(m *ui.Menu) {
				if m.Item(T.ServiceUninstall + "…").Danger().Disabled(!w).Chosen() {
					a.confirm(fmt.Sprintf(T.ServiceUninstallTitle, name), T.ServiceUninstallBody, T.ServiceUninstall, func() {
						a.serviceOp(rpc.MethodServiceUninstall, name, T.ServiceUninstall)
					})
				}
			})
		}
	})
	// Rows without a value are left out, not shown as — (R-unslop-27).
	if own {
		if l := a.ownMihomoLabel(); l != "" {
			kvMono(c, T.ServiceLabel, l)
		}
		return
	}
	if st.Label != "" {
		kvMono(c, T.ServiceLabel, st.Label)
	}
	if st.PID > 0 {
		kvMono(c, "PID", fmt.Sprint(st.PID))
	}
	if st.LastExitCode != nil && *st.LastExitCode != 0 {
		kvMono(c, T.ServiceExit, fmt.Sprint(*st.LastExitCode))
	}
	if st.Program != "" {
		kvMono(c, T.ServiceProgram, st.Program)
	}
	if st.Definition != "" {
		kvMono(c, T.ServiceDefinition, st.Definition)
	}
	if st.Error != "" {
		errorNote(c, T.RPCErrorText(0, st.Error))
	}
}

// ownMihomoLabel is the person's own mihomo service as takeover found it
// ("" when no plan names one).
func (a *app) ownMihomoLabel() string {
	for _, p := range a.hd.take.plans {
		if p.MihomoService != "" {
			return p.MihomoService
		}
	}
	return ""
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
