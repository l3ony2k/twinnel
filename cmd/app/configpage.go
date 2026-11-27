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

	// Layer list and its inline editor.
	reveal    bool   // scroll the editor into view in the next frame
	sel       string // open layer ID; "" = none
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
	a.previewCard(c, s)
}

// --- subscriptions ---

func (a *app) subscriptionsCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.cfg
	w := a.helperWrites(s)
	listCard(c, listHead(c, nil, T.Subscriptions, func() {
		if !v.subOpen && pushButton(c.Key("sub-add"), T.SubAdd).Disabled(!w).Clicked() {
			v.subOpen, v.subMins = true, "1440"
		}
	}), func() {
		if a.helperListsLoading(c, 2) {
			return // no false 还没有订阅 before the first load
		}
		if len(d.subs) == 0 {
			listRow(c).Children(func() { ui.Text(c, T.SubNone).TextColor(t.TextMuted) })
		}
		for _, sub := range d.subs {
			a.subscriptionRow(c.Key("sub:"+sub.ID), sub, s, w)
		}
		if v.subOpen {
			listRow(c.Key("sub-form")).Transition(rowMotion).Children(func() { a.subscriptionForm(c, w) })
		}
	})
}

func (a *app) subscriptionRow(c *ui.Context, sub rpc.Subscription, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	id := sub.ID
	refresh := func() {
		a.helperOp(T.SubRefresh+" "+id, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
			var r rpc.SubRefreshResult
			return hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: id}, &r)
		}, nil)
	}
	activate := func() {
		a.helperOp(T.SubActivate+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
			return hc.Call(ctx, rpc.MethodSubActivate, rpc.IDParams{ID: id}, nil)
		}, nil)
	}
	remove := func() {
		a.confirm(fmt.Sprintf(T.SubRemoveTitle, sub.Name), T.SubRemoveBody, T.Remove, func() {
			a.helperOp(T.Remove+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
				return hc.Call(ctx, rpc.MethodSubDelete, rpc.IDParams{ID: id}, nil)
			}, nil)
		})
	}
	row := ui.Column(c).Gap(4).PaddingY(10).Transition(rowMotion).Label(sub.Name)
	row.ContextMenu(func(m *ui.Menu) {
		if sub.HasURL && m.Item(T.SubRefresh).Disabled(!w).Chosen() {
			refresh()
		}
		if m.Item(T.SubActivate).Disabled(!w || sub.Active).Chosen() {
			activate()
		}
		m.Separator()
		if m.Item(T.CopyID).Chosen() {
			copyText(c, id)
		}
		m.Separator()
		if m.Item(T.Remove + "…").Danger().Disabled(!w).Chosen() {
			remove()
		}
	})
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Height(28).Children(func() {
			ui.Text(c, sub.Name).FontWeight(600).SingleLine().Shrink(1).Tooltip(sub.Name)
			ui.Text(c, sub.ID).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
			if sub.Active {
				tag(c, T.SubActive)
			}
			ui.Spacer(c)
			if sub.HasURL && a.opButton(c.Key("refresh"), iconRefresh, T.SubRefresh, false, T.SubRefresh+" "+id).Disabled(!w).Clicked() {
				refresh()
			}
			if !sub.Active && pushButton(c.Key("activate"), T.SubActivate).Disabled(!w).Clicked() {
				activate()
			}
			if pushButton(c.Key("remove"), T.Remove).Disabled(!w).Clicked() {
				remove()
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
		num(small(c, strings.Join(parts, " · ")).SingleLine())
		fetched := T.SubNeverFetched
		if !sub.LastFetch.IsZero() {
			fetched = fmt.Sprintf(T.SubFetched, status.Ago(s.Time, sub.LastFetch))
			if !sub.LastChange.IsZero() {
				fetched += " · " + fmt.Sprintf(T.SubChanged, status.Ago(s.Time, sub.LastChange))
			}
		}
		num(small(c, fetched).SingleLine())
		if sub.LastError != "" {
			errorNote(c, T.ErrorPrefix+sub.LastError)
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
			ui.Text(c, label).FontSize(plat.calloutSize).TextColor(t.TextMuted)
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
		ui.Text(c, T.Or).FontSize(plat.calloutSize).TextColor(t.TextMuted).MarginY(8)
		if pushButton(c.Key("sub-file"), T.SubImportFile).Clicked() {
			a.pickSubscriptionFile()
		}
	})
	if v.subFile != "" {
		ui.Text(c, fmt.Sprintf(T.SubFileChosen, v.subFile, status.Bytes(int64(len(v.subContent))))).FontSize(plat.calloutSize).TextColor(t.TextMuted)
	}
	ui.Text(c, T.SubURLHint).FontSize(plat.smallSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
	ui.Row(c).Gap(8).Children(func() {
		ready := strings.TrimSpace(v.subID) != "" && (strings.TrimSpace(v.subURL) != "" || v.subContent != "")
		if ui.PrimaryButton(c.Key("sub-save"), T.Save).Disabled(!w || !ready).Clicked() {
			a.saveSubscription()
		}
		if pushButton(c.Key("sub-cancel"), T.Cancel).Clicked() {
			v.subOpen, v.subID, v.subName, v.subURL, v.subMins, v.subContent, v.subFile = false, "", "", "", "", "", ""
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
	// subscription.put's warnings (a plain-http URL, …) show until the
	// next operation.
	a.helperOpW(T.SubSave+" "+p.ID, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) ([]string, error) {
		var sub rpc.Subscription
		if err := hc.Call(ctx, rpc.MethodSubPut, p, &sub); err != nil {
			return nil, err
		}
		if fetch && sub.Size == 0 { // fetch right away so the user sees whether the URL works
			var r rpc.SubRefreshResult
			return sub.Warnings, hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: sub.ID}, &r)
		}
		return sub.Warnings, nil
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

// layerDrag is a stored layer's ID being dragged to another place.
type layerDrag string

// layersCard lists the layers in compose order. The stored (user) layers
// reorder by dragging, as macOS lists do (an accent drop line, the rows
// move on drop; review-a 005), by ⌥⌘↑ / ⌥⌘↓ on the open layer, and
// from each row's context menu. The managed layers (coexist, leak guard)
// follow, pinned. A layer opens its editor inline, under its row, with
// a disclosure motion (review-b 015 #1).
func (a *app) layersCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.cfg
	w := a.helperWrites(s)
	var user, managed []rpc.Layer
	for _, l := range d.layers {
		if l.Managed {
			managed = append(managed, l)
		} else {
			user = append(user, l)
		}
	}
	ids := make([]string, len(user))
	for i, l := range user {
		ids[i] = l.ID
	}
	listCard(c, func() {
		ui.Text(c, T.Layers).FontSize(plat.headingSize).FontWeight(plat.headingWeight).Shrink(0)
		ui.Spacer(c)
		if !v.isNew && pushButton(c.Key("layer-new"), T.LayerNew).Disabled(!w).Clicked() {
			a.newLayer()
		}
	}, func() {
		if a.helperListsLoading(c, 2) {
			return
		}
		if len(user) == 0 && !v.isNew {
			listRow(c).Children(func() { ui.Text(c, T.LayersNone).TextColor(t.TextMuted) })
		}
		for i, l := range user {
			a.layerRow(c.Key("layer:"+l.ID), l, ids, s, w, i)
		}
		for _, l := range managed {
			a.layerRow(c.Key("layer:"+l.ID), l, nil, s, w, -1)
		}
		if v.isNew {
			a.newLayerRow(c.Key("layer-new-row"), s, w)
		}
	})
	if i := slices.Index(ids, v.sel); i >= 0 && w && !v.isNew {
		c.OnWindowShortcut(ui.Alt|ui.Cmd, ui.KeyUp, func() { a.moveLayer(ids, i, -1) })
		c.OnWindowShortcut(ui.Alt|ui.Cmd, ui.KeyDown, func() { a.moveLayer(ids, i, 1) })
	}
}

// reorderIDs moves the rows (indices into ids, in order) to before row
// to (len(ids) for the end).
func reorderIDs(ids []string, rows []int, to int) []string {
	moving := map[int]bool{}
	var moved []string
	for _, r := range rows {
		if r >= 0 && r < len(ids) && !moving[r] {
			moving[r] = true
			moved = append(moved, ids[r])
		}
	}
	out := make([]string, 0, len(ids))
	for i := 0; i <= len(ids); i++ {
		if i == to {
			out = append(out, moved...)
		}
		if i < len(ids) && !moving[i] {
			out = append(out, ids[i])
		}
	}
	return out
}

// layerRow is one layer: its head (disclosure, switch, name, meta), and
// its editor under it while it is open. ids are the stored layers' IDs
// in order and at the row's place among them (-1 for a managed layer,
// which neither drags nor takes drops).
func (a *app) layerRow(c *ui.Context, l rpc.Layer, ids []string, s status.Snapshot, w bool, at int) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	open := v.sel == l.ID && !v.isNew
	// Pulled out 8 past the box's padding, so the open row's tint has
	// room around its text (r = 10 − 4 = 6, review-a 004).
	col := ui.Column(c).Transition(rowMotion).Clip().MarginX(-8)
	if at >= 0 && w {
		// A drop on this row puts the dragged layer at its place: the line
		// shows above when it comes from below, under when from above.
		if d, over := ui.DragOver[layerDrag](col); over && string(d) != l.ID {
			from := slices.Index(ids, string(d))
			edge := []float32{2, 0, 0, 0}
			if from >= 0 && from < at {
				edge = []float32{0, 0, 2, 0}
			}
			col.BorderWidth(edge[0], edge[1], edge[2], edge[3]).BorderColor(t.Accent)
		}
		if d, ok := ui.Drop[layerDrag](col); ok && string(d) != l.ID {
			if next, ok := dropLayer(ids, string(d), at); ok {
				a.reorderLayers(next)
			}
		}
	}
	title := layerTitle(l)
	col.Children(func() {
		head := ui.Row(c.Key("head")).Gap(10).AlignItems(ui.Center).Padding(8).MarginY(4).Radius(plat.controlRadius).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleDisclosure).Expanded(open).Label(title)
		switch {
		case open:
			head.Background(pal(t).accentSoft)
		case head.Hovered():
			head.Background(t.Text.Alpha(0.04))
		}
		head.ContextMenu(func(m *ui.Menu) { a.layerMenu(c, m, l, ids, w) })
		if at >= 0 && w && len(ids) > 1 {
			head.Drag(layerDrag(l.ID))
			if head.Dragging() {
				head.Opacity(0.4)
			}
		}
		head.Children(func() {
			disclosure(c, open).Shrink(0)
			on := l.Enabled
			sw := ui.Switch(c.Key("on"), &on).Label(T.LayerEnabled + " " + title)
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
			ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if l.Managed {
						icon := iconManaged
						if l.Stage == "guard" {
							icon = iconLeakGuard
						}
						ui.Icon(c, icon).FontSize(iconInline).TextColor(t.TextMuted).Tooltip(T.LayerManagedFixed)
					}
					ui.Text(c, title).FontWeight(600).SingleLine().Shrink(1).Tooltip(title)
					if stage := T.StageName(l.Stage); stage != title {
						tag(c, stage)
					}
					if l.Managed {
						tag(c, T.LayerManaged)
					}
				})
				// The ID is an identifier (mono, copyable from the menu);
				// the rest is sans meta with even digits (review-a 007-8).
				ui.Row(c).Gap(0).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, l.ID).Font("monospace").FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine().Shrink(1)
					info := " · " + l.Format
					if !l.Managed {
						info += " · " + status.Bytes(int64(l.Size))
					}
					num(meta(c, info))
				})
			})
		})
		if head.Clicked() {
			if open {
				v.sel, v.ed = "", nil
			} else {
				a.selectLayer(l)
			}
		}
		if open {
			a.layerEditor(c.Key("editor"), s, w)
		}
	})
}

