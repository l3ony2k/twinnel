package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// 内核 → 组件: the GEO databases and the web dashboard. Each section reads
// geo.status / ui.status when the page opens (the disk and the last
// check, no network), so it always says what is installed and offers the
// action that fits: 下载 when it is missing, 更新 when a check found a
// newer one, 检查更新 otherwise, 打开控制面板 for an installed dashboard.
// A download shows its stage (accessory.progress); a failed one says why
// and offers 重试 and another route (the subscription's nodes, a proxy),
// as the setup flow does.

type accessoryKind struct {
	kind                  string // rpc.AccessoryGeo or rpc.AccessoryUI
	title, tip, what      string
	icon                  *ui.SVG
	status, check, update string
	view                  func(a *app) *accessoryView
}

func geoKind() accessoryKind {
	return accessoryKind{
		kind: rpc.AccessoryGeo, title: i18n.T.GeoTitle, tip: i18n.T.TipGeo, what: i18n.T.CompGeoWhat, icon: iconGeo,
		status: rpc.MethodGeoStatus, check: rpc.MethodGeoCheck, update: rpc.MethodGeoUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.geo },
	}
}

func uiKind() accessoryKind {
	return accessoryKind{
		kind: rpc.AccessoryUI, title: i18n.T.UITitle, tip: i18n.T.TipUI, what: i18n.T.CompUIWhat, icon: iconDashboard,
		status: rpc.MethodUIStatus, check: rpc.MethodUICheck, update: rpc.MethodUIUpdate,
		view: func(a *app) *accessoryView { return &a.hd.kern.ui },
	}
}

func accessoryKinds() []accessoryKind { return []accessoryKind{geoKind(), uiKind()} }

type accessoryView struct {
	check    *rpc.AccessoryCheck // the last status or check
	checkErr string              // the last check's error (a failed row, raw behind 复制详情)
	loading  bool                // geo.status / ui.status under way
	tooOld   bool                // the helper has no *.status: only 检查更新 works

	res     *rpc.AccessoryUpdateResult // the last download's result
	failErr string                     // the last download's failure (raw)
	prog    *rpc.AccessoryProgress     // the running download's stage
	proxy   string                     // the route of the last download (retry keeps it)

	proxyOpen bool
	proxyAddr string
}

// loadAccessories reads geo.status and ui.status (local reads). Main
// thread.
func (a *app) loadAccessories() {
	if !a.hd.connected || a.hc == nil {
		return
	}
	hc := a.hc
	for _, k := range accessoryKinds() {
		v := k.view(a)
		if v.loading {
			continue
		}
		v.loading = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var r rpc.AccessoryCheck
			err := hc.Call(ctx, k.status, nil, &r)
			mygo.RunOnMain(func() {
				v.loading = false
				switch {
				case err == nil:
					v.check, v.tooOld = &r, false
				case isTooOld(err):
					v.tooOld = true
				}
				a.invalidate()
			})
		}()
	}
}

// accessoryEvent reports whether a batch changes what 组件 shows.
func accessoryEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		switch e.Type {
		case rpc.EventAccessoryChanged, rpc.EventComposeApplied:
			return true
		}
	}
	return false
}

// takeAccessoryProgress keeps the last accessory.progress per kind as the
// running download's stage and returns the other events. Main thread.
func (a *app) takeAccessoryProgress(evs []rpc.Event) []rpc.Event {
	out := evs[:0:0]
	for _, e := range evs {
		if e.Type != rpc.EventAccessoryProgress {
			out = append(out, e)
			continue
		}
		var p rpc.AccessoryProgress
		if json.Unmarshal(e.Data, &p) != nil {
			continue
		}
		for _, k := range accessoryKinds() {
			if k.kind == p.Kind {
				k.view(a).prog = &p
			}
		}
		a.invalidate()
	}
	return out
}

