package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/status"
)

const (
	pageOverview  = "overview"
	pageProxies   = "proxies"
	pageTailscale = "tailscale"
	pageHealth    = "health"
	pageLogs      = "logs"
	pageSettings  = "settings"
)

func (a *app) mainView(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.win.Close()
		return
	}
	s := a.mon.Snapshot()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(200).PaddingY(16).Background(t.Surface).Shrink(0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 20, 10).Children(func() {
				dot(c, levelColor(t, s.Level), 10)
				ui.Text(c, T.AppName).FontSize(15).Bold()
			})
			old := a.page
			if ui.Sidebar(c, &a.page, func() {
				ui.SidebarItem(c, pageOverview, iconOverview, T.PageOverview)
				ui.SidebarItem(c, pageProxies, iconProxies, T.PageProxies)
				ui.SidebarItem(c, pageTailscale, iconTailscale, T.PageTailscale).Children(func() {
					if s.Tailscale.Reachable {
						ui.Badge(c, fmt.Sprint(s.Tailscale.State.OnlinePeers()))
					}
				})
				ui.SidebarItem(c, pageHealth, iconHealth, T.PageHealth)
				ui.SidebarItem(c, pageLogs, iconLogs, T.PageLogs)
				ui.SidebarItem(c, pageSettings, iconSettings, T.PageSettings)
			}).Grow(1).Label(T.AppName).Changed() {
				a.pageChanged(old)
			}
			ui.Text(c, s.Level.Text()).FontSize(12).TextColor(t.TextMuted).Padding(0, 20)
		})
		ui.Column(c).Grow(1).Children(func() {
			switch a.page {
			case pageLogs:
				a.logsPage(c) // has its own scrolling list
			default:
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).Padding(24).Gap(16).Children(func() {
						switch a.page {
						case pageOverview:
							a.overviewPage(c, s)
						case pageProxies:
							a.proxiesPage(c, s)
						case pageTailscale:
							a.tailscalePage(c, s)
						case pageHealth:
							a.healthPage(c)
						case pageSettings:
							a.settingsPage(c, s)
						}
					})
				})
			}
		})
	})
}

func (a *app) pageChanged(old string) {
	if a.page == pageLogs {
		a.mon.SetLogLevel(a.logs.levelName())
	} else if old == pageLogs {
		a.mon.SetLogLevel("")
	}
	if a.page == pageSettings {
		a.settings.load(a.cfg)
	}
}

func pageTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(22).Bold()
}

// --- overview ---

func (a *app) overviewPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	pageTitle(c, T.PageOverview)
	card(c, "", func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			dot(c, levelColor(t, s.Level), 12)
			ui.Text(c, s.Level.Text()).FontSize(16).Bold()
		})
		for _, r := range s.Reasons {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				dot(c, levelColor(t, r.Level), 6).MarginY(5)
				ui.Text(c, r.Text).FontSize(13).Grow(1).Wrap()
			})
		}
	})
	ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
		card(c, T.Mihomo, func() { a.mihomoSummary(c, s) }).Basis(0).Grow(1)
		card(c, T.Tailscale, func() { a.tailscaleSummary(c, s) }).Basis(0).Grow(1)
	})
	card(c, T.Traffic, func() { a.trafficChart(c, s) })
}

func (a *app) mihomoSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	m := s.Mihomo
	if !m.Reachable {
		kv(c, T.State, T.Unreachable)
		if m.Err != "" {
			ui.Text(c, m.Err).FontSize(12).TextColor(t.Danger).Wrap()
		}
		kv(c, T.Controller, a.mh.BaseURL())
		return
	}
	kv(c, T.State, T.Running)
	kv(c, T.Version, m.Version)
	kv(c, T.Mode, T.ModeName(string(m.Config.Mode)))
	tun := T.TunOff
	if m.Config.Tun.Enable {
		tun = fmt.Sprintf("%s · %s · %s", T.TunOn, m.Config.Tun.Device, m.Config.Tun.Stack)
	}
	kv(c, T.Tun, tun)
	if p := m.Primary; p != nil {
		kvWith(c, p.Name, func() {
			ui.Text(c, p.Node).FontSize(13).SingleLine().Shrink(1)
			ui.Text(c, status.DelayText(p.Delay, p.Tested)).FontSize(12).TextColor(delayColor(t, p.Delay, p.Tested)).Shrink(0)
		})
		if len(p.Chain) > 1 {
			kv(c, T.Exit, strings.Join(p.Chain, " → "))
		}
	}
	kv(c, T.Connections, fmt.Sprint(m.Connections))
	kv(c, T.Total, fmt.Sprintf("↓ %s  ↑ %s", status.Bytes(m.Traffic.DownTotal), status.Bytes(m.Traffic.UpTotal)))
}

