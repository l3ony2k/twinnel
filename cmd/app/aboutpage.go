package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/duet/assets/appicon"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
	"github.com/l3ony2k/duet/core/update"
	"github.com/l3ony2k/duet/core/version"
)

// The 关于 page: the app's version with a manual update check, the
// version of every component, a short getting-started list whose steps
// open the page they need, a few questions, and the project's links.

// aboutView is the page's state. Main thread.
type aboutView struct {
	checking bool
	checked  bool
	res      update.Result
	err      error // the failed check; worded when drawn, in the UI language of the moment (N15)

	cliVersion string // `duet version --short`; "" unknown
	cliLoaded  bool

	faqOpen map[string]bool

	// reveal is a section to scroll to in the next frame (the anchor a
	// link on another page names, e.g. 了解接管); "" none.
	reveal string
}

// aboutTakeoverAnchor is 接管是什么, the target of 了解接管 (接管 page).
const aboutTakeoverAnchor = "takeover"

// aboutFAQAnchor prefixes a question of 常见问题 as a reveal anchor
// (openAboutFAQ).
const aboutFAQAnchor = "faq:"

// openAboutFAQ opens 关于 scrolled to the question q of 常见问题, open:
// an ⓘ's 了解更多 where the FAQ says more.
func (a *app) openAboutFAQ(q string) {
	if a.about.faqOpen == nil {
		a.about.faqOpen = map[string]bool{}
	}
	a.about.faqOpen[q] = true
	a.about.reveal = aboutFAQAnchor + q
	a.showPage(pageAbout)
}

// appIconImage is the app icon, decoded once.
var appIconImage = func() ui.ImageSource {
	img, err := ui.DecodeImage(appicon.MacPNG)
	if err != nil {
		return nil
	}
	return img
}()

// aboutOpened runs when the page is shown: the CLI's version is read once
// (a local command, read-only). The update check is manual only.
func (a *app) aboutOpened() {
	v := &a.about
	if v.cliLoaded {
		return
	}
	v.cliLoaded = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := runCLI(ctx, "version", "--short")
		ver := ""
		if err == nil {
			ver = strings.TrimSpace(firstLine(out))
		}
		mygo.RunOnMain(func() {
			a.about.cliVersion = ver
			a.invalidate()
		})
	}()
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// checkUpdate asks GitHub Releases once (a read; nothing is downloaded).
func (a *app) checkUpdate() {
	v := &a.about
	if v.checking {
		return
	}
	v.checking, v.err = true, nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := update.Check(ctx, update.Options{Proxy: systemProxy()})
		mygo.RunOnMain(func() {
			v := &a.about
			v.checking, v.checked, v.res, v.err = false, true, res, err
			a.invalidate()
		})
	}()
}

// updateFailText is a failed check in the UI language: the reason in
// words when duet knows it, else 检查失败 alone (the system's text is the
// tooltip). Never the system's English, and never a reason worded in
// another language (walkthrough 2, N15).
func updateFailText(err error) string {
	T := i18n.T
	why := ""
	switch {
	case errors.Is(err, update.ErrRateLimited):
		why = T.UpdateRateLimit
	default:
		why = i18n.PlainNetError(status.ShortError(err))
	}
	if why == "" {
		return T.UpdateFailedPlain
	}
	return fmt.Sprintf(T.UpdateFailed, why)
}

func (a *app) aboutPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pageTitle(c, T.PageAbout)
	a.aboutHeader(c)
	a.aboutComponents(c, s)
	a.aboutBasics(c, s)
	a.aboutSteps(c, s)
	a.aboutTakeover(c)
	a.aboutFAQ(c)
	aboutLinks(c)
}