func (k accessoryKind) opUpdate() string { return k.title + " · " + i18n.T.CompUpdate }
func (k accessoryKind) opCheck() string  { return k.title + " · " + i18n.T.CompCheck }

// accessorySummary is what the files of a section add up to.
type accessorySummary struct {
	installed, missing []string
	updates            int   // files to download (missing or newer)
	size               int64 // bytes to download (0: unknown)
	from, to           string
	version, date      string
	source             string // "" when nothing installed came from Twinnel
	foreign            bool   // something installed was not Twinnel's download
	errs               []string
}

func summarize(ck *rpc.AccessoryCheck) accessorySummary {
	var s accessorySummary
	var newest, newestRemote time.Time
	tags := map[string]bool{}
	for _, f := range ck.Files {
		if f.Error != "" {
			s.errs = append(s.errs, f.Error)
		}
		if f.Installed {
			s.installed = append(s.installed, f.Name)
			if f.ModTime.After(newest) {
				newest = f.ModTime
			}
			if f.Version != "" {
				tags[f.Version] = true
			}
			if f.Source != "" {
				s.source = f.Source
			} else {
				s.foreign = true
			}
		} else {
			s.missing = append(s.missing, f.Name)
		}
		if f.UpdateAvailable || !f.Installed {
			s.updates++
			s.size += f.RemoteSize
			if f.RemoteTime.After(newestRemote) {
				newestRemote = f.RemoteTime
			}
			if f.RemoteTag != "" && !isMovingTag(f.RemoteTag) {
				s.to = f.RemoteTag
			}
		}
	}
	if len(tags) == 1 {
		for t := range tags {
			if !isMovingTag(t) {
				s.version = t
			}
		}
	}
	if !newest.IsZero() {
		s.date = newest.Local().Format("2006-01-02")
	}
	s.from = s.version
	if s.from == "" {
		s.from = s.date
	}
	if s.to == "" && !newestRemote.IsZero() {
		s.to = newestRemote.Local().Format("2006-01-02")
	}
	return s
}

// isMovingTag: a tag that names no version (meta-rules-dat's "latest").
func isMovingTag(t string) bool { return strings.EqualFold(t, "latest") }

// sourceName shortens a download URL to what a person recognises: the
// GitHub owner/repo, else the host.
func sourceName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.Host == "github.com" {
		p := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(p) >= 2 {
			return p[0] + "/" + p[1]
		}
	}
	return u.Host
}

// checkedAt words when a check ran: the time today, else the date.
func checkedAt(t time.Time) string {
	l := t.Local()
	if y, m, d := l.Date(); y == time.Now().Year() && m == time.Now().Month() && d == time.Now().Day() {
		return l.Format("15:04")
	}
	return l.Format("2006-01-02 15:04")
}