// layerMenu is a layer row's context menu: what its row and editor do.
func (a *app) layerMenu(c *ui.Context, m *ui.Menu, l rpc.Layer, ids []string, w bool) {
	T := i18n.T
	v := &a.hd.cfg
	if v.sel == l.ID && !v.isNew {
		if m.Item(T.Close).Chosen() {
			v.sel, v.ed = "", nil
		}
	} else if m.Item(T.LayerOpen).Chosen() {
		a.selectLayer(l)
	}
	toggle := T.LayerEnable
	if l.Enabled {
		toggle = T.LayerDisable
	}
	if m.Item(toggle).Disabled(!w || (l.Managed && l.ID != coexistLayerID)).Chosen() {
		a.setLayerEnabled(l, !l.Enabled)
	}
	if i := slices.Index(ids, l.ID); i >= 0 {
		m.Separator()
		if m.Item(T.MoveUp).Shortcut(ui.Alt|ui.Cmd, ui.KeyUp).Disabled(!w || i == 0).Chosen() {
			a.moveLayer(ids, i, -1)
		}
		if m.Item(T.MoveDown).Shortcut(ui.Alt|ui.Cmd, ui.KeyDown).Disabled(!w || i == len(ids)-1).Chosen() {
			a.moveLayer(ids, i, 1)
		}
	}
	m.Separator()
	if m.Item(T.CopyID).Chosen() {
		copyText(c, l.ID)
	}
	if !l.Managed {
		m.Separator()
		if m.Item(T.Remove + "…").Danger().Disabled(!w).Chosen() {
			a.removeLayer(l.ID, layerTitle(l))
		}
	}
}