// aboutHeader: the icon, name, version and tagline, and 检查更新 with its
// outcome under it.
func (a *app) aboutHeader(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	v := &a.about
	groupBox(c, t).Padding(16, groupPadX).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			if appIconImage != nil {
				ui.Image(c, appIconImage).Size(64, 64).Shrink(0).Label("duet")
			}
			ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, T.AppName).FontSize(plat.titleSize).FontWeight(plat.titleWeight).SingleLine()
				num(small(c, version.String())).SingleLine().Selectable()
				small(c, T.AboutTagline).Wrap()
			})
			ui.Column(c).Gap(6).AlignItems(ui.End).Shrink(0).Children(func() {
				label := T.CheckUpdate
				if v.err != nil {
					label = T.Retry
				}
				b := pushButton(c.Key("check-update"), "").Label(label).Disabled(v.checking)
				spin := showBusy(c, b, v.checking)
				b.Children(func() {
					if spin {
						spinnerIcon(c)
					}
					ui.Text(c, label).SingleLine()
				})
				if b.Clicked() {
					a.checkUpdate()
				}
				a.updateOutcome(c)
			})
		})
	})
}

// updateOutcome is the line under 检查更新.
func (a *app) updateOutcome(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	v := &a.about
	switch {
	case v.checking:
		small(c, T.UpdateChecking).SingleLine()
	case v.err != nil:
		ui.Text(c, updateFailText(v.err)).FontSize(plat.calloutSize).TextColor(pal(t).badText).
			SingleLine().Shrink(1).Tooltip(status.ShortError(v.err))
	case !v.checked:
	case v.res.State == update.Available:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			num(ui.Text(c, fmt.Sprintf(T.UpdateNewVersion, v.res.Latest)).FontSize(plat.calloutSize)).SingleLine()
			ui.Link(c, T.UpdateDownload, v.res.URL).FontSize(plat.calloutSize)
		})
	case v.res.State == update.NoReleases:
		small(c, T.UpdateNone).SingleLine()
	default:
		small(c, T.UpdateLatest).SingleLine()
	}
}

// aboutComponents: every component's version (core/version, what `duet
// version` reports), from what the app already knows: no extra reads
// besides the CLI's own --short.
func (a *app) aboutComponents(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	rep := componentVersions(s, a.about.cliVersion)
	helper := rep.Helper.Version
	if helper == "" && s.Helper.State != status.HelperConnected && s.Helper.State != status.HelperUnknown {
		helper = s.Helper.State.Text() // 未安装 / 未运行 / 被拒绝
	}
	card(c, T.Components, func() {
		row := func(label, val string) {
			if val == "" {
				val = status.NoDelay // —
			}
			kv(c, label, val)
		}
		row(T.CompApp, version.String())
		row(T.CompCLI, rep.CLI.Version)
		row(T.CompHelper, helper)
		row("mihomo", rep.Mihomo.Version)
		row("Tailscale", rep.Tailscale.Version)
	})
}

// componentVersions fills a version report from the snapshot: the helper's
// status.get, the controller's /version (the monitor's), tailscaled's.
func componentVersions(s status.Snapshot, cli string) version.Report {
	src := version.Sources{}
	if s.Helper.Connected() && s.Helper.Status != nil {
		st := *s.Helper.Status
		src.Helper = func(context.Context) (rpc.Status, error) { return st, nil }
	}
	if s.Mihomo.Reachable && s.Mihomo.Version != "" {
		mv := s.Mihomo.Version
		src.Mihomo = func(context.Context) (mihomo.Version, error) { return mihomo.Version{Version: mv}, nil }
	}
	if s.Tailscale.Reachable && s.Tailscale.State.Version != "" {
		tv := s.Tailscale.State.Version
		src.Tailscale = func(context.Context) (string, error) { return tv, nil }
	}
	return version.Collect(context.Background(), time.Second, version.Entry{Version: cli}, src)
}

// aboutBasics: where the app's settings live and how it runs (moved here
// from 设置, which keeps only settings).
func (a *app) aboutBasics(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	card(c, T.AboutBasics, func() {
		pathRow(c, "prefs", T.AppPrefs, a.cfgPath)
		// The config the running mihomo uses: the helper's own path is
		// duet's default until the takeover.
		if p := a.liveConfigPath(s); p != "" {
			pathRow(c, "mihomo-config", T.MihomoConfigRow, p)
		} else if a.live.loaded || a.shotMode {
			kv(c.Key("mihomo-config"), T.MihomoConfigRow, T.TkNotDetected)
		}
		in := s.Tailscale.Install
		ts := T.InstallKind(string(in.Kind)) + sourceSuffix(in.Source)
		if in.Daemon != "" {
			ts += " · " + in.Daemon
		}
		kv(c, T.Tailscale, ts)
		kv(c, T.AboutPlatform, fmt.Sprintf("%s/%s · %s", runtime.GOOS, runtime.GOARCH, runtime.Version()))
	})
}

