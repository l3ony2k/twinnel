package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/status"
)

// The 订阅与覆写 page: subscriptions, the layer list, the layer editor,
// preview (compose --preview + diff against the live config) and apply.

const (
	stageUser = "user"
	stageBase = "base"

	formatMerge = "merge"
	formatVerge = "verge"

	coexistLayerID = "tailscale-coexist" // managed; toggled by settings.update
)

var (
	layerStages  = []string{stageUser, stageBase}
	layerFormats = []string{formatMerge, formatVerge}
)

type configView struct {
	// Subscription form. The URL is only held while typing: after saving
	// it is cleared and never shown again (the helper keeps it).
	subOpen                         bool
	subID, subName, subURL, subMins string
	subContent, subFile             string

	// Layer editor.
	sel       string // selected layer ID; "" = none
	isNew     bool
	managed   bool
	loading   bool
	edID      string
	edName    string
	edStage   int
	edFormat  int
	edEnabled bool
	ed        *editor.State
	lint      []status.LintNote
	lintKey   lintKey // what lint was computed for

	// Preview and apply.
	reason  string
	preview *rpc.ComposeResult
	applied *rpc.ApplyResult
	diff    *editor.DiffState
	diffErr string
}

type lintKey struct {
	buf    *editor.Buffer
	ver    uint64
	format int
}

func yamlBuffer(text string) *editor.Buffer {
	b := editor.NewBuffer(text)
	b.SetLanguage(editor.LanguageFor("config.yaml"))
	return b
}

func (a *app) configPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pageTitle(c, T.PageConfig)
	if !a.helperGate(c, s) {
		return
	}
	a.opNotes(c, s)
	a.subscriptionsCard(c, s)
	a.layersCard(c, s)
	if a.hd.cfg.sel != "" || a.hd.cfg.isNew {
		a.layerEditorCard(c, s)
	}
	a.previewCard(c, s)
}

// --- subscriptions ---

func (a *app) subscriptionsCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.cfg
	w := a.helperWrites(s)
	headedCard(c, func() {
		ui.Icon(c, iconSubscription).FontSize(15).TextColor(t.TextMuted)
		ui.Text(c, T.Subscriptions).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		ui.Spacer(c)
		if !v.subOpen && ui.Button(c.Key("sub-add"), T.SubAdd).Disabled(!w).Clicked() {
			v.subOpen, v.subMins = true, "1440"
		}
	}, func() {
		if len(d.subs) == 0 {
			ui.Text(c, T.SubNone).FontSize(13).TextColor(t.TextMuted)
		}
		for _, sub := range d.subs {
			a.subscriptionRow(c.Key("sub:"+sub.ID), sub, s, w)
		}
		if v.subOpen {
			ui.Divider(c)
			a.subscriptionForm(c, w)
		}
	})
}