func (a *app) tailscaleSummary(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	ts := s.Tailscale
	st := ts.State
	if !ts.Reachable {
		kv(c, T.State, T.Unreachable)
		if ts.Err != "" {
			ui.Text(c, ts.Err).FontSize(12).TextColor(t.Danger).Wrap()
		}
		kv(c, T.Install, T.InstallKind(string(ts.Install.Kind)))
		return
	}
	kv(c, T.State, T.TSState(st.BackendState))
	kv(c, T.SelfName, st.Self.Name)
	if ip := st.Self.IPv4(); ip.IsValid() {
		kv(c, T.SelfIP, ip.String())
	}
	kv(c, T.OnlinePeers, fmt.Sprintf("%d / %d", st.OnlinePeers(), len(st.Peers)))
	exit := T.NoExitNode
	if st.ExitNode != nil {
		exit = st.ExitNode.Name
	}
	kv(c, T.ExitNode, exit)
	kv(c, T.MagicDNS, onOff(st.Prefs.MagicDNS))
	if st.KeyExpiry != nil {
		kv(c, T.KeyExpiry, st.KeyExpiry.Local().Format("2006-01-02")+" ("+status.Until(s.Time, *st.KeyExpiry)+")")
	}
	kv(c, T.Install, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source))
}

func sourceSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}

func onOff(b bool) string {
	if b {
		return i18n.T.On
	}
	return i18n.T.Off
}

// trafficChart draws the last two minutes of download and upload rates.
func (a *app) trafficChart(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	hist := a.mon.TrafficHistory()
	ui.Row(c).Gap(16).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			dot(c, t.Accent, 8)
			ui.Text(c, T.Download+" "+status.Rate(s.Mihomo.Traffic.Down)).FontSize(12)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			dot(c, t.Success, 8)
			ui.Text(c, T.Upload+" "+status.Rate(s.Mihomo.Traffic.Up)).FontSize(12)
		})
	})
	const n = 120
	var hi float64 = 1024
	for _, h := range hist {
		hi = max(hi, float64(h.Down), float64(h.Up))
	}
	y := chart.NewLinear(0, hi).Nice(3)
	ticks := y.Ticks(3)
	for i := range ticks {
		ticks[i].Label = status.Rate(int64(ticks[i].Value))
	}
	xs := make([]float32, len(hist))
	down := make([]float32, len(hist))
	up := make([]float32, len(hist))
	for i, h := range hist {
		xs[i] = float32(n-len(hist)+i) / float32(n-1)
		down[i] = y.At(float64(h.Down))
		up[i] = y.At(float64(h.Up))
	}
	axes := chart.Axes{Left: ticks, Label: func(_ chart.Side, tk chart.Tick) {
		ui.Text(c, tk.Label).FontSize(11).TextColor(t.TextMuted).NoWrap()
	}}
	chart.Plot(c, axes, func(area ui.Element) {
		area.Draw(func(p *ui.Painter, r ui.Rect) {
			for _, tk := range ticks {
				p.Line(r.X, chart.Y(r, tk.At), r.X+r.W, chart.Y(r, tk.At), 1, t.Border)
			}
			if len(xs) < 2 {
				return
			}
			p.FillPath(chart.Area(r, xs, nil, down, chart.CurveMonotone), t.Accent.Alpha(0.15))
			p.StrokePath(chart.Line(r, xs, down, chart.CurveMonotone), 2, t.Accent)
			p.StrokePath(chart.Line(r, xs, up, chart.CurveMonotone), 1.5, t.Success)
		})
	}).Label(T.Traffic).Height(160).Gap(8)
}