// accessoryRows are a GEO 数据库 or 控制面板 section of 内核 → 组件.
func (a *app) accessoryRows(c *ui.Context, s status.Snapshot, k accessoryKind) {
	t := c.Theme()
	T := i18n.T
	v := k.view(a)
	ck := v.check
	w := a.helperWrites(s)
	running := a.hd.busy == k.opUpdate()
	var sum accessorySummary
	if ck != nil {
		sum = summarize(ck)
	}
	none := ck != nil && len(sum.installed) == 0
	listRow(c).Children(func() {
		componentHead(c, k.title, k.tip, func() {
			switch {
			case ck == nil && v.loading:
				small(c, T.Loading)
			case ck == nil:
			case none:
				tag(c, T.AccessoryMissing)
			case sum.version != "":
				num(monoText(c, sum.version).FontSize(plat.calloutSize).TextColor(t.TextMuted).SingleLine())
			case sum.date != "":
				num(small(c, sum.date).SingleLine())
			}
		})
		if ck == nil {
			if v.tooOld {
				hintText(c, T.HelperTooOld)
			}
			return
		}
		if none {
			hintText(c, k.what)
		}
		if !none {
			if sum.version != "" || sum.date != "" {
				val := sum.version
				if sum.date != "" {
					val = strings.TrimPrefix(val+" · "+sum.date, " · ")
				}
				kvMono(c, T.CompVersion, val)
			}
			src := T.CompSourceOther
			if sum.source != "" {
				src = sourceName(sum.source)
			}
			kv(c, T.CompSource, src)
			if len(sum.missing) > 0 {
				ui.Text(c, i18n.F(T.CompMissingFiles, strings.Join(sum.missing, ", "))).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Wrap().LineHeight(lineText)
			}
			if k.kind == rpc.AccessoryUI && !ck.Configured {
				ui.Text(c, T.CompUINotServed).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Wrap().LineHeight(lineText)
			}
		}
		if running {
			a.accessoryProgressRow(c, k, v)
		} else {
			a.accessoryActionRow(c, s, k, v, sum, none, w)
		}
	})
	if k.kind == rpc.AccessoryGeo && !none && ck != nil {
		for i, f := range ck.Files {
			listRow(c.Key(fmt.Sprint("f", i))).Children(func() { accessoryFileRow(c, f) })
		}
		listRow(c).Children(func() { a.geoAutoRow(c, s, ck) })
	}
	if v.checkErr != "" && !running {
		listRow(c).Children(func() {
			failedRow(c.Key("check-failed"), i18n.F(T.PgCheckFailed, k.title), v.checkErr, func() { a.accessoryCheck(k) }, a.hd.busy != "")
		})
	}
	if r := v.res; r != nil && !running {
		listRow(c).Children(func() { a.accessoryResult(c, s, k, r) })
	}
	if v.failErr != "" && !running {
		listRow(c).Children(func() { a.accessoryFailed(c, k, v) })
	}
}

// accessoryActionRow is the state line and the one action that fits.
func (a *app) accessoryActionRow(c *ui.Context, s status.Snapshot, k accessoryKind, v *accessoryView, sum accessorySummary, none, w bool) {
	t := c.Theme()
	T := i18n.T
	ck := v.check
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Wrap().Children(func() {
		line, col := "", t.TextMuted
		switch {
		case none:
		case sum.updates > 0 && len(sum.missing) == 0:
			line, col = i18n.F(T.CompNewer, orDash(sum.from), orDash(sum.to)), t.Accent
			if sum.size > 0 {
				line = i18n.F(T.CompNewerSize, orDash(sum.from), orDash(sum.to), status.Bytes(sum.size))
			}
		case len(sum.missing) > 0:
		case ck.Checked && !ck.CheckedAt.IsZero():
			line = i18n.F(T.CompUpToDate, checkedAt(ck.CheckedAt))
		default:
			line = T.CompNotChecked
		}
		if line != "" {
			ui.Text(c, line).FontSize(plat.calloutSize).TextColor(col).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
		} else {
			ui.Spacer(c)
		}
		// 检查更新 for this section alone, unless a download is what fits.
		if !none && len(sum.missing) == 0 && sum.updates == 0 {
			if a.opButton(c.Key("check"), iconRefresh, T.CompCheck, false, k.opCheck()).Disabled(a.hd.busy != "" || !s.Helper.Connected()).Clicked() {
				a.accessoryCheck(k)
			}
		}
		if k.kind == rpc.AccessoryUI && !none && !ck.Configured && sum.updates == 0 {
			if a.opButton(c.Key("enable"), iconOK, T.CompEnableUI, true, k.opUpdate()).Disabled(!w).Clicked() {
				a.accessoryUpdate(k, v.proxy)
			}
		}
		if label := downloadLabel(sum, none); label != "" {
			if a.opButton(c.Key("download"), iconOK, label, true, k.opUpdate()).Disabled(!w).Clicked() {
				a.accessoryUpdate(k, v.proxy)
			}
		}
		if k.kind == rpc.AccessoryUI && !none && ck.Configured {
			if pushButton(c.Key("open"), T.CompOpenUI).Disabled(!s.Mihomo.Reachable).Tooltip(dashTooltip(s)).Clicked() {
				a.openDashboard(ck.Path)
			}
		}
	})
}