func (a *app) subscriptionRow(c *ui.Context, sub rpc.Subscription, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	ui.Column(c).Gap(4).Padding(10, 12).Radius(plat.controlRadius+2).Background(pal(t).code).Border(1, pal(t).groupLine).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, sub.Name).FontWeight(600).SingleLine().Shrink(1)
			ui.Text(c, sub.ID).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine()
			if sub.Active {
				tag(c, T.SubActive)
			}
			ui.Spacer(c)
			id := sub.ID
			if sub.HasURL && iconButton(c.Key("refresh"), iconRefresh, T.SubRefresh, false).Disabled(!w).Clicked() {
				a.helperOp(T.SubRefresh+" "+id, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
					var r rpc.SubRefreshResult
					return hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: id}, &r)
				}, nil)
			}
			if !sub.Active && ui.Button(c.Key("activate"), T.SubActivate).Disabled(!w).Clicked() {
				a.helperOp(T.SubActivate+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
					return hc.Call(ctx, rpc.MethodSubActivate, rpc.IDParams{ID: id}, nil)
				}, nil)
			}
			if ui.Button(c.Key("remove"), T.Remove).Disabled(!w).Clicked() {
				a.confirm(fmt.Sprintf(T.SubRemoveTitle, sub.Name), T.SubRemoveBody, T.Remove, func() {
					a.helperOp(T.Remove+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
						return hc.Call(ctx, rpc.MethodSubDelete, rpc.IDParams{ID: id}, nil)
					}, nil)
				})
			}
		})
		src := T.SubFromFile
		if sub.HasURL {
			src = T.SubFromURL // the URL itself never leaves the helper
		}
		interval := T.SubManual
		if sub.IntervalMinutes > 0 {
			interval = fmt.Sprintf(T.SubEvery, sub.IntervalMinutes)
		}
		parts := []string{src, interval, status.Bytes(int64(sub.Size))}
		if sub.Hash != "" {
			parts = append(parts, shortHash(sub.Hash))
		}
		ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(t.TextMuted).SingleLine()
		fetched := T.SubNeverFetched
		if !sub.LastFetch.IsZero() {
			fetched = fmt.Sprintf(T.SubFetched, status.Ago(s.Time, sub.LastFetch))
			if !sub.LastChange.IsZero() {
				fetched += " · " + fmt.Sprintf(T.SubChanged, status.Ago(s.Time, sub.LastChange))
			}
		}
		ui.Text(c, fetched).FontSize(12).TextColor(t.TextMuted).SingleLine()
		if sub.LastError != "" {
			ui.Text(c, T.ErrorPrefix+sub.LastError).FontSize(12).TextColor(pal(t).badText).Wrap().Selectable()
		}
	})
}

func (a *app) subscriptionForm(c *ui.Context, w bool) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	field := func(key, label string, val *string, placeholder string) ui.Element {
		var in ui.Element
		ui.Column(c).Gap(4).Basis(0).Grow(1).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
			in = ui.TextInput(c.Key(key), val).Label(label).Placeholder(placeholder)
		})
		return in
	}
	ui.Row(c).Gap(12).Children(func() {
		field("sub-id", T.SubID, &v.subID, "main")
		field("sub-name", T.SubName, &v.subName, T.SubNamePlaceholder)
		field("sub-mins", T.SubInterval, &v.subMins, "1440")
	})
	ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
		field("sub-url", T.SubURL, &v.subURL, "https://…")
		ui.Text(c, T.Or).FontSize(12).TextColor(t.TextMuted).MarginY(8)
		if ui.Button(c.Key("sub-file"), T.SubImportFile).Clicked() {
			a.pickSubscriptionFile()
		}
	})
	if v.subFile != "" {
		ui.Text(c, fmt.Sprintf(T.SubFileChosen, v.subFile, status.Bytes(int64(len(v.subContent))))).FontSize(12).TextColor(t.TextMuted)
	}
	ui.Text(c, T.SubURLHint).FontSize(11).TextColor(t.TextMuted).Wrap()
	ui.Row(c).Gap(8).Children(func() {
		ready := strings.TrimSpace(v.subID) != "" && (strings.TrimSpace(v.subURL) != "" || v.subContent != "")
		if ui.PrimaryButton(c.Key("sub-save"), T.Save).Disabled(!w || !ready).Clicked() {
			a.saveSubscription()
		}
		if ui.Button(c.Key("sub-cancel"), T.Cancel).Clicked() {
			*v = configView{sel: v.sel, isNew: v.isNew, managed: v.managed, edID: v.edID, edName: v.edName,
				edStage: v.edStage, edFormat: v.edFormat, edEnabled: v.edEnabled, ed: v.ed, reason: v.reason,
				preview: v.preview, applied: v.applied, diff: v.diff, diffErr: v.diffErr}
		}
	})
}

func (a *app) pickSubscriptionFile() {
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  parent,
			Title:   i18n.T.SubImportFile,
			Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		b, err := os.ReadFile(paths[0])
		mygo.RunOnMain(func() {
			v := &a.hd.cfg
			if err != nil {
				a.hd.msg, a.hd.msgErr = i18n.T.ErrorPrefix+err.Error(), true
				return
			}
			v.subFile, v.subContent = filepath.Base(paths[0]), string(b)
			a.invalidate()
		})
	}()
}