// aboutStep is one getting-started step: what to do, whether it is done
// (nil: no way to tell), and the pages that do it.
type aboutStep struct {
	text  string
	done  *bool
	pages []string
}

// aboutStepList is 入门's steps, and the label of each page button: the
// page's sidebar name, or 安装 for the install guide (pageInstall).
func (a *app) aboutStepList(s status.Snapshot) ([]aboutStep, map[string]string) {
	T := i18n.T
	helperOK := s.Helper.Connected()
	setUp := helperOK && a.hd.loaded && len(a.hd.subs) > 0
	_, fails := s.Health.Counts()
	healthy := s.Health.Report != nil && fails == 0
	steps := []aboutStep{
		{T.StepHelper, &helperOK, []string{pageInstall}},
		{T.StepSetup, &setUp, []string{pageTakeover, pageConfig}},
		{T.StepHealth, &healthy, []string{pageHealth}},
		{T.StepDaily, nil, []string{pageProxies, pageTailscale}},
	}
	names := map[string]string{pageInstall: T.StepInstall}
	for _, sec := range navSections(0, 0, toneNeutral) {
		for _, it := range sec.items {
			names[it.id] = it.label
		}
	}
	return steps, names
}

func (a *app) aboutSteps(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	steps, names := a.aboutStepList(s)
	listCard(c, listHead(c, nil, T.GettingStarted, nil), func() {
		for i, st := range steps {
			ui.Row(c.Key(fmt.Sprint("step", i))).Gap(10).AlignItems(ui.Center).PaddingY(8).Children(func() {
				// The step's number, or a check once it is done.
				ui.Box(c).Size(20, 20).Radius(10).Border(1, t.Border).Shrink(0).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					if st.done != nil && *st.done {
						ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.Success)
					} else {
						num(ui.Text(c, fmt.Sprint(i+1)).FontSize(plat.smallSize).TextColor(t.TextMuted))
					}
				})
				ui.Text(c, st.text).Grow(1).Shrink(1).SingleLine()
				for _, p := range st.pages {
					if pushButton(c.Key("go:"+p), names[p]).Clicked() {
						a.showPage(p)
					}
				}
			})
		}
	})
}

// aboutFAQ: a few questions, each opening its one- or two-line answer.
func (a *app) aboutFAQ(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	if a.about.faqOpen == nil {
		a.about.faqOpen = map[string]bool{}
	}
	qs := [][2]string{
		{T.FAQVPNQ, T.FAQVPNA},
		{T.FAQRollbackQ, T.FAQRollbackA},
		{T.FAQQuitQ, T.FAQQuitA},
		{T.FAQLeaveQ, T.FAQLeaveA},
	}
	listCard(c, listHead(c, nil, T.FAQ, nil), func() {
		for _, qa := range qs {
			open := a.about.faqOpen[qa[0]]
			item := ui.Column(c.Key("faq:" + qa[0])).Gap(4).PaddingY(8).Clip().Transition(expandFor(open)).Children(func() {
				head := ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(22).
					Role(ui.RoleDisclosure).Label(qa[0]).Expanded(open).Focusable().FocusRingOffset(2)
				head.Children(func() {
					disclosure(c, open)
					ui.Text(c, qa[0]).Grow(1).Shrink(1).Wrap()
				})
				if head.Clicked() {
					a.about.faqOpen[qa[0]] = !open
				}
				if open {
					ui.Text(c, qa[1]).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).
						MarginStart(22).Selectable()
					if qa[0] == T.FAQLeaveQ && actionLink(c.Key("leave"), T.LeavePage).MarginStart(22).Clicked() {
						a.showPage(pageLeave)
					}
				}
			})
			if a.about.reveal == aboutFAQAnchor+qa[0] {
				a.about.reveal = ""
				item.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter, Animation: &rowMove})
			}
		}
	})
}