// --- proxies ---

func (a *app) proxiesPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	pageTitle(c, T.PageProxies)
	m := s.Mihomo
	if !m.Reachable {
		ui.Text(c, T.Unreachable+" "+m.Err).TextColor(t.Danger)
		return
	}
	if len(m.Groups) == 0 {
		ui.Text(c, T.NoGroups).TextColor(t.TextMuted)
	}
	if a.actionErr != "" {
		ui.Text(c, a.actionErr).FontSize(12).TextColor(t.Danger)
	}
	if len(a.groupsOpen) == 0 && m.Primary != nil {
		a.groupsOpen[m.Primary.Name] = true
	}
	for _, g := range m.Groups {
		a.groupCard(c.Key("group:"+g.Name), c, g, s.WritesAllowed)
	}
}

func (a *app) groupCard(_ *ui.Context, c *ui.Context, g status.Group, writes bool) {
	t := c.Theme()
	T := i18n.T
	open := a.groupsOpen[g.Name]
	ui.Column(c.Key("group:"+g.Name)).Padding(14).Gap(12).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
		head := ui.Row(c).Gap(10).AlignItems(ui.Center)
		head.Children(func() {
			arrow := "▸"
			if open {
				arrow = "▾"
			}
			ui.Text(c, arrow).FontSize(12).TextColor(t.TextMuted).Width(10)
			ui.Text(c, g.Name).FontSize(14).Bold().SingleLine()
			ui.Badge(c, g.Type)
			ui.Text(c, strings.Join(g.Chain, " → ")).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1)
			ui.Spacer(c)
			ui.Text(c, status.DelayText(g.Delay, g.Tested)).FontSize(12).TextColor(delayColor(t, g.Delay, g.Tested)).Shrink(0)
			testing := a.mon.Testing(g.Name)
			label := T.TestGroupDelay
			if testing {
				label = T.TestingDelay
			}
			if ui.Button(c.Key("test"), label).Disabled(testing).Clicked() {
				name, mon := g.Name, a.mon
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_ = mon.TestGroup(ctx, name)
				}()
				a.groupsOpen[g.Name] = true
			}
		})
		if head.Clicked() {
			a.groupsOpen[g.Name] = !open
		}
		if !open {
			return
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, mem := range g.Members {
				selected := mem.Name == g.Now
				border := t.Border
				if selected {
					border = t.Accent
				}
				chip := ui.Column(c.Key(mem.Name)).Width(168).Padding(8, 10).Gap(4).Radius(8).
					Background(t.Background).Border(1.5, border)
				chip.Children(func() {
					ui.Text(c, mem.Name).FontSize(12).SingleLine()
					ui.Row(c).Gap(6).Children(func() {
						typ := mem.Type
						if typ == "" {
							typ = "?"
						}
						ui.Text(c, typ).FontSize(11).TextColor(t.TextMuted).Grow(1).SingleLine()
						ui.Text(c, status.DelayText(mem.Delay, mem.Tested)).FontSize(11).TextColor(delayColor(t, mem.Delay, mem.Tested))
					})
				})
				canSelect := writes && g.Selectable() && !selected
				if canSelect {
					chip.Cursor(ui.CursorPointer)
				}
				if chip.Clicked() && canSelect {
					group, name := g.Name, mem.Name
					a.do(func(ctx context.Context) error { return a.mh.SelectProxy(ctx, group, name) })
				}
			}
		})
	})
}

// --- tailscale ---