func (a *app) saveSubscription() {
	T := i18n.T
	v := &a.hd.cfg
	p := rpc.SubPutParams{ID: strings.TrimSpace(v.subID), Name: strings.TrimSpace(v.subName),
		URL: strings.TrimSpace(v.subURL), Content: v.subContent}
	if m, err := strconv.Atoi(strings.TrimSpace(v.subMins)); err == nil && m >= 0 {
		p.IntervalMinutes = &m
	}
	fetch := p.URL != "" && p.Content == ""
	// Clear the form at once: the URL is not kept in the app.
	v.subOpen, v.subID, v.subName, v.subURL, v.subMins, v.subContent, v.subFile = false, "", "", "", "", "", ""
	a.helperOp(T.SubSave+" "+p.ID, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		var sub rpc.Subscription
		if err := hc.Call(ctx, rpc.MethodSubPut, p, &sub); err != nil {
			return err
		}
		if fetch && sub.Size == 0 { // fetch right away so the user sees whether the URL works
			var r rpc.SubRefreshResult
			return hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: sub.ID}, &r)
		}
		return nil
	}, nil)
}

func shortHash(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// --- layers ---

func (a *app) layersCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	w := a.helperWrites(s)
	stored := []string{}
	for _, l := range d.layers {
		if !l.Managed {
			stored = append(stored, l.ID)
		}
	}
	headedCard(c, func() {
		ui.Icon(c, iconLayer).FontSize(15).TextColor(t.TextMuted)
		ui.Text(c, T.Layers).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		ui.Text(c, T.LayersHint).FontSize(12).TextColor(t.TextMuted).Shrink(1).SingleLine()
		ui.Spacer(c)
		if ui.Button(c.Key("layer-new"), T.LayerNew).Disabled(!w).Clicked() {
			a.newLayer()
		}
	}, func() {
		for _, l := range d.layers {
			a.layerRow(c.Key("layer:"+l.ID), l, stored, w)
		}
	})
}

func (a *app) layerRow(c *ui.Context, l rpc.Layer, stored []string, w bool) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	selected := v.sel == l.ID
	border := t.Border
	if selected {
		border = t.Accent
	}
	row := ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8, 12).Radius(8).Background(t.Background).Border(1, border).Cursor(ui.CursorPointer)
	row.Children(func() {
		on := l.Enabled
		sw := ui.Switch(c.Key("on"), &on).Label(T.LayerEnabled)
		switch {
		case l.ID == coexistLayerID:
			sw.Disabled(!w)
		case l.Managed: // leak guard: on while groups are set (settings)
			sw.Disabled(true)
		default:
			sw.Disabled(!w)
		}
		if sw.Changed() {
			a.setLayerEnabled(l, on)
		}
		ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if l.Managed {
					icon := iconManaged
					if l.Stage == "guard" {
						icon = iconLeakGuard
					}
					ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
				}
				ui.Text(c, l.Name).FontWeight(600).SingleLine().Shrink(1)
				tag(c, T.StageName(l.Stage))
				if l.Managed {
					tag(c, T.LayerManaged)
				}
			})
			info := l.ID + " · " + l.Format
			if !l.Managed {
				info += " · " + status.Bytes(int64(l.Size))
			}
			ui.Text(c, info).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		if !l.Managed {
			i := slices.Index(stored, l.ID)
			if ui.Button(c.Key("up"), "↑").Label(T.MoveUp).Disabled(!w || i <= 0).Clicked() {
				a.moveLayer(stored, i, -1)
			}
			if ui.Button(c.Key("down"), "↓").Label(T.MoveDown).Disabled(!w || i < 0 || i >= len(stored)-1).Clicked() {
				a.moveLayer(stored, i, 1)
			}
		}
	})
	if row.Clicked() && !selected {
		a.selectLayer(l)
	}
}