// newLayerRow is the row of a layer being made: its title and the editor,
// at the end of the list, coming in as a new row does.
func (a *app) newLayerRow(c *ui.Context, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	ui.Column(c).Transition(rowMotion).Clip().MarginX(-8).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8).MarginY(4).Radius(plat.controlRadius).
			Background(pal(t).accentSoft).Children(func() {
			disclosure(c, true).Shrink(0)
			ui.Text(c, T.LayerNew).FontWeight(600).Grow(1).SingleLine()
			if pushButton(c.Key("ed-cancel"), T.Cancel).Clicked() {
				v.isNew, v.ed = false, nil
			}
		})
		a.layerEditor(c.Key("editor-new"), s, w)
	})
}

// layerTitle is a layer's name in the UI language: a managed layer's or a
// takeover-generated one's from its code (Tailscale 共存, 基础设置, 现有配置的
// 规则：…), the person's own name otherwise.
func layerTitle(l rpc.Layer) string {
	return i18n.T.TakeoverLayerName(l.NameCode, l.NameArgs, l.Name)
}

func (a *app) setLayerEnabled(l rpc.Layer, on bool) {
	T := i18n.T
	label := fmt.Sprintf(T.LayerToggle, layerTitle(l))
	a.helperOp(label, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		if l.ID == coexistLayerID {
			return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{TailscaleCoexist: &on}, nil)
		}
		return hc.Call(ctx, rpc.MethodLayerSetEnabled, rpc.LayerEnableParams{ID: l.ID, Enabled: on}, nil)
	}, nil)
}