// aboutTakeover: 接管是什么, in detail: before and after as a flow, what
// takeover changes on disk, how to change the config afterwards, how to
// undo it, and when not to take over. 了解接管 on the 接管 page scrolls
// here (aboutView.reveal).
func (a *app) aboutTakeover(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	sub := func(title string) {
		ui.Text(c, title).FontWeight(plat.headingWeight).SingleLine().Margin(4, 0, 0, 0)
	}
	para := func(text string) {
		ui.Text(c, text).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
	}
	el := card(c, T.AboutTakeover, func() {
		flowRow(c, "now", T.TkNow, T.AbtTkYourConfig, "mihomo")
		flowRow(c, "after", T.TkAfter, T.AbtTkSub+" + "+T.AbtTkLayers, T.AbtTkComposed, "mihomo")

		sub(T.AbtTkChanges)
		ui.Column(c).Gap(4).Children(func() {
			for i, line := range []string{T.AbtTkBackup, T.AbtTkRewrite, T.AbtTkKeep, T.AbtTkAutoUndo} {
				ui.Row(c.Key(fmt.Sprint("chg", i))).Gap(8).AlignItems(ui.Start).Children(func() {
					// The bullet sits on the first line's middle.
					ui.Box(c).Height(plat.calloutSize * lineText).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
						dot(c, t.TextMuted, 4)
					})
					para(line)
				})
			}
		})

		sub(T.AbtTkEdit)
		para(T.AbtTkEditBody)
		if actionLink(c.Key("go-config"), T.PageConfig).Clicked() {
			a.showPage(pageConfig)
		}

		sub(T.AbtTkUndo)
		para(T.AbtTkUndoBody)
		if actionLink(c.Key("go-leave"), T.LeavePage).Clicked() {
			a.showPage(pageLeave)
		}

		sub(T.AbtTkWhenNot)
		para(T.AbtTkWhenNotBody)
	})
	if a.about.reveal == aboutTakeoverAnchor {
		a.about.reveal = ""
		el.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollStart, Animation: &rowMove})
	}
}

// flowRow is one line of the before/after picture: a label, then the
// steps as quiet chips joined by arrows.
func flowRow(c *ui.Context, key, label string, steps ...string) {
	t := c.Theme()
	kvWith(c.Key(key), label, func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Shrink(1).Children(func() {
			for i, st := range steps {
				if i > 0 {
					ui.Icon(c, iconCollapsed).FontSize(iconInline).TextColor(t.TextMuted).Shrink(0)
				}
				tag(c.Key(fmt.Sprint("step", i)), st)
			}
		})
	})
}

// aboutLinks: the repository, license, notices and issues (real links:
// the pointing hand is theirs).
func aboutLinks(c *ui.Context) {
	T := i18n.T
	card(c, T.Links, func() {
		ui.Row(c).Gap(20).Wrap().Children(func() {
			for _, l := range [][2]string{
				{T.LinkRepo, version.RepoURL()},
				{T.LinkLicense, version.LicenseURL()},
				{T.LinkNotices, version.NoticesURL()},
				{T.LinkIssues, version.IssuesURL()},
			} {
				ui.Link(c.Key(l[1]), l[0], l[1]).Tooltip(l[1])
			}
		})
	})
}

// pathRow is a file row: its name (the path in the tooltip) and 在
// Finder 中显示.
func pathRow(c *ui.Context, key, label, path string) {
	kvWith(c.Key(key), label, func() {
		ui.Text(c, filepath.Base(path)).SingleLine().Shrink(1).Selectable().Tooltip(path)
		ui.Spacer(c)
		if actionLink(c.Key("finder"), i18n.T.ShowInFinder).Clicked() {
			mygo.Shell.ShowItemInFolder(path)
		}
	})
}
