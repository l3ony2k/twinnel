package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 内核 page: installed mihomo kernels, update check (stable/alpha),
// install, rollback; Tailscale's version, install kind and the latest.

var kernelChannels = []string{"stable", "alpha"}

type kernelView struct {
	channel   int // index into kernelChannels
	inited    bool
	release   *rpc.KernelRelease
	ts        *rpc.TailscaleInfo
	tsLoading bool

	svc     serviceView
	geo, ui accessoryView
}

func (a *app) kernelPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	d := &a.hd
	v := &d.kern
	pageTitle(c, T.PageKernel)
	if !a.helperGate(c, s) {
		return
	}
	if !v.inited && d.settings != nil {
		v.inited = true
		if d.settings.KernelChannel == "alpha" {
			v.channel = 1
		}
	}
	a.opNotes(c, s)
	if a.section("kernel") {
		a.kernelCard(c, s)
	}
	if a.section("services") {
		a.servicesCard(c, s)
	}
	for _, k := range []accessoryKind{geoKind(), uiKind()} {
		if a.section(k.check) {
			ui.Column(c.Key(k.check)).Children(func() { a.accessoryCard(c, s, k) })
		}
	}
	if a.section("kernel") {
		a.tailscaleVersionCard(c, s)
	}
}

func (a *app) kernelCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.kern
	w := a.helperWrites(s)
	k := d.kernels
	listCard(c, listHead(c, nil, T.KernelMihomo, nil), func() {
		if k == nil {
			listRow(c).Children(func() { ui.Text(c, T.Loading).TextColor(t.TextMuted) })
			return
		}
		listRow(c).Children(func() {
			active := k.Active
			if k.External {
				active += " · " + T.KernelExternal
			}
			kv(c, T.KernelActive, active)
			if st := s.Helper.Status; st != nil && st.Mihomo.Version != "" {
				kv(c, T.KernelRunning, st.Mihomo.Version)
			}
			kv(c, T.KernelCurrent, orDash(k.Current))
			kv(c, T.KernelPrevious, orDash(k.Previous))
		})
		for _, kv := range k.Installed {
			listRow(c.Key(kv.Version)).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
					num(ui.Text(c, kv.Version).Font("monospace").FontSize(plat.bodySize - 1).SingleLine().Shrink(0))
					if kv.Current {
						tag(c, T.KernelCurrent)
					}
					if kv.Previous {
						tag(c, T.KernelPrevious)
					}
					ui.Spacer(c)
					if !kv.Time.IsZero() {
						num(small(c, kv.Time.Local().Format("2006-01-02")).SingleLine().Shrink(0))
					}
				})
				ui.Text(c, kv.Path).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Selectable()
			})
		}
		listRow(c).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.KernelChannel).TextColor(t.TextMuted).Width(labelWidth).Shrink(0)
				if ui.Segmented(c.Key("channel"), &v.channel, T.ChannelStable, T.ChannelAlpha).Changed() {
					v.release = nil
				}
				ui.Spacer(c)
				if iconButton(c.Key("k-check"), iconRefresh, T.KernelCheck, false).Disabled(d.busy != "").Clicked() {
					a.kernelCheck()
				}
				if k.Previous != "" && iconButton(c.Key("k-rollback"), iconRollback, T.KernelRollback, false).Disabled(!w).Clicked() {
					a.confirm(fmt.Sprintf(T.KernelRollbackTitle, k.Previous), T.KernelRollbackBody, T.KernelRollback, func() {
						a.helperOp(T.KernelRollback, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
							return hc.Call(ctx, rpc.MethodKernelRollback, nil, nil)
						}, nil)
					})
				}
			})
		})
		if r := v.release; r != nil {
			listRow(c).Children(func() {
				head, col := T.KernelUpToDate, t.Success
				switch {
				case r.UpdateAvailable && r.Current == "": // only an external binary so far
					head, col = fmt.Sprintf(T.KernelFirstInstall, r.Version), t.Accent
				case r.UpdateAvailable:
					head, col = fmt.Sprintf(T.KernelUpdate, r.Version), t.Accent
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
					dot(c, col, 8)
					ui.Text(c, head).FontWeight(600).Grow(1).Wrap()
					if r.UpdateAvailable {
						rel := *r
						if iconButton(c.Key("k-install"), iconOK, fmt.Sprintf(T.KernelInstall, r.Version), true).Disabled(!w).Clicked() {
							a.confirm(fmt.Sprintf(T.KernelInstallTitle, rel.Version), T.KernelInstallBody, T.KernelInstallOK, func() {
								a.kernelInstall(rel)
							})
						}
					}
				})
				pub := ""
				if !r.Published.IsZero() {
					pub = " · " + r.Published.Local().Format("2006-01-02")
				}
				kv(c, T.KernelLatest, r.Version+" ("+r.Tag+")"+pub)
				kv(c, T.KernelAsset, fmt.Sprintf("%s · %s", r.Asset, status.Bytes(r.Size)))
				digest := r.Digest
				if digest == "" {
					digest = T.KernelNoDigest
				}
				kv(c, T.KernelDigest, digest)
			})
		}
	})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (a *app) kernelCheck() {
	T := i18n.T
	ch := kernelChannels[a.hd.kern.channel]
	var r rpc.KernelRelease
	a.helperOp(T.KernelCheck, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodKernelCheck, rpc.KernelCheckParams{Channel: ch}, &r)
	}, func(err error) {
		a.hd.kern.release = nil
		if err == nil {
			a.hd.kern.release = &r
		}
	})
}