func (a *app) moveLayer(ids []string, i, delta int) {
	if next, ok := stepID(ids, i, delta); ok {
		a.reorderLayers(next)
	}
}

// stepID is ids with the one at i moved delta places (⌥⌘↑ -1, ⌥⌘↓ 1),
// and false when it would leave the list (ids are not changed).
func stepID(ids []string, i, delta int) ([]string, bool) {
	j := i + delta
	if i < 0 || i >= len(ids) || j < 0 || j >= len(ids) || delta == 0 {
		return nil, false
	}
	ids = slices.Clone(ids)
	ids[i], ids[j] = ids[j], ids[i]
	return ids, true
}

// dropLayer is the stored layers' order after the layer dragged is
// dropped on the row at index at: it takes that row's place, the line
// drawn above the row when it comes from below and under it when from
// above (layerRow). false: nothing to send (an unknown or the same ID).
func dropLayer(ids []string, dragged string, at int) ([]string, bool) {
	from := slices.Index(ids, dragged)
	if from < 0 || at < 0 || at >= len(ids) || from == at {
		return nil, false
	}
	to := at
	if from < at {
		to = at + 1
	}
	return reorderIDs(ids, []int{from}, to), true
}

// reorderLayers sends the stored layers' new order. The helper's list
// comes back in it.
func (a *app) reorderLayers(ids []string) {
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
	v.edID, v.edName, v.edEnabled = l.ID, layerTitle(l), l.Enabled
	v.edStage = max(0, slices.Index(layerStages, l.Stage))
	v.edFormat = max(0, slices.Index(layerFormats, l.Format))
	v.ed, v.reveal = nil, true
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
	v.reveal = true
}

// layerEditor is the open layer's editor, inline under its row: the
// form, the YAML, lint notes, save and delete. It scrolls into view in
// the frame it opens.
func (a *app) layerEditor(c *ui.Context, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	ed := ui.Column(c).Gap(gapRow).Padding(4, 8, 12)
	if v.reveal {
		v.reveal = false
		ed.ScrollIntoView(ui.ScrollIntoViewOptions{Animation: &rowMove})
	}
	ed.Children(func() {
		if v.managed {
			ui.Text(c, T.LayerManagedHint).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
		} else {
			ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
				ui.Column(c).Gap(4).Basis(0).Grow(1).Children(func() {
					ui.Text(c, T.LayerID).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					in := ui.TextInput(c.Key("ed-id"), &v.edID).Label(T.LayerID).Placeholder("my-rules")
					if !v.isNew {
						in.Disabled(true)
					}
				})
				ui.Column(c).Gap(4).Basis(0).Grow(1).Children(func() {
					ui.Text(c, T.LayerName).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					ui.TextInput(c.Key("ed-name"), &v.edName).Label(T.LayerName)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, T.LayerStage).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					ui.Segmented(c.Key("ed-stage"), &v.edStage, T.StageName(stageUser), T.StageName(stageBase))
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, T.LayerFormat).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					ui.Segmented(c.Key("ed-format"), &v.edFormat, T.FormatMerge, T.FormatVerge)
				})
			})
		}
		if v.ed == nil {
			// Hold the editor's height while it loads, so the page does
			// not jump when the text comes (review-b 017 #6).
			a.loadingBox(c, "layer-editor", 320)
			return
		}
		codeFrame(c, 320, func() { editor.View(c.Key("ed:"+v.sel), v.ed).Grow(1) })
		if v.managed {
			return
		}
		a.lintNotes(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			id := strings.TrimSpace(v.edID)
			if ui.PrimaryButton(c.Key("ed-save"), T.Save).Disabled(!w || id == "").Clicked() {
				a.saveLayer()
			}
			if v.ed.Buffer.Modified() {
				ui.Text(c, T.Unsaved).FontSize(plat.calloutSize).TextColor(pal(t).warnText)
			}
			ui.Spacer(c)
			if !v.isNew && pushButton(c.Key("ed-delete"), T.Remove).Disabled(!w).Clicked() {
				a.removeLayer(id, v.edName)
			}
		})
	})
}