func dashTooltip(s status.Snapshot) string {
	if !s.Mihomo.Reachable {
		return i18n.T.CompUIStopped
	}
	return ""
}

// downloadLabel is the download button's label, "" when nothing is to
// download.
func downloadLabel(sum accessorySummary, none bool) string {
	T := i18n.T
	size := ""
	if sum.size > 0 {
		size = status.Bytes(sum.size)
	}
	switch {
	case sum.updates == 0:
		return ""
	case none && size != "":
		return i18n.F(T.CompDownloadSize, size)
	case none:
		return T.CompDownload
	case len(sum.missing) > 0:
		return T.CompDownloadMiss
	case size != "":
		return i18n.F(T.CompUpdateSize, size)
	}
	return T.CompUpdate
}

// accessoryProgressRow says what the running download does.
func (a *app) accessoryProgressRow(c *ui.Context, k accessoryKind, v *accessoryView) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Role(ui.RoleStatus).Children(func() {
		spinnerIcon(c).TextColor(t.TextMuted)
		num(ui.Text(c, progressText(k, v.prog)).FontSize(plat.calloutSize).TextColor(t.TextMuted).Grow(1).Wrap().LineHeight(lineText))
	})
}

// progressText words an accessory.progress.
func progressText(k accessoryKind, p *rpc.AccessoryProgress) string {
	T := i18n.T
	if p == nil {
		return i18n.F(T.CompStageDownloadAny, k.title)
	}
	switch p.Stage {
	case rpc.AccStageVerify:
		return T.CompStageVerify
	case rpc.AccStageInstall:
		return T.CompStageInstall
	case rpc.AccStageReload:
		if p.Kind == rpc.AccessoryUI {
			return T.CompStageConfigure
		}
		return T.CompStageReload
	}
	name := p.Name
	if name == "" || name == "ui" {
		name = k.title
	}
	if p.Total > 0 {
		pct := min(100, int(p.Done*100/p.Total))
		return i18n.F(T.CompStageDownload, pct, name)
	}
	return i18n.F(T.CompStageDownloadAny, name)
}

// accessoryFileRow is one GEO database: its name, date and size, or what
// is wrong with it.
func accessoryFileRow(c *ui.Context, f rpc.AccessoryFile) {
	t := c.Theme()
	T := i18n.T
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		mono(ui.Text(c, filepath.Base(f.File))).SingleLine().Shrink(1).MinWidth(0).TruncateMiddle().Selectable().Tooltip(f.File)
		ui.Spacer(c)
		switch {
		case !f.Installed:
			tag(c, T.AccessoryMissing)
		case f.UpdateAvailable:
			ui.Text(c, T.UpdateAvailable).FontSize(plat.calloutSize).TextColor(t.Accent).SingleLine().Shrink(0)
		}
		if f.Installed {
			info := status.Bytes(f.Size)
			if !f.ModTime.IsZero() {
				info = f.ModTime.Local().Format("2006-01-02") + " · " + info
			}
			num(small(c, info).SingleLine().Shrink(0))
		}
	})
}

// geoAutoRow is the weekly auto-update switch (settings geo_auto_update).
func (a *app) geoAutoRow(c *ui.Context, s status.Snapshot, ck *rpc.AccessoryCheck) {
	t := c.Theme()
	T := i18n.T
	on := ck.AutoUpdate == rpc.GeoAutoWeekly
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(plat.rowMinHeight).Children(func() {
		ui.Text(c, T.CompAutoWeekly).SingleLine().Shrink(0)
		infoTipAbout(c, T.CompAutoWeekly, T.TipGeoAuto, nil)
		ui.Spacer(c)
		if ui.Switch(c.Key("geo-auto"), &on).Label(T.CompAutoWeekly).Disabled(!a.helperWrites(s)).Changed() {
			a.setGeoAuto(on)
		}
	})
	switch {
	case ck.AutoError != "" && !ck.LastAuto.IsZero():
		ui.Text(c, i18n.F(T.CompAutoFailed, checkedAt(ck.LastAuto))).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Wrap().LineHeight(lineText).Tooltip(ck.AutoError)
	case on && !ck.LastAuto.IsZero():
		small(c, i18n.F(T.CompAutoLast, checkedAt(ck.LastAuto)))
	}
}