func (a *app) kernelInstall(r rpc.KernelRelease) {
	T := i18n.T
	a.helperOp(fmt.Sprintf(T.KernelInstall, r.Version), 10*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodKernelInstall, rpc.KernelInstallParams{Channel: r.Channel, Version: r.Version}, nil)
	}, func(err error) {
		if err == nil {
			a.hd.kern.release = nil
		}
	})
}

func (a *app) tailscaleVersionCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.kern
	listCard(c, listHead(c, nil, T.Tailscale, func() {
		label := T.KernelCheck
		if v.tsLoading {
			label = T.Checking
		}
		if iconButton(c.Key("ts-check"), iconRefresh, label, false).Disabled(v.tsLoading).Clicked() {
			a.tailscaleCheck()
		}
	}), func() {
		listRow(c).Children(func() { hintText(c, T.TSVersionHint) })
		info := v.ts
		if info == nil {
			return
		}
		listRow(c).Children(func() {
			if !info.Installed {
				kv(c, T.State, T.TSNotInstalled)
			} else {
				kv(c, T.Version, info.Version)
				kv(c, T.Install, T.TSInstallKind(info.InstallKind))
				kv(c, T.BinaryPath, info.Path)
			}
			if info.Latest != "" {
				latest := info.Latest
				if info.UpdateAvailable {
					latest += " · " + T.UpdateAvailable
				}
				kvWith(c, T.KernelLatest, func() {
					col := t.Text
					if info.UpdateAvailable {
						col = t.Accent
					}
					ui.Text(c, latest).TextColor(col).SingleLine().Shrink(1)
				})
				if info.UpdateAvailable {
					hintText(c, T.TSUpdateHint(info.InstallKind))
				}
			}
			if info.Error != "" {
				errorNote(c, T.ErrorPrefix+strings.TrimSpace(info.Error))
			}
		})
	})
}

// tailscaleCheck asks the helper for tailscale.info (reads the local CLI
// and pkgs.tailscale.com; installs nothing).
func (a *app) tailscaleCheck() {
	v := &a.hd.kern
	v.tsLoading = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var info rpc.TailscaleInfo
		err := hc.Call(ctx, rpc.MethodTailscaleInfo, nil, &info)
		mygo.RunOnMain(func() {
			v.tsLoading = false
			if err != nil {
				info = rpc.TailscaleInfo{Error: helperError(err)}
			}
			v.ts = &info
			a.invalidate()
		})
	}()
}