func (a *app) setLayerEnabled(l rpc.Layer, on bool) {
	T := i18n.T
	label := fmt.Sprintf(T.LayerToggle, l.Name)
	a.helperOp(label, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		if l.ID == coexistLayerID {
			return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{TailscaleCoexist: &on}, nil)
		}
		return hc.Call(ctx, rpc.MethodLayerSetEnabled, rpc.LayerEnableParams{ID: l.ID, Enabled: on}, nil)
	}, nil)
}

func (a *app) moveLayer(ids []string, i, delta int) {
	j := i + delta
	if i < 0 || j < 0 || j >= len(ids) {
		return
	}
	ids = slices.Clone(ids)
	ids[i], ids[j] = ids[j], ids[i]
	a.helperOp(i18n.T.LayerReorder, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodLayerReorder, rpc.ReorderParams{IDs: ids}, nil)
	}, nil)
}

// moveLayerByID moves a stored layer up (-1) or down (1).
func (a *app) moveLayerByID(id string, delta int) {
	var ids []string
	for _, l := range a.hd.layers {
		if !l.Managed {
			ids = append(ids, l.ID)
		}
	}
	a.moveLayer(ids, slices.Index(ids, id), delta)
}

// selectLayer loads a layer into the editor.
func (a *app) selectLayer(l rpc.Layer) {
	v := &a.hd.cfg
	v.sel, v.isNew, v.managed, v.loading = l.ID, false, l.Managed, true
	v.edID, v.edName, v.edEnabled = l.ID, l.Name, l.Enabled
	v.edStage = max(0, slices.Index(layerStages, l.Stage))
	v.edFormat = max(0, slices.Index(layerFormats, l.Format))
	v.ed = nil
	a.loadLayerContent(l.ID)
}

func (a *app) loadLayerContent(id string) {
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var lc rpc.LayerContent
		err := hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: id}, &lc)
		mygo.RunOnMain(func() {
			v := &a.hd.cfg
			if v.sel != id {
				return
			}
			v.loading = false
			if err != nil {
				a.hd.msg, a.hd.msgErr = i18n.T.ErrorPrefix+helperError(err), true
				a.invalidate()
				return
			}
			if v.ed != nil && v.ed.Buffer.Modified() {
				return // keep the user's unsaved edits
			}
			v.ed = &editor.State{Buffer: yamlBuffer(lc.Content), ReadOnly: lc.Managed, Label: lc.Name}
			a.invalidate()
		})
	}()
}

// layersLoaded keeps the editor in step with a reloaded layer list.
func (a *app) layersLoaded() {
	v := &a.hd.cfg
	if v.sel == "" || v.isNew {
		return
	}
	i := slices.IndexFunc(a.hd.layers, func(l rpc.Layer) bool { return l.ID == v.sel })
	if i < 0 {
		v.sel, v.ed = "", nil // deleted
		return
	}
	l := a.hd.layers[i]
	v.edEnabled = l.Enabled
	if l.Managed { // generated: follows the tailnet and settings
		a.loadLayerContent(l.ID)
	}
}

func (a *app) newLayer() {
	v := &a.hd.cfg
	v.sel, v.isNew, v.managed, v.loading = "", true, false, false
	v.edID, v.edName, v.edStage, v.edFormat, v.edEnabled = "", "", 0, 0, true
	v.ed = &editor.State{Buffer: yamlBuffer(i18n.T.LayerTemplate)}
}