func (a *app) tailscalePage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	pageTitle(c, T.PageTailscale)
	ts := s.Tailscale
	if !ts.Reachable {
		ui.Text(c, T.Unreachable+" "+ts.Err).TextColor(t.Danger)
		return
	}
	st := ts.State
	card(c, "", func() {
		ui.Row(c).Gap(24).Children(func() {
			ui.Column(c).Gap(8).Basis(0).Grow(1).Children(func() {
				kv(c, T.State, T.TSState(st.BackendState))
				kv(c, T.Account, st.User)
				kv(c, T.Version, st.Version)
			})
			ui.Column(c).Gap(8).Basis(0).Grow(1).Children(func() {
				kv(c, T.MagicDNS, onOff(st.Prefs.MagicDNS))
				kv(c, T.ShieldsUp, onOff(st.Prefs.ShieldsUp))
				kv(c, T.Install, T.InstallKind(string(ts.Install.Kind))+sourceSuffix(ts.Install.Source))
			})
		})
	})
	if len(st.Peers) == 0 {
		ui.Text(c, T.NoPeers).TextColor(t.TextMuted)
		return
	}
	peers := st.Peers
	a.peers.RowHeight = 32
	cols := []ui.TableColumn{
		{Title: T.PeerName, Width: 180},
		{Title: T.PeerIP, Width: 160},
		{Title: T.PeerOS, Width: 80},
		{Title: T.PeerPath, Width: 110},
		{Title: T.PeerRelay, Width: 70},
		{Title: T.PeerLastSeen},
	}
	ui.Table(c, &a.peers, cols, len(peers), func(row, col int) {
		p := peers[row]
		switch col {
		case 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				col := t.TextMuted
				if p.Online {
					col = t.Success
				}
				dot(c, col, 8)
				ui.Text(c, p.Name).SingleLine()
			})
		case 1:
			ip := ""
			if v4 := p.IPv4(); v4.IsValid() {
				ip = v4.String()
			}
			ui.Text(c, ip).Font("monospace").FontSize(12).SingleLine().Selectable()
		case 2:
			ui.Text(c, p.OS).SingleLine()
		case 3:
			col := t.TextMuted
			switch p.Path() {
			case "direct":
				col = t.Success
			case "derp", "peer-relay":
				col = t.Warning
			}
			ui.Text(c, T.Path(p.Path())).TextColor(col).SingleLine()
		case 4:
			ui.Text(c, p.Relay).SingleLine()
		case 5:
			seen := T.Online
			if !p.Online {
				seen = T.Offline
				if !p.LastSeen.IsZero() {
					seen = status.Ago(s.Time, p.LastSeen)
				}
			}
			ui.Text(c, seen).SingleLine()
		}
	}).Height(float32(min(len(peers), 14)*32 + 40))
}

// --- health ---

func (a *app) healthPage(c *ui.Context) {
	T := i18n.T
	pageTitle(c, T.PageHealth)
	card(c, "", func() {
		ui.Text(c, T.HealthPlaceholder).TextColor(c.Theme().TextMuted).Wrap()
	})
}

// --- logs ---

var logLevels = []string{"debug", "info", "warning", "error"}

type logsView struct {
	level  int // index into logLevels; zero value means "info" via init
	inited bool
	paused bool
	frozen []mihomo.LogEntry
	list   ui.ListState
}

func (l *logsView) levelName() string {
	if !l.inited {
		l.level, l.inited = 1, true
	}
	return logLevels[l.level]
}

func (a *app) logsPage(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	a.logs.levelName()
	entries := a.mon.Logs()
	if a.logs.paused {
		entries = a.logs.frozen
	}
	ui.Column(c).Grow(1).Padding(24).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			pageTitle(c, T.PageLogs)
			ui.Spacer(c)
			ui.Text(c, T.LogLevel).FontSize(12).TextColor(t.TextMuted)
			if ui.Segmented(c.Key("level"), &a.logs.level, logLevels...).Changed() {
				a.mon.ClearLogs()
				a.mon.SetLogLevel(a.logs.levelName())
			}
			pause := T.LogPause
			if a.logs.paused {
				pause = T.LogResume
			}
			if ui.Button(c.Key("pause"), pause).Clicked() {
				a.logs.paused = !a.logs.paused
				a.logs.frozen = a.mon.Logs()
			}
			if ui.Button(c.Key("clear"), T.LogClear).Clicked() {
				a.mon.ClearLogs()
				a.logs.frozen = nil
			}
		})
		if len(entries) == 0 {
			ui.Text(c, T.LogEmpty).TextColor(t.TextMuted)
			return
		}
		ui.List(c, &a.logs.list, len(entries), func(i int) {
			e := entries[i]
			col := t.Text
			switch e.Type {
			case "warning":
				col = t.Warning
			case "error":
				col = t.Danger
			case "debug":
				col = t.TextMuted
			}
			ui.Row(c).Gap(10).PaddingX(4).AlignItems(ui.Start).Children(func() {
				ui.Text(c, e.Time.Format("15:04:05")).Font("monospace").FontSize(11).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, e.Type).Font("monospace").FontSize(11).TextColor(col).Width(56).Shrink(0)
				ui.Text(c, e.Payload).Font("monospace").FontSize(11).Grow(1).Wrap().Selectable()
			})
		}).Grow(1).Border(1, t.Border).Radius(8)
	})
}

