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
	// saveErr is the helper's refusal of the last save, shown in the
	// editor (at its line when it has one), not at the top of the page.
	saveErr    *rpc.Msg
	saveErrBuf uint64   // the buffer version it was for
	baseIDs    []string // the stored base layers, in order (reorder sends them first)

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
	a.driftRow(c, s)
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
		if sub.Hash != "" && m.Item(T.SubCopyHash).Chosen() {
			copyText(c, sub.Hash)
		}
		m.Separator()
		if m.Item(T.Remove + "…").Danger().Disabled(!w).Chosen() {
			remove()
		}
	})
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Height(28).Children(func() {
			// The ID is an identifier: in the context menu, not the row (U32).
			ui.Text(c, sub.Name).FontWeight(600).SingleLine().Shrink(1).Tooltip(sub.Name)
			if sub.Active {
				tag(c, T.SubActive)
			}
			ui.Spacer(c)
			never := sub.LastFetch.IsZero()
			if sub.HasURL && a.opButton(c.Key("refresh"), iconRefresh, T.SubRefresh, never, T.SubRefresh+" "+id).Disabled(!w).Clicked() {
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
		// Where the content comes from, and when it last did (U32): an
		// adopted subscription runs on the file takeover used until its
		// first download, which is not "never downloaded".
		num(small(c, src+" · "+interval).SingleLine())
		var fetched string
		switch {
		case !sub.LastFetch.IsZero():
			fetched = fmt.Sprintf(T.SubUpdated, status.Ago(s.Time, sub.LastFetch), status.Bytes(int64(sub.Size)))
			if !sub.LastChange.IsZero() {
				fetched += " · " + fmt.Sprintf(T.SubChanged, status.Ago(s.Time, sub.LastChange))
			}
		case sub.ID == adoptedSubID:
			fetched = T.SubSourceAdoptedBare
			if base := a.adoptedBase(); base != "" {
				fetched = fmt.Sprintf(T.SubSourceAdopted, base)
			}
			fetched += " · " + status.Bytes(int64(sub.Size))
		case sub.Size > 0:
			fetched = status.Bytes(int64(sub.Size))
		default:
			fetched = T.SubNeverFetched
		}
		num(small(c, fetched).SingleLine().Shrink(1).Tooltip(fetched))
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
			Title:   i18n.Cur().SubImportFile,
			Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		b, err := os.ReadFile(paths[0])
		mygo.RunOnMain(func() {
			v := &a.hd.cfg
			if err != nil {
				a.hd.msg, a.hd.msgErr = i18n.Cur().ErrorPrefix+err.Error(), true
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
	// The list is in compose order, grouped by stage (U33): the
	// subscription, base, the user's overrides (the only ones that move),
	// then the managed ones. Compose sorts by stage, so a user layer never
	// goes above the base ones.
	var base, user, managed []rpc.Layer
	for _, l := range d.layers {
		switch {
		case l.Managed:
			managed = append(managed, l)
		case l.Stage == stageBase:
			base = append(base, l)
		default:
			user = append(user, l)
		}
	}
	ids := make([]string, len(user))
	for i, l := range user {
		ids[i] = l.ID
	}
	baseIDs := make([]string, len(base))
	for i, l := range base {
		baseIDs[i] = l.ID
	}
	v.baseIDs = baseIDs
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
		groupLabel(c.Key("g-sub"), T.GroupSubscription)
		ui.Row(c.Key("sub-chain")).Gap(10).AlignItems(ui.Center).Padding(8).MarginX(-8).Children(func() {
			ui.Icon(c, iconConfig).FontSize(iconInline).TextColor(t.TextMuted).Width(14)
			name := a.activeSubscriptionName()
			if name == "" {
				name = T.SubNone
			}
			ui.Text(c, name).FontWeight(600).SingleLine().Shrink(1)
		})
		if len(base) > 0 {
			groupLabel(c.Key("g-base"), T.GroupBase)
			for _, l := range base {
				a.layerRow(c.Key("layer:"+l.ID), l, nil, s, w, -1)
			}
		}
		groupLabel(c.Key("g-user"), T.GroupUser)
		if len(user) == 0 && !v.isNew {
			listRow(c).Children(func() { ui.Text(c, T.LayersNone).TextColor(t.TextMuted) })
		}
		for i, l := range user {
			a.layerRow(c.Key("layer:"+l.ID), l, ids, s, w, i)
		}
		if v.isNew {
			a.newLayerRow(c.Key("layer-new-row"), s, w)
		}
		groupLabel(c.Key("g-managed"), T.GroupManaged)
		for _, l := range managed {
			a.layerRow(c.Key("layer:"+l.ID), l, nil, s, w, -1)
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
			Focusable().Role(ui.RoleDisclosure).Expanded(open).Label(title)
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
					if !l.Managed {
						num(meta(c, " · "+status.Bytes(int64(l.Size))))
					}
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
	if _, ok := a.revertable(l); ok {
		m.Separator()
		if m.Item(T.LayerRevert + "…").Disabled(!w).Chosen() {
			a.revertLayer(l)
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
	all := append(slices.Clone(a.hd.cfg.baseIDs), ids...) // the helper wants every stored layer
	a.helperOp(i18n.T.LayerReorder, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodLayerReorder, rpc.ReorderParams{IDs: all}, nil)
	}, nil)
}

// moveLayerByID moves a stored layer up (-1) or down (1).
func (a *app) moveLayerByID(id string, delta int) {
	var ids []string
	for _, l := range a.hd.layers {
		if !l.Managed && l.Stage != stageBase {
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
				a.hd.msg, a.hd.msgErr = i18n.Cur().ErrorPrefix+helperError(err), true
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
			})
			// The options say what they change (U36). A new override is a
			// user one: 基础 is for what takeover made, and only shows on a
			// layer that already has it.
			ui.Row(c).Gap(12).AlignItems(ui.End).Wrap().Children(func() {
				if !v.isNew && layerStages[v.edStage] == stageBase {
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, T.LayerStage).FontSize(plat.calloutSize).TextColor(t.TextMuted)
						ui.Segmented(c.Key("ed-stage"), &v.edStage, T.StageUserLong, T.StageBaseLong)
					})
				}
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
		blocked := a.lintNotes(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			id := strings.TrimSpace(v.edID)
			// An error-level note means the helper would refuse the save:
			// the button waits, the note says why (U35).
			save := ui.PrimaryButton(c.Key("ed-save"), T.Save).Disabled(!w || id == "" || blocked)
			if blocked {
				save.Tooltip(T.LayerSaveBlocked)
			}
			if save.Clicked() {
				a.saveLayer()
			}
			if v.ed.Buffer.Modified() {
				ui.Text(c, T.Unsaved).FontSize(plat.calloutSize).TextColor(pal(t).warnText)
			}
			ui.Spacer(c)
			if !v.isNew {
				if i := indexLayer(a.hd.layers, v.sel); i >= 0 {
					if _, ok := a.revertable(a.hd.layers[i]); ok && pushButton(c.Key("ed-revert"), T.LayerRevert+"…").Disabled(!w).Clicked() {
						a.revertLayer(a.hd.layers[i])
					}
				}
			}
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

// lintNotes shows the merge-directive notes of the edited layer, and the
// helper's refusal of the last save at its line; it reports whether one
// of them is an error (the save is then held back).
func (a *app) lintNotes(c *ui.Context) (blocked bool) {
	t := c.Theme()
	v := &a.hd.cfg
	key := lintKey{v.ed.Buffer, v.ed.Buffer.Version(), v.edFormat}
	if key != v.lintKey {
		v.lintKey, v.lint = key, nil
		if layerFormats[v.edFormat] == formatMerge {
			v.lint = status.LintOverride(v.ed.Buffer.String())
		}
	}
	notes := v.lint
	if m := v.saveErr; m != nil && v.saveErrBuf == v.ed.Buffer.Version() {
		text, raw := i18n.T.MsgText(*m, false)
		if raw {
			text = m.Text // a refusal duet has no words for: the helper's own, here at the editor
		}
		_, line := layerErrorLine(*m)
		notes = append([]status.LintNote{{Level: status.LevelError, Line: line, Text: text}}, notes...)
	}
	for _, n := range notes {
		if n.Level == status.LevelError {
			blocked = true
		}
	}
	for i, n := range notes {
		if i == 8 {
			ui.Textf(c, "… +%d", len(notes)-8).FontSize(plat.calloutSize).TextColor(t.TextMuted)
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
	return blocked
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
	ver := buf.Version()
	v.saveErr = nil
	if v.isNew { // a new layer is a user one (U36)
		p.Stage = stageUser
	}
	a.helperOp(T.Save+" "+p.ID, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodLayerPut, p, nil)
	}, func(err error) {
		if err != nil {
			// The refusal belongs to the editor, at its line (U35).
			m := errorMsg(helperError(err), nil)
			v.saveErr, v.saveErrBuf = &m, ver
			a.hd.msg, a.hd.msgErr = "", false
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
		if a.opButton(c.Key("preview"), iconPreview, T.Preview, false, T.Preview).Disabled(!s.Helper.Connected() || a.hd.busy != "").Clicked() {
			a.runPreview(s)
		}
	}, func() {
		// What is pending, by name, with 放弃改动 (U39).
		a.pendingRow(c, s, w)
		if p := v.preview; p != nil {
			a.composeSummary(c, *p)
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
			// 说明 belongs to a previewed change (H6): optional, filled
			// from what changed, and only there once there is a preview.
			if v.preview != nil {
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, T.ApplyReason).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					ui.TextInput(c.Key("reason"), &v.reason).Label(T.ApplyReason).Placeholder(T.ReasonOptional)
				})
			} else {
				ui.Spacer(c)
			}
			if a.opButton(c.Key("apply"), iconOK, T.Apply, a.hd.unapplied, T.Apply).Disabled(!w).Clicked() {
				a.runApply()
			}
		})
		if r := v.applied; r != nil {
			a.applySummary(c, *r)
		}
	})
}

// composeChain is the compose order in names (U33): the subscription,
// then each applied layer by its display name.
func (a *app) composeChain(applied []string) string {
	names := []string{}
	if n := a.activeSubscriptionName(); n != "" {
		names = append(names, n)
	}
	for _, id := range applied {
		name := id
		if i := indexLayer(a.hd.layers, id); i >= 0 {
			name = layerTitle(a.hd.layers[i])
		} else if hid, ok := strings.CutPrefix(id, "history:"); ok {
			if e := historyByID(a.hd.history, hid); e != nil {
				name = e.Time.Local().Format("01-02 15:04:05")
			}
		}
		names = append(names, name)
	}
	return strings.Join(names, " → ")
}

// composeSummary shows a compose.preview result.
func (a *app) composeSummary(c *ui.Context, r rpc.ComposeResult) {
	t := c.Theme()
	T := i18n.T
	kvWith(c, T.ComposeOrder, func() {
		ui.Text(c, a.composeChain(r.Applied)).Grow(1).Wrap().LineHeight(lineText)
	})
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
	validateErrors(c, r.ValidateErrors)
	for i, m := range resultMessages(r, nil) {
		msgNote(c.Key(fmt.Sprint("cw", i)), m, true)
	}
}

// validateErrors are mihomo -t's own lines: its output, as a log is.
func validateErrors(c *ui.Context, errs []string) {
	t := c.Theme()
	for _, e := range errs {
		ui.Text(c, e).Font("monospace").FontSize(plat.smallSize).TextColor(pal(t).badText).Wrap().LineHeight(lineText).Selectable()
	}
}

// applySummary shows a compose.apply or history.rollback result (U38):
// the outcome, when, what it did to the network, and the way to its
// history entry. Write/reload flags and the record ID are internals.
func (a *app) applySummary(c *ui.Context, r rpc.ApplyResult) {
	t := c.Theme()
	T := i18n.T
	col, head := t.Success, i18n.F(T.AppliedAt, r.Time.Local().Format("15:04"))
	switch {
	case r.Error != "":
		col, head = t.Danger, T.ApplyFailed
	case !r.Changed && !r.Written:
		col, head = t.TextMuted, T.ApplyUnchanged
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dot(c, col, 8)
		ui.Text(c, head).FontWeight(600)
		if r.Error == "" && r.Impact != nil {
			text := T.ImpactGentle
			if r.Impact.Disruptive {
				text = T.ImpactDisruptive
			}
			small(c, "· "+text)
		}
		ui.Spacer(c)
		if r.HistoryID != "" && a.page != pageHistory && pushButton(c.Key("in-history"), T.ViewInHistory).Clicked() {
			a.showHistoryEntry(r.HistoryID)
		}
	})
	if r.Error == "" {
		impactLines(c, r.Impact)
	}
	for i, m := range resultMessages(r.ComposeResult, r.Impact) {
		msgNote(c.Key(fmt.Sprint("aw", i)), m, true)
	}
	if r.Error != "" {
		// What failed, in words (U53); mihomo -t's own lines under it.
		m := errorMsg(r.Error, r.ErrorMsg)
		if r.RolledBack && m.Code != "reload-failed" && m.Code != "verify-failed" { // those say it themselves
			small(c, T.ApplyFailedRestore)
		}
		msgNote(c.Key("aerr"), m, false)
		validateErrors(c, r.ValidateErrors)
	}
}

// showHistoryEntry opens 历史 with an entry chosen.
func (a *app) showHistoryEntry(id string) {
	v := &a.hd.hist
	v.sel, v.reveal = id, true
	a.openPage(pageHistory)
}

// impactRow says what a mihomo hot reload does (or will do) to the
// network, for a plan or confirm: a few milliseconds of pause, or a short
// interruption and why. Nothing when the helper did not judge it.
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
			impactLines(c, im)
		})
	})
}

