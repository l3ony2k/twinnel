package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
	checkErr  string // the last check's error (behind 复制详情)
	ts        *rpc.TailscaleInfo
	tsLoading bool

	svc     serviceView
	geo, ui accessoryView
}

func (a *app) kernelPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	d := &a.hd
	v := &d.kern
	// One 检查更新 for the page: mihomo, the GEO databases, the external
	// UI and Tailscale at once (R-unslop-35).
	pageHeader(c, T.PageKernel, func() {
		if !s.Helper.Connected() {
			return
		}
		if a.opButton(c.Key("check-all"), iconRefresh, T.KernelCheck, false, T.KernelCheck).Disabled(d.busy != "").Clicked() {
			a.checkUpdates()
		}
	})
	if !a.helperGate(c, s) {
		return
	}
	if !v.inited && d.settings != nil {
		v.inited = true
		if d.settings.KernelChannel == "alpha" {
			v.channel = 1
		}
	}
	a.refreshHelperMgmt(s) // helper.info: is mihomo's service adopted
	a.opNotes(c, s)
	if a.section("kernel") {
		a.kernelCard(c, s)
	}
	if a.section("services") {
		a.servicesCard(c, s)
	}
	a.componentsCard(c, s)
}

// componentsCard is 内核 → 组件: the GEO databases, the external UI and
// Tailscale, one section each in one list.
func (a *app) componentsCard(c *ui.Context, s status.Snapshot) {
	listCard(c, listHead(c, nil, i18n.T.PgComponents, nil), func() {
		for _, k := range []accessoryKind{geoKind(), uiKind()} {
			if a.section(k.check) {
				ui.Column(c.Key(k.check)).Children(func() { a.accessoryRows(c, s, k) })
			}
		}
		if a.section("kernel") {
			a.tailscaleRows(c, s)
		}
	})
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
			running := ""
			if st := s.Helper.Status; st != nil {
				running = st.Mihomo.Version
			}
			// Before the takeover the running mihomo is the user's own:
			// duet's kernel (k.Active) is not what runs.
			own := !s.TakenOver()
			kvTipWith(c, T.State, T.TipKernelState, nil, func() {
				state := T.KernelStateManaged
				switch {
				case own:
					state = T.KernelStateOwn
				case k.External:
					state = T.KernelStateExternal
				}
				tag(c, state)
				if !own && !k.External && pendingRestart(running, k.Current) {
					effectChip(c, T.KernelPendingRestart, "", true, false)
				}
			})
			// 使用中 is one row: duet's current kernel by version and file,
			// or the binary that runs; rows without a value are left out
			// (R-unslop-27).
			switch {
			case own:
				a.liveFileRow(c, "active", T.KernelActive, a.live.Binary)
			case k.Active != "" || k.Current != "":
				used := filepath.Base(k.Active)
				if !k.External && k.Current != "" {
					used = k.Current
					if k.Active != "" {
						used += " · " + filepath.Base(k.Active)
					}
				}
				kvWith(c, T.KernelActive, func() {
					monoText(c, used).SingleLine().Shrink(1).Selectable().Tooltip(k.Active)
				})
			}
			if running != "" {
				kvMono(c, T.KernelRunning, running)
			}
			if !own && !k.External && k.Previous != "" { // 上一个 only means something for duet's kernels
				kvMono(c, T.KernelPrevious, k.Previous)
			}
		})
		own := !s.TakenOver() // duet's kernels: none of them runs yet
		for _, kv := range k.Installed {
			listRow(c.Key(kv.Version)).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
					monoText(c, kv.Version).SingleLine().Shrink(0)
					if kv.Current && !own {
						tag(c, T.KernelCurrent)
					}
					if kv.Previous && !own {
						tag(c, T.KernelPrevious)
					}
					ui.Spacer(c)
					if !kv.Time.IsZero() {
						num(small(c, kv.Time.Local().Format("2006-01-02")).SingleLine().Shrink(0))
					}
				})
				mono(ui.Text(c, kv.Path)).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Selectable().Tooltip(kv.Path)
			})
		}
		listRow(c).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.KernelChannel).TextColor(t.TextMuted).Width(labelWidth).Shrink(0)
				if ui.Segmented(c.Key("channel"), &v.channel, T.ChannelStable, T.ChannelAlpha).Changed() {
					v.release = nil
				}
				ui.Spacer(c)
				if k.Previous != "" && a.opButton(c.Key("k-rollback"), iconRollback, T.KernelRollback, false, T.KernelRollback).Disabled(!w).Clicked() {
					a.confirm(fmt.Sprintf(T.KernelRollbackTitle, k.Previous), T.KernelRollbackBody, T.KernelRollback, func() {
						a.helperOp(T.KernelRollback, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
							return hc.Call(ctx, rpc.MethodKernelRollback, nil, nil)
						}, nil)
					})
				}
			})
		})
		if v.checkErr != "" { // what failed, why in words, 重试 (kubai R-10)
			listRow(c).Children(func() {
				failedRow(c.Key("k-failed"), i18n.F(T.PgCheckFailed, "mihomo"), v.checkErr, a.kernelCheck, d.busy != "")
			})
		}
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
					ui.Text(c, head).FontWeight(500).Grow(1).Wrap().LineHeight(lineText)
					if r.UpdateAvailable {
						rel := *r
						if a.opButton(c.Key("k-install"), iconOK, fmt.Sprintf(T.KernelInstall, r.Version), true, fmt.Sprintf(T.KernelInstall, r.Version)).Disabled(!w).Clicked() {
							body := i18n.F(T.KernelUpdateBody, orDash(k.Current), rel.Version)
							if k.Current == "" {
								body = i18n.F(T.KernelFirstBody, rel.Version)
							}
							a.confirm(fmt.Sprintf(T.KernelInstallTitle, rel.Version), body, T.KernelInstallOK, func() {
								a.kernelInstall(rel)
							})
						}
					}
				})
				pub := ""
				if !r.Published.IsZero() {
					pub = " · " + r.Published.Local().Format("2006-01-02")
				}
				tag := r.Tag // the tag names the release once (R-unslop-26)
				if tag == "" {
					tag = r.Version
				}
				kvMono(c, T.KernelLatest, tag+pub)
				kvMono(c, T.KernelAsset, fmt.Sprintf("%s · %s", r.Asset, status.Bytes(r.Size)))
				if r.Digest == "" {
					kv(c, T.KernelDigest, T.KernelNoDigest)
				} else {
					kvMono(c, T.KernelDigest, r.Digest)
				}
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
		v := &a.hd.kern
		v.release, v.checkErr = nil, ""
		if err == nil {
			v.release = &r
			return
		}
		// The headline says it; the helper's text is behind 复制详情.
		v.checkErr = helperError(err)
		a.hd.msg, a.hd.msgErr = "", false
	})
}