func (a *app) layerEditorCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	w := a.helperWrites(s)
	headedCard(c, func() {
		title := T.LayerEdit
		switch {
		case v.isNew:
			title = T.LayerNew
		case v.managed:
			title = T.LayerGenerated
		}
		ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		if !v.isNew {
			ui.Text(c, v.edID).Font("monospace").FontSize(12).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if ui.Button(c.Key("ed-close"), T.Close).Clicked() {
			v.sel, v.isNew, v.ed = "", false, nil
		}
	}, func() {
		if v.managed {
			ui.Text(c, T.LayerManagedHint).FontSize(12).TextColor(t.TextMuted).Wrap()
		} else {
			ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
				ui.Column(c).Gap(4).Basis(0).Grow(1).Children(func() {
					ui.Text(c, T.LayerID).FontSize(12).TextColor(t.TextMuted)
					in := ui.TextInput(c.Key("ed-id"), &v.edID).Label(T.LayerID).Placeholder("my-rules")
					if !v.isNew {
						in.Disabled(true)
					}
				})
				ui.Column(c).Gap(4).Basis(0).Grow(1).Children(func() {
					ui.Text(c, T.LayerName).FontSize(12).TextColor(t.TextMuted)
					ui.TextInput(c.Key("ed-name"), &v.edName).Label(T.LayerName)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, T.LayerStage).FontSize(12).TextColor(t.TextMuted)
					ui.Segmented(c.Key("ed-stage"), &v.edStage, T.StageName(stageUser), T.StageName(stageBase))
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, T.LayerFormat).FontSize(12).TextColor(t.TextMuted)
					ui.Segmented(c.Key("ed-format"), &v.edFormat, T.FormatMerge, T.FormatVerge)
				})
			})
		}
		if v.ed == nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, T.Loading).FontSize(12).TextColor(t.TextMuted)
			})
			return
		}
		editor.View(c.Key("ed:"+v.sel), v.ed).Height(320).Radius(plat.controlRadius+2).Border(1, pal(t).groupLine)
		if !v.managed {
			a.lintNotes(c)
		}
		if v.managed {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			id := strings.TrimSpace(v.edID)
			if ui.PrimaryButton(c.Key("ed-save"), T.Save).Disabled(!w || id == "").Clicked() {
				a.saveLayer()
			}
			if v.ed.Buffer.Modified() {
				ui.Text(c, T.Unsaved).FontSize(12).TextColor(pal(t).warnText)
			}
			ui.Spacer(c)
			if !v.isNew && ui.Button(c.Key("ed-delete"), T.Remove).Disabled(!w).Clicked() {
				name := v.edName
				a.confirm(fmt.Sprintf(T.LayerRemoveTitle, name), T.LayerRemoveBody, T.Remove, func() {
					a.helperOp(T.Remove+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
						return hc.Call(ctx, rpc.MethodLayerDelete, rpc.IDParams{ID: id}, nil)
					}, func(err error) {
						if err == nil {
							v.sel, v.ed = "", nil
						}
					})
				})
			}
		})
	})
}

// lintNotes shows the merge-directive warnings of the edited layer.
func (a *app) lintNotes(c *ui.Context) {
	t := c.Theme()
	v := &a.hd.cfg
	key := lintKey{v.ed.Buffer, v.ed.Buffer.Version(), v.edFormat}
	if key != v.lintKey {
		v.lintKey, v.lint = key, nil
		if layerFormats[v.edFormat] == formatMerge {
			v.lint = status.LintOverride(v.ed.Buffer.String())
		}
	}
	for i, n := range v.lint {
		if i == 8 {
			ui.Textf(c, "… +%d", len(v.lint)-8).FontSize(12).TextColor(t.TextMuted)
			break
		}
		col, icon := t.TextMuted, iconSkip
		switch n.Level {
		case status.LevelWarn:
			col, icon = t.Warning, iconWarning
		case status.LevelError:
			col, icon = t.Danger, iconError
		}
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(col).MarginY(1)
			text := n.Text
			if n.Line > 0 {
				text = fmt.Sprintf(i18n.T.LintLine, n.Line) + text
			}
			ui.Text(c, text).FontSize(12).TextColor(col).Grow(1).Wrap().Selectable()
		})
	}
}