// impactLines are the reasons a reload interrupts the network, and what
// only a restart applies, from the impact's fields (U57: no English notes).
func impactLines(c *ui.Context, im *rpc.ReloadImpact) {
	if im == nil {
		return
	}
	T := i18n.T
	if im.Disruptive || im.Unknown {
		for _, l := range T.ImpactLines(im) {
			small(c, l).Wrap()
		}
	}
	if len(im.RestartNeeded) > 0 {
		small(c, T.ImpactRestart+T.LabelSep+strings.Join(im.RestartNeeded, T.ListSep)).Wrap()
	}
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
		if strings.TrimSpace(v.reason) == "" { // H6: the change says what it is
			if last := lastApplied(a.hd.history); last != nil {
				ch, _ := pendingChanges(last.Layers, a.hd.layers, a.hd.settings, a.activeSubscription())
				v.reason = changeReason(ch)
			}
		}
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
	// No words from the user: the helper records the code "apply".
	p := rpc.ApplyParams{Reason: strings.TrimSpace(v.reason)}
	var res rpc.ApplyResult
	a.helperOp(T.Apply, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodComposeApply, p, &res)
	}, func(err error) {
		if err != nil {
			// The failed result travels with the error (U53): show it in
			// the card, in words, instead of the raw line at the top.
			v.applied = failedResult(err)
			if v.applied != nil {
				a.hd.msg, a.hd.msgErr = "", false
			}
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
		// MyGo's own separator gives its expand buttons the pointing
		// hand; ours keep the arrow, as every button (AGENTS.md).
		RenderSeparator: diffSeparator,
	}
}