// --- settings ---

type settingsForm struct {
	controller, secret, primary, testURL string
	mihomoBinary, mihomoConfigDir        string
	msg                                  string
	failed                               bool
}

func (f *settingsForm) load(c status.Config) {
	*f = settingsForm{
		controller: c.Controller, primary: c.PrimaryGroup, testURL: c.TestURL,
		mihomoBinary: c.MihomoBinary, mihomoConfigDir: c.MihomoConfigDir,
	}
	if !c.SecretFromEnv {
		f.secret = c.Secret
	}
}

func (a *app) settingsPage(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	f := &a.settings
	pageTitle(c, T.PageSettings)
	field := func(key, label, hint string, v *string, password bool) {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
			in := ui.TextInput(c.Key(key), v).Label(label)
			if password {
				in.Password()
			}
			if hint != "" {
				ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
			}
		})
	}
	card(c, T.Mihomo, func() {
		field("controller", T.Controller, "", &f.controller, false)
		secretHint := T.SecretHint
		if a.cfg.SecretFromEnv {
			secretHint = "DUET_MIHOMO_SECRET ✓"
		}
		field("secret", T.Secret, secretHint, &f.secret, true)
		field("primary", T.PrimaryGroup, "", &f.primary, false)
		field("binary", T.MihomoBinary, "", &f.mihomoBinary, false)
		field("confdir", T.MihomoConfigDir, "", &f.mihomoConfigDir, false)
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c.Key("save"), T.Save).Clicked() {
				a.saveSettings()
			}
			if f.msg != "" {
				col := t.Success
				if f.failed {
					col = t.Danger
				}
				ui.Text(c, f.msg).FontSize(12).TextColor(col)
			}
			ui.Spacer(c)
			ui.Text(c, T.SettingsRestartHint).FontSize(11).TextColor(t.TextMuted)
		})
	})
	card(c, T.Tailscale, func() {
		in := s.Tailscale.Install
		kv(c, T.Install, T.InstallKind(string(in.Kind))+sourceSuffix(in.Source))
		if in.Daemon != "" {
			kv(c, "tailscaled", in.Daemon)
		}
	})
	card(c, T.AppName, func() {
		kv(c, T.ConfigFile, a.cfgPath)
		kv(c, T.AllowWrites, onOff(a.cfg.AllowWrites))
	})
}

func (a *app) saveSettings() {
	f := &a.settings
	cfg := a.cfg
	cfg.Controller = strings.TrimSpace(f.controller)
	cfg.PrimaryGroup = strings.TrimSpace(f.primary)
	cfg.TestURL = strings.TrimSpace(f.testURL)
	cfg.MihomoBinary = strings.TrimSpace(f.mihomoBinary)
	cfg.MihomoConfigDir = strings.TrimSpace(f.mihomoConfigDir)
	if !cfg.SecretFromEnv {
		cfg.Secret = f.secret
	}
	if cfg.PrimaryGroup == "" {
		cfg.PrimaryGroup = status.DefaultPrimaryGroup
	}
	if _, err := mihomo.New(mihomo.Options{Address: cfg.Controller}); err != nil {
		f.msg, f.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		f.msg, f.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	a.cfg = cfg
	f.msg, f.failed = i18n.T.Saved, false
	a.groupsOpen = map[string]bool{}
	a.start()
}