func (a *app) saveLayer() {
	T := i18n.T
	v := &a.hd.cfg
	p := rpc.LayerPutParams{
		ID:      strings.TrimSpace(v.edID),
		Name:    strings.TrimSpace(v.edName),
		Stage:   layerStages[v.edStage],
		Format:  layerFormats[v.edFormat],
		Content: v.ed.Buffer.String(),
	}
	if v.isNew {
		on := true
		p.Enabled = &on
	}
	buf := v.ed.Buffer
	a.helperOp(T.Save+" "+p.ID, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodLayerPut, p, nil)
	}, func(err error) {
		if err != nil {
			return
		}
		buf.MarkSaved()
		if v.isNew {
			v.isNew, v.sel = false, p.ID
		}
	})
}

// --- preview and apply ---

func (a *app) previewCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	w := a.helperWrites(s)
	headedCard(c, func() {
		ui.Text(c, T.PreviewApply).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
		ui.Spacer(c)
		if iconButton(c.Key("preview"), iconSpeed, T.Preview, false).Disabled(!s.Helper.Connected() || a.hd.busy != "").Clicked() {
			a.runPreview(s)
		}
	}, func() {
		ui.Text(c, T.PreviewHint).FontSize(12).TextColor(t.TextMuted).Wrap()
		if p := v.preview; p != nil {
			composeSummary(c, *p)
		}
		if v.diffErr != "" {
			ui.Text(c, v.diffErr).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		if v.diff != nil {
			add, del := v.diff.Stats()
			ui.Text(c, fmt.Sprintf(T.DiffStats, add, del)).FontSize(12).TextColor(t.TextMuted)
			if add+del > 0 {
				editor.DiffView(c.Key("preview-diff"), v.diff).Height(380).Radius(plat.controlRadius+2).Border(1, pal(t).groupLine)
			}
		}
		ui.Divider(c)
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, T.ApplyReason).FontSize(12).TextColor(t.TextMuted)
				ui.TextInput(c.Key("reason"), &v.reason).Label(T.ApplyReason).Placeholder(T.ApplyReasonPlaceholder)
			})
			if iconButton(c.Key("apply"), iconOK, T.Apply, true).Disabled(!w).Clicked() {
				a.runApply()
			}
		})
		if r := v.applied; r != nil {
			applySummary(c, *r)
		}
	})
}

// composeSummary shows a compose.preview result.
func composeSummary(c *ui.Context, r rpc.ComposeResult) {
	t := c.Theme()
	T := i18n.T
	kv(c, T.ComposeLayers, strings.Join(r.Applied, " → "))
	kv(c, T.ComposeHash, fmt.Sprintf("%s · %s", shortHash(r.Hash), status.Bytes(int64(r.Size))))
	valid, col := T.ValidOK, t.Success
	if !r.Valid {
		valid, col = T.ValidFail, t.Danger
	}
	kvWith(c, T.ComposeValid, func() { ui.Text(c, valid).FontSize(13).TextColor(col) })
	changed := T.No
	if r.Changed {
		changed = T.Yes
	}
	kv(c, T.ComposeChanged, changed)
	for _, e := range r.ValidateErrors {
		ui.Text(c, e).Font("monospace").FontSize(11).TextColor(pal(t).badText).Wrap().Selectable()
	}
	for _, wn := range r.Warnings {
		warningNote(c, wn)
	}
}

// applySummary shows a compose.apply or history.rollback result.
func applySummary(c *ui.Context, r rpc.ApplyResult) {
	t := c.Theme()
	T := i18n.T
	col, head := t.Success, T.ApplyDone
	switch {
	case r.Error != "":
		col, head = t.Danger, T.ApplyFailed
	case !r.Changed:
		col, head = t.TextMuted, T.ApplyUnchanged
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, head).FontWeight(600)
		ui.Text(c, r.Time.Local().Format("15:04:05")+" · "+r.Duration).FontSize(12).TextColor(t.TextMuted)
	})
	flags := []string{
		fmt.Sprintf("%s %s", T.ApplyWritten, yesNo(r.Written)),
		fmt.Sprintf("%s %s", T.ApplyReloaded, yesNo(r.Reloaded)),
	}
	if r.RolledBack {
		flags = append(flags, T.ApplyRolledBack)
	}
	if r.HistoryID != "" {
		flags = append(flags, T.HistoryEntry+" "+r.HistoryID)
	}
	ui.Text(c, strings.Join(flags, " · ")).FontSize(12).TextColor(t.TextMuted).Wrap().Selectable()
	impactRow(c, r.Impact)
	for _, w := range r.Warnings {
		if k := i18n.WarningKind(w); r.Impact != nil && (k == "reload-disrupts" || k == "restart-needed") {
			continue // the impact row says it
		}
		warningNote(c, w)
	}
	if r.Error != "" {
		errorNote(c, r.Error)
	}
}