// removeLayer asks, then deletes a stored layer; its content is gone for
// good, which the question says.
func (a *app) removeLayer(id, name string) {
	T := i18n.T
	v := &a.hd.cfg
	a.confirm(fmt.Sprintf(T.LayerRemoveTitle, name), T.LayerRemoveDetail, T.Remove, func() {
		a.helperOp(T.Remove+" "+id, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
			return hc.Call(ctx, rpc.MethodLayerDelete, rpc.IDParams{ID: id}, nil)
		}, func(err error) {
			if err == nil && v.sel == id {
				v.sel, v.ed = "", nil
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
			ui.Textf(c, "… +%d", len(v.lint)-8).FontSize(plat.calloutSize).TextColor(t.TextMuted)
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
			ui.Icon(c, icon).FontSize(iconInline).TextColor(col).MarginY(1)
			text := n.Text
			if n.Line > 0 {
				text = fmt.Sprintf(i18n.T.LintLine, n.Line) + text
			}
			ui.Text(c, text).FontSize(plat.calloutSize).TextColor(col).Grow(1).Wrap().LineHeight(lineText).Selectable()
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
		a.unappliedNote(c)
		ui.Spacer(c)
		if a.opButton(c.Key("preview"), iconPreview, T.Preview, false, T.Preview).Disabled(!s.Helper.Connected() || a.hd.busy != "").Clicked() {
			a.runPreview(s)
		}
	}, func() {
		if p := v.preview; p != nil {
			composeSummary(c, *p)
		}
		if v.diffErr != "" {
			ui.Text(c, v.diffErr).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
		}
		if v.diff != nil {
			add, del := v.diff.Stats()
			ui.Text(c, fmt.Sprintf(T.DiffStats, add, del)).FontSize(plat.calloutSize).TextColor(t.TextMuted)
			if add+del > 0 {
				codeFrame(c, 380, func() { editor.DiffView(c.Key("preview-diff"), v.diff).Grow(1) })
			}
		}
		if v.preview != nil || v.diffErr != "" || v.diff != nil {
			ui.Divider(c) // only between a preview and the apply row
		}
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, T.ApplyReason).FontSize(plat.calloutSize).TextColor(t.TextMuted)
				ui.TextInput(c.Key("reason"), &v.reason).Label(T.ApplyReason).Placeholder(T.ApplyReasonPlaceholder)
			})
			if a.opButton(c.Key("apply"), iconOK, T.Apply, a.hd.unapplied, T.Apply).Disabled(!w).Clicked() {
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
	kvWith(c, T.ComposeValid, func() { ui.Text(c, valid).FontSize(plat.bodySize).TextColor(col) })
	changed := T.No
	if r.Changed {
		changed = T.Yes
	}
	kv(c, T.ComposeChanged, changed)
	for _, e := range r.ValidateErrors {
		ui.Text(c, e).Font("monospace").FontSize(plat.smallSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Selectable()
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
		ui.Text(c, r.Time.Local().Format("15:04:05")+" · "+r.Duration).FontSize(plat.calloutSize).TextColor(t.TextMuted)
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
	ui.Text(c, strings.Join(flags, " · ")).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText).Selectable()
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
				ui.Icon(c, icon).FontSize(iconInline).TextColor(col)
				ui.Text(c, text).TextColor(map[bool]ui.Color{true: col, false: t.Text}[im.Disruptive]).Wrap().LineHeight(lineText)
			})
			if len(im.RestartNeeded) > 0 {
				small(c, T.ImpactRestart+"："+strings.Join(im.RestartNeeded, "、")).Wrap().LineHeight(lineText)
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