func (a *app) setGeoAuto(on bool) {
	val := rpc.GeoAutoOff
	if on {
		val = rpc.GeoAutoWeekly
	}
	var r rpc.Settings
	a.helperOp(i18n.T.CompAutoWeekly, 15*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{GeoAutoUpdate: &val}, &r)
	}, func(err error) {
		if err == nil {
			if ck := a.hd.kern.geo.check; ck != nil {
				ck.AutoUpdate = val
			}
		}
	})
}

// accessoryResult is the last download's outcome.
func (a *app) accessoryResult(c *ui.Context, s status.Snapshot, k accessoryKind, r *rpc.AccessoryUpdateResult) {
	t := c.Theme()
	T := i18n.T
	ui.Column(c).Gap(gapSub).Children(func() {
		switch {
		case len(r.Updated) == 0 && !r.Configured:
			small(c, T.CompNothing)
		case k.kind == rpc.AccessoryUI:
			ver := ""
			if len(r.Files) > 0 {
				ver = r.Files[0].Version
			}
			if ver != "" {
				ui.Text(c, i18n.F(T.CompDoneUI, ver)).FontSize(plat.calloutSize).TextColor(pal(t).okText).Wrap().LineHeight(lineText)
			} else {
				ui.Text(c, i18n.F(T.CompDone, k.title)).FontSize(plat.calloutSize).TextColor(pal(t).okText).Wrap().LineHeight(lineText)
			}
		default:
			line := i18n.F(T.CompDone, strings.Join(r.Updated, ", "))
			switch {
			case r.Reloaded:
				line += " · " + T.CompDoneReload
			case !r.ReloadNeeded:
				line += " · " + T.CompDoneNext
			}
			ui.Text(c, line).FontSize(plat.calloutSize).TextColor(pal(t).okText).Wrap().LineHeight(lineText)
		}
		if r.ReloadNeeded && k.kind == rpc.AccessoryGeo {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T.CompReloadLater).FontSize(plat.calloutSize).TextColor(pal(t).warnText).Grow(1).Wrap().LineHeight(lineText).Tooltip(r.ReloadError)
				if a.opButton(c.Key("apply"), iconRefresh, T.AccessoryApply, true, T.AccessoryApply).Disabled(!a.helperWrites(s)).Clicked() {
					a.applyForce(T.AccessoryApplyReason, func() { r.ReloadNeeded = false })
				}
			})
		}
	})
}

// accessoryFailed is a failed download: why in plain words, 重试 (same
// route), and the other routes the setup flow offers.
func (a *app) accessoryFailed(c *ui.Context, k accessoryKind, v *accessoryView) {
	T := i18n.T
	busy := a.hd.busy != ""
	ui.Column(c).Gap(8).Children(func() {
		failedRowWhy(c.Key("dl-failed"), i18n.F(T.CompFailed, k.title), helperErrorText(v.failErr), v.failErr, func() { a.accessoryUpdate(k, v.proxy) }, busy)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if v.proxy != rpc.ProxySubscription && pushButton(c.Key("via-sub"), T.CompViaSub).Disabled(busy).Clicked() {
				a.accessoryUpdate(k, rpc.ProxySubscription)
			}
			if pushButton(c.Key("via-proxy"), T.CompViaProxy).Disabled(busy).Clicked() {
				v.proxyOpen = !v.proxyOpen
			}
			infoTipAbout(c, T.CompViaSub, T.CompViaHint, nil)
		})
		if v.proxyOpen {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("proxy"), &v.proxyAddr).Label(T.SetupProxyAddr).Placeholder("http://192.168.1.2:7890").Grow(1)
				if pushButton(c.Key("proxy-use"), T.SetupProxyUse).Disabled(busy || strings.TrimSpace(v.proxyAddr) == "").Clicked() {
					v.proxyOpen = false
					a.accessoryUpdate(k, strings.TrimSpace(v.proxyAddr))
				}
			})
		}
	})
}