// impactRow says what a mihomo hot reload does (or did) to the network:
// a few milliseconds of pause, or a short interruption, plus what only a
// restart applies. Nothing when the helper did not judge it.
func impactRow(c *ui.Context, im *rpc.ReloadImpact) {
	if im == nil {
		return
	}
	t := c.Theme()
	T := i18n.T
	text, icon, col := T.ImpactGentle, iconOK, t.TextMuted
	if im.Disruptive {
		text, icon, col = T.ImpactDisruptive, iconWarning, pal(t).warnText
	}
	kvWith(c, T.ImpactLabel, func() {
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).FontSize(14).TextColor(col)
				ui.Text(c, text).TextColor(map[bool]ui.Color{true: col, false: t.Text}[im.Disruptive]).Wrap()
			})
			if len(im.RestartNeeded) > 0 {
				small(c, T.ImpactRestart+"："+strings.Join(im.RestartNeeded, "、")).Wrap()
			}
			if im.Disruptive {
				for _, n := range im.Notes {
					small(c, n).Wrap().Selectable()
				}
			}
		})
	})
}

func yesNo(b bool) string {
	if b {
		return i18n.T.Yes
	}
	return i18n.T.No
}

// runPreview runs compose.preview, then loads the live and the composed
// text for the diff.
func (a *app) runPreview(s status.Snapshot) {
	T := i18n.T
	cfgPath := ""
	if st := s.Helper.Status; st != nil {
		cfgPath = st.Mihomo.ConfigPath
	}
	var res rpc.ComposeResult
	var live, next string
	var diffErr error
	a.helperOp(T.Preview, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		if err := hc.Call(ctx, rpc.MethodComposePreview, nil, &res); err != nil {
			return err
		}
		live, diffErr = fetchConfig(ctx, hc, cfgPath, rpc.ConfigLive, "")
		if diffErr == nil {
			next, diffErr = fetchConfig(ctx, hc, cfgPath, rpc.ConfigPreview, "")
		}
		return nil
	}, func(err error) {
		v := &a.hd.cfg
		v.diff, v.diffErr = nil, ""
		if err != nil {
			v.preview = nil
			return
		}
		v.preview = &res
		if diffErr != nil {
			v.diffErr = fmt.Sprintf(T.DiffUnavailable, helperError(diffErr))
			return
		}
		v.diff = newDiff(live, next)
	})
}

func (a *app) runApply() {
	T := i18n.T
	v := &a.hd.cfg
	p := rpc.ApplyParams{Reason: strings.TrimSpace(v.reason)}
	if p.Reason == "" {
		p.Reason = T.ApplyReasonDefault
	}
	var res rpc.ApplyResult
	a.helperOp(T.Apply, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodComposeApply, p, &res)
	}, func(err error) {
		if err != nil {
			v.applied = nil
			return
		}
		v.applied, v.reason = &res, ""
		v.preview, v.diff, v.diffErr = nil, nil, ""
	})
}

// newDiff is a read-only unified diff of two YAML texts.
func newDiff(old, new string) *editor.DiffState {
	return &editor.DiffState{
		Old:    yamlBuffer(old),
		New:    yamlBuffer(new),
		Layout: editor.DiffUnified,
		Wrap:   true,
	}
}