// pendingRestart: the running mihomo is not the current managed kernel
// (installed or rolled back since it started).
func pendingRestart(running, current string) bool {
	if running == "" || current == "" {
		return false
	}
	norm := func(v string) string { return strings.TrimPrefix(strings.Fields(v + " ")[0], "v") }
	return norm(running) != norm(current) && !strings.Contains(running, strings.TrimPrefix(current, "v"))
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

// tailscaleRows are the Tailscale section of 内核 → 组件: its version,
// install kind and path, and the latest release.
func (a *app) tailscaleRows(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.kern
	info := v.ts
	listRow(c).Children(func() {
		componentHead(c, T.Tailscale, "", func() {
			if info == nil && v.tsLoading {
				ui.Text(c, T.Checking).FontSize(plat.calloutSize).TextColor(t.TextMuted).SingleLine()
			}
		})
		if info == nil {
			return
		}
		if info.Error != "" && !info.Installed && info.Version == "" {
			failedRow(c.Key("ts-failed"), i18n.F(T.PgCheckFailed, T.Tailscale), info.Error, a.tailscaleCheck, v.tsLoading)
			return
		}
		if !info.Installed {
			kv(c, T.State, T.TSNotInstalled)
		} else {
			kvMono(c, T.Version, info.Version)
			kv(c, T.Install, T.TSInstallKind(info.InstallKind))
			if info.Path != "" {
				kvMono(c, T.BinaryPath, info.Path)
			}
		}
		if info.Latest != "" {
			kvWith(c, T.KernelLatest, func() {
				col := t.Text
				if info.UpdateAvailable {
					col = t.Accent
				}
				monoText(c, info.Latest).TextColor(col).SingleLine().Shrink(0)
				if info.UpdateAvailable {
					ui.Text(c, T.UpdateAvailable).TextColor(col).SingleLine().Shrink(1)
				}
			})
			if info.UpdateAvailable {
				hintText(c, T.TSUpdateHint(info.InstallKind))
			}
		}
		if info.Error != "" { // the latest release could not be read
			failedRow(c.Key("ts-failed"), i18n.F(T.PgCheckFailed, T.Tailscale), info.Error, a.tailscaleCheck, v.tsLoading)
		}
	})
}

// checkUpdates is the page's 检查更新: mihomo's channel, the GEO
// databases, the external UI and Tailscale, at once, as one operation;
// each section shows its own result or failure.
func (a *app) checkUpdates() {
	T := i18n.T
	ch := kernelChannels[a.hd.kern.channel]
	geo, uik := geoKind(), uiKind()
	var (
		rel        rpc.KernelRelease
		geoR, uiR  rpc.AccessoryCheck
		ts         rpc.TailscaleInfo
		relE, geoE error
		uiE, tsE   error
	)
	a.helperOp(T.KernelCheck, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		var wg sync.WaitGroup
		run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }
		run(func() { relE = hc.Call(ctx, rpc.MethodKernelCheck, rpc.KernelCheckParams{Channel: ch}, &rel) })
		run(func() { geoE = hc.Call(ctx, geo.check, nil, &geoR) })
		run(func() { uiE = hc.Call(ctx, uik.check, nil, &uiR) })
		run(func() { tsE = hc.Call(ctx, rpc.MethodTailscaleInfo, nil, &ts) })
		wg.Wait()
		return nil // each section shows its own failure
	}, func(error) {
		v := &a.hd.kern
		v.release, v.checkErr = nil, ""
		if relE == nil {
			v.release = &rel
		} else {
			v.checkErr = helperError(relE)
		}
		for _, r := range []struct {
			k   accessoryKind
			res *rpc.AccessoryCheck
			err error
		}{{geo, &geoR, geoE}, {uik, &uiR, uiE}} {
			av := r.k.view(a)
			av.checkErr = ""
			if r.err == nil {
				av.check = r.res
			} else {
				av.checkErr = helperError(r.err)
			}
		}
		if tsE != nil {
			ts = rpc.TailscaleInfo{Error: helperError(tsE)}
		}
		v.ts = &ts
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