// helperErrorText is helperError's wording for a raw message kept as
// text (a failure from an earlier frame).
func helperErrorText(raw string) string {
	return i18n.PlainNetError(raw)
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

// accessoryCheck asks GitHub for one section (geo.check / ui.check).
func (a *app) accessoryCheck(k accessoryKind) {
	var r rpc.AccessoryCheck
	a.helperOp(k.opCheck(), 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, k.check, rpc.AccessoryCheckParams{Proxy: k.view(a).proxy}, &r)
	}, func(err error) {
		v := k.view(a)
		v.checkErr = ""
		if err == nil {
			v.check = &r
			v.res, v.failErr = nil, ""
			return
		}
		// The failed row says it; the helper's text is behind 复制详情.
		v.checkErr = helperError(err)
		a.hd.msg, a.hd.msgErr = "", false
	})
}

// accessoryUpdate downloads (or updates) a section through route proxy;
// GEO databases are reloaded into mihomo at once.
func (a *app) accessoryUpdate(k accessoryKind, proxy string) {
	v := k.view(a)
	v.prog, v.res, v.failErr, v.proxy = nil, nil, "", proxy
	var r rpc.AccessoryUpdateResult
	a.helperOp(k.opUpdate(), 15*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, k.update, rpc.AccessoryUpdateParams{Proxy: proxy, Reload: k.kind == rpc.AccessoryGeo}, &r)
	}, func(err error) {
		v.prog = nil
		if len(r.Files) > 0 && v.check != nil {
			v.check.Files = r.Files
		}
		if err != nil {
			v.failErr = rawError(err)
			a.hd.msg, a.hd.msgErr = "", false // the section says it
			a.loadAccessories()
			return
		}
		v.res = &r
		a.loadAccessories()
	})
}

// rawError is the helper's own message (behind 复制详情).
func rawError(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}

// openDashboard opens the dashboard mihomo serves under its controller.
// The controller secret never goes into the URL (it would stay in the
// browser's history): the URL names the controller, and the secret is
// put on the clipboard for the dashboard's sign-in.
func (a *app) openDashboard(path string) {
	if a.mh == nil {
		return
	}
	u := dashboardSetupURL(a.mh.BaseURL(), path)
	if sec := controllerConfig(a.cfg, a.creds.cur).Secret; sec != "" && !a.shotMode {
		if err := mygo.Clipboard.Write(transfer.TextData(sec)); err == nil {
			a.toast = i18n.T.CompSecretCopied
		}
	}
	a.openExternal(u)
}

// dashboardSetupURL is the dashboard's setup page for the controller at
// base: hostname and port prefilled, no secret.
func dashboardSetupURL(base, path string) string {
	if path == "" {
		path = "/ui/"
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return dashboardURL(base)
	}
	host, port := b.Hostname(), b.Port()
	if port == "" {
		port = "80"
	}
	q := url.Values{"hostname": {host}, "port": {port}}
	return strings.TrimSuffix(base, "/") + path + "#/setup?" + q.Encode()
}

// componentHead is the first line of a 组件 section: its name (and ⓘ),
// then what is known of it.
func componentHead(c *ui.Context, title, tip string, rest func()) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).Children(func() {
		ui.Text(c, title).FontWeight(500).SingleLine().Shrink(0)
		if tip != "" {
			infoTipAbout(c, title, tip, nil)
		}
		ui.Spacer(c)
		if rest != nil {
			rest()
		}
	})
}