// diffSeparator draws a diff's folded unchanged lines as MyGo's
// DefaultSeparator does (a bar with chevrons expanding up and down, or
// all of them when few, and "N 行未改动" expanding all; Shift expands all
// from any button), with the arrow cursor and the app's own texts.
func diffSeparator(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
	t := c.Theme()
	T := i18n.T
	ct := s.Theme()
	sep.Padding(4)
	sep.Children(func() {
		bar := ui.Row(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(2).Padding(0, 6).
			Radius(6).Background(ct.Background.Mix(ct.Text, 0.06))
		bar.Children(func() {
			if s.Lines == 0 { // a patch hunk's header
				ui.Text(c, s.Header).Font(s.Font().Family).FontSize(s.Font().Size - 1).TextColor(t.TextMuted).SingleLine().Shrink(1)
				return
			}
			expand := func(fn func()) {
				if c.Modifiers()&ui.Shift != 0 {
					fn = s.ExpandAll
				}
				fn()
			}
			chevron := func(dir int, label string, fn func()) {
				b := ui.ButtonBase(c).Key(label).Size(20, 20).Radius(4).Label(label).Tooltip(label)
				if b.Hovered() {
					b.Background(ct.Text.Alpha(0.08))
				}
				b.Draw(func(p *ui.Painter, r ui.Rect) {
					cx, cy, d := r.X+r.W/2, r.Y+r.H/2, float32(3.5)
					var path ui.Path
					switch dir {
					case 1:
						path.MoveTo(cx-d, cy-d/2).LineTo(cx, cy+d/2).LineTo(cx+d, cy-d/2)
					case -1:
						path.MoveTo(cx-d, cy+d/2).LineTo(cx, cy-d/2).LineTo(cx+d, cy+d/2)
					default:
						path.MoveTo(cx-d, cy-2).LineTo(cx, cy-2-d).LineTo(cx+d, cy-2)
						path.MoveTo(cx-d, cy+2).LineTo(cx, cy+2+d).LineTo(cx+d, cy+2)
					}
					p.StrokePath(&path, 1.5, t.TextMuted)
				})
				if b.Clicked() {
					expand(fn)
				}
			}
			switch {
			case s.First:
				chevron(-1, T.DiffExpandUp, s.ExpandUp)
			case s.Last:
				chevron(1, T.DiffExpandDown, s.ExpandDown)
			case s.Lines <= s.Step:
				chevron(0, T.DiffExpandAll, s.ExpandAll)
			default:
				chevron(1, T.DiffExpandDown, s.ExpandDown)
				chevron(-1, T.DiffExpandUp, s.ExpandUp)
			}
			label := i18n.F(T.DiffUnchanged, s.Lines)
			b := ui.ButtonBase(c).Key("all").Padding(2, 4).Radius(4).Label(T.DiffExpandAll + T.LabelSep + label)
			b.Children(func() { ui.Text(c, label).FontSize(plat.smallSize).TextColor(t.TextMuted) })
			if b.Clicked() {
				s.ExpandAll()
			}
		})
	})
}
