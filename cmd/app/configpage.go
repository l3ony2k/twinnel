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

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// The 订阅与覆写 page: subscriptions, the layer list, the layer editor,
// preview (compose --preview + diff against the live config) and apply.

const (
	stageUser = "user"
	stageBase = "base"

	formatMerge = "merge"
	formatVerge = "verge"

	coexistLayerID   = "tailscale-coexist" // managed; toggled by settings.update
	leakGuardLayerID = "leak-guard"        // managed
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
	edIDAuto  bool // a new override's ID follows its name (until edited under 高级)
	edAdv     bool // 高级 is open
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

	imp importView // 导入配置 (importsheet.go)

	// Managed layers' views.
	showGen bool // the open managed layer shows its generated YAML
	// What each stored layer does, by content hash (layersummary.go).
	sums        map[string]layerSummary
	sumsLoading map[string]bool
	guardLast   []string // leak guard's groups before it was switched off (this session)

	// Preview and apply.
	reason  string
	preview *rpc.ComposeResult
	prevFor string // the pending state the preview was made for
	applied *rpc.ApplyResult
	diff    *editor.DiffState
	diffErr string
	prevErr string // the last preview's failure, shown in the apply card

	// What each layer row marks, made once a frame (rowMarks): changed
	// since the running config, or named by the last failed apply.
	pendingIDs, failedIDs map[string]bool

	// undo puts back what the last reversible action changed, while its
	// toast shows (undo.go); toastUndo is that toast, waiting for the
	// page's next frame.
	undo      *undoState
	toastUndo *undoToast
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

// codeThemes caches codeTheme by the muted color it was made with.
var codeThemes = map[ui.Color]*editor.Theme{}

// codeTheme is the theme of every editor and diff view: MyGo's, with
// YAML's punctuation (a list's "-", a key's ":", "|", ">") in the muted
// text color instead of red. In a diff red and green mean removed and
// added, nothing else (kubai R-3). Set it on each frame: the appearance
// can change while a view is open.
func codeTheme(t *ui.Theme) *editor.Theme {
	if ct, ok := codeThemes[t.TextMuted]; ok {
		return ct
	}
	ct := &editor.Theme{}
	ct.Tokens[editor.TokenOperator].Color = t.TextMuted
	ct.Tokens[editor.TokenPunctuation].Color = t.TextMuted
	codeThemes[t.TextMuted] = ct
	return ct
}

// codeView is an editor in Twinnel's code theme.
func codeView(c *ui.Context, st *editor.State) ui.Element {
	st.Theme = codeTheme(c.Theme())
	return editor.View(c, st)
}

// diffView is a diff in Twinnel's code theme.
func diffView(c *ui.Context, d *editor.DiffState) ui.Element {
	d.Theme = codeTheme(c.Theme())
	return editor.DiffView(c, d)
}

// codeLineH is the editor's line height: 13 pt at 1.5.
const codeLineH = 19.5

// editorHeight is a layer editor's height for its lines: as tall as the
// text plus a line to type into, between 120 and 320, so a short
// override does not sit in an empty box (kubai R-22, unslop R-4). It
// grows while the person types.
func editorHeight(lines int) float32 {
	return min(max(float32(lines+1)*codeLineH+16, 120), 320)
}

func (a *app) configPage(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	pageTitle(c, T.PageConfig)
	if !a.helperGate(c, s) {
		return
	}
	a.opNotes(c, s)
	d := &a.hd
	// Nothing to compose yet: one empty state for the page, with the two
	// ways in, instead of three empty sections (022, page size).
	if d.loaded && len(d.subs) == 0 && !d.cfg.subOpen && !hasStoredLayers(d.layers) {
		a.configEmpty(c, s)
		return
	}
	a.rowMarks()
	a.flushUndoToast(c)
	// The page reads as the compose stack: where it stands against what
	// mihomo runs, then the subscription, your overrides, and Twinnel's.
	a.applyCard(c, s)
	a.subscriptionsCard(c, s)
	a.overridesCard(c, s)
	a.managedCard(c, s)
}

// hasStoredLayers reports whether the person has overrides of their own
// (or takeover's base ones).
func hasStoredLayers(ls []rpc.Layer) bool {
	return slices.ContainsFunc(ls, func(l rpc.Layer) bool { return !l.Managed })
}

// configEmpty is the page before there is anything to compose: what is
// missing and the two ways to start, a subscription or taking over the
// config this Mac already runs.
func (a *app) configEmpty(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	w := a.helperWrites(s)
	ui.Column(c.Key("config-empty")).Gap(8).AlignItems(ui.Center).PaddingY(72).Transition(fadeMotion).Children(func() {
		ui.Box(c).Size(56, 56).Radius(plat.groupRadius).Background(t.TextMuted.Alpha(0.12)).
			AlignItems(ui.Center).Justify(ui.Center).AccessHidden(true).Children(func() {
			ui.Icon(c, iconConfig).FontSize(iconHero).TextColor(t.TextMuted)
		})
		ui.Text(c, T.ConfigEmptyTitle).FontSize(plat.headingSize).FontWeight(plat.headingWeight).Margin(8, 0, 0)
		ui.Text(c, T.ConfigEmptyHint).FontSize(plat.calloutSize).TextColor(t.TextMuted).TextAlign(ui.Center).
			Wrap().LineHeight(lineText).MaxWidth(360)
		ui.Row(c).Gap(8).Margin(8, 0, 0).Children(func() {
			if ui.PrimaryButton(c.Key("empty-add"), T.SubAdd).Disabled(!w).Clicked() {
				a.hd.cfg.subOpen, a.hd.cfg.subMins = true, "1440"
			}
			if pushButton(c.Key("empty-takeover"), T.ConfigEmptyTakeover).Clicked() {
				a.openPage(pageTakeover)
			}
			if pushButton(c.Key("empty-import"), T.ImportRun).Disabled(!w).Clicked() {
				a.openImport()
			}
		})
	})
}

// --- subscriptions ---

func (a *app) subscriptionsCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.cfg
	w := a.helperWrites(s)
	listCard(c, listHead(c, nil, T.Subscriptions, func() {
		if !v.subOpen && pushButton(c.Key("sub-import"), T.ImportRun).Disabled(!w).Clicked() {
			a.openImport()
		}
		if !v.subOpen && pushButton(c.Key("sub-add"), T.SubAdd).Disabled(!w).Clicked() {
			v.subOpen, v.subMins = true, "1440"
		}
	}), func() {
		if a.helperListsLoading(c, 1) {
			return // no false 还没有订阅 before the first load
		}
		if len(d.subs) == 0 && !v.subOpen {
			// Overrides exist but nothing to apply them to: the row size
			// of the page's empty state, with the other way in.
			listRow(c).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).Children(func() {
					ui.Text(c, T.SubNone).TextColor(t.TextMuted).Grow(1)
					if pushButton(c.Key("sub-takeover"), T.ConfigEmptyTakeover).Clicked() {
						a.openPage(pageTakeover)
					}
				})
			})
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
		// A local file is never re-read on a schedule (refreshDue needs a
		// URL): it has no interval to show (walkthrough 2, N12).
		line := src
		if sub.HasURL {
			interval := T.SubManual
			if sub.IntervalMinutes > 0 {
				interval = T.Interval(sub.IntervalMinutes)
			}
			line += " · " + interval
		}
		// Where the content comes from, and when it last did (U32): an
		// adopted subscription runs on the file takeover used until its
		// first download, which is not "never downloaded".
		num(small(c, line).SingleLine())
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
		case !sub.HasURL: // a local file is never downloaded (R-unslop-29)
			fetched = T.SubNoContent
		default:
			fetched = T.SubNeverFetched
		}
		num(small(c, fetched).SingleLine().Shrink(1).Tooltip(fetched))
		if sub.LastError != "" {
			errorNote(c, T.ErrorText(sub.LastError))
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

// overridesCard lists the person's overrides in compose order: the base
// ones takeover made (pinned first), then their own. Those reorder by
// dragging, as macOS lists do (an accent drop line, the rows move on
// drop; 005), by ⌥⌘↑ / ⌥⌘↓ on the open one, and from each row's context
// menu. A row opens its editor inline, under it, with a disclosure
// motion (015).
func (a *app) overridesCard(c *ui.Context, s status.Snapshot) {
	t := c.Theme()
	T := i18n.T
	d := &a.hd
	v := &d.cfg
	w := a.helperWrites(s)
	var base, user []rpc.Layer
	for _, l := range d.layers {
		switch {
		case l.Managed:
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
		sectionHead(c, T.Layers, T.OverridesInfo)
		if !v.isNew && pushButton(c.Key("layer-new"), T.LayerNew).Disabled(!w).Clicked() {
			a.newLayer()
		}
	}, func() {
		if a.helperListsLoading(c, 2) {
			return
		}
		for _, l := range base {
			a.layerRow(c.Key("layer:"+l.ID), l, nil, s, w, -1)
		}
		if len(base)+len(user) == 0 && !v.isNew {
			// Row size (022): the heading's 新建覆写 is the way in.
			listRow(c.Key("layers-none")).Children(func() {
				ui.Row(c).MinHeight(28).AlignItems(ui.Center).Children(func() {
					ui.Text(c, T.LayersNone).TextColor(t.TextMuted)
				})
			})
		}
		for i, l := range user {
			a.layerRow(c.Key("layer:"+l.ID), l, ids, s, w, i)
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

// managedCard lists the layers Twinnel generates (Tailscale coexistence,
// leak guard). They come after the person's, pinned; 托管 is said once,
// by the section, not on every row.
func (a *app) managedCard(c *ui.Context, s status.Snapshot) {
	T := i18n.T
	d := &a.hd
	w := a.helperWrites(s)
	listCard(c, func() {
		sectionHead(c, T.ManagedTitle, T.ManagedInfo)
	}, func() {
		if a.helperListsLoading(c, 2) {
			return
		}
		for _, l := range d.layers {
			if l.Managed {
				a.layerRow(c.Key("layer:"+l.ID), l, nil, s, w, -1)
			}
		}
	})
}

// sectionHead is a listCard heading with its ⓘ (AGENTS.md: the ⓘ says
// what a section is for, the page does not), then a spacer for the
// heading's actions.
func sectionHead(c *ui.Context, title, info string) {
	ui.Text(c, title).FontSize(plat.headingSize).FontWeight(plat.headingWeight).SingleLine().Shrink(0)
	if info != "" {
		infoTip(c, "", info, nil)
	}
	ui.Spacer(c)
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

// Layer rows share one anatomy (every row of both sections): the
// disclosure, the name over one line of state, and the switch at the
// trailing edge, as System Settings' rows. Identifiers stay in the
// context menu (复制 ID) and the editor.
const layerRowH = 52

// layerRow is one layer: its head, and under it while it is open the
// editor (a stored layer) or what the layer does (a managed one). ids
// are the stored layers' IDs in order and at the row's place among them
// (-1 for a base or managed layer, which neither drags nor takes drops).
func (a *app) layerRow(c *ui.Context, l rpc.Layer, ids []string, s status.Snapshot, w bool, at int) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	open := v.sel == l.ID && !v.isNew
	// Edge to edge in the list (it clips to animate its height), so the
	// head's fill spans the row (rowFill); head and body pad back in.
	col := ui.Column(c).Transition(rowMotion).Clip().MarginX(-groupPadX)
	dropAt := 0 // 1: the drop line above the row, 2: under it
	if at >= 0 && w {
		// A drop on this row puts the dragged layer at its place: the line
		// shows above when it comes from below, under when from above.
		if d, over := ui.DragOver[layerDrag](col); over && string(d) != l.ID {
			dropAt = 1
			if from := slices.Index(ids, string(d)); from >= 0 && from < at {
				dropAt = 2
			}
		}
		if d, ok := ui.Drop[layerDrag](col); ok && string(d) != l.ID {
			if next, ok := dropLayer(ids, string(d), at); ok {
				a.reorderLayers(next)
			}
		}
	}
	title := layerTitle(l)
	col.Children(func() {
		if dropAt != 0 {
			dropLine(c, t, dropAt == 2)
		}
		// Open is not selected: the chevron turns and the body shows; the
		// row takes no fill (never the accent, AGENTS.md).
		head := ui.Row(c.Key("head")).Gap(10).AlignItems(ui.Center).PaddingX(groupPadX).MinHeight(layerRowH).
			Transition(colorMotion).
			Focusable().Role(ui.RoleDisclosure).Expanded(open).Label(title)
		rowFill(c, head, false) // col reaches the box's edges
		head.ContextMenu(func(m *ui.Menu) { a.layerMenu(c, m, l, ids, w) })
		if at >= 0 && w && len(ids) > 1 {
			head.Drag(layerDrag(l.ID))
			if head.Dragging() {
				head.Opacity(0.4)
			}
		}
		head.Children(func() {
			disclosure(c, open).Shrink(0)
			ui.Column(c).Gap(gapSub).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, title).FontWeight(500).SingleLine().Shrink(1).Tooltip(title)
					switch {
					case l.ID == coexistLayerID:
						infoTip(c, "", T.CoexistInfo, nil)
					case l.Managed:
						infoTip(c, "", T.LeakGuardInfo, nil)
					}
				})
				m := num(meta(c, a.layerMeta(l, s)).Shrink(1))
				if !l.Managed {
					m.Tooltip(status.Bytes(int64(l.Size)))
				}
			})
			a.layerMark(c, l.ID)
			a.layerSwitch(c, l, w)
		})
		if head.Clicked() {
			if open {
				v.sel, v.ed = "", nil
			} else {
				a.selectLayer(l)
			}
		}
		if open {
			if l.Managed {
				a.managedBody(c.Key("managed"), l, s, w)
			} else {
				a.layerEditor(c.Key("editor"), s, w)
			}
		}
	})
}

// rowMarks finds the layers a row marks: those that differ from what
// mihomo runs while the composed config does (the names the apply card
// lists), and those the last failed apply names.
func (a *app) rowMarks() {
	d := &a.hd
	v := &d.cfg
	v.pendingIDs, v.failedIDs = nil, nil
	if d.unapplied && driftOf(d) == nil {
		if last := lastApplied(d.history); last != nil {
			ch, _ := pendingChanges(last.Layers, d.layers, d.settings, a.activeSubscription())
			for _, c := range ch {
				if !c.removed {
					if v.pendingIDs == nil {
						v.pendingIDs = map[string]bool{}
					}
					v.pendingIDs[c.id] = true
				}
			}
		}
	}
	if r := v.applied; r != nil && r.Error != "" {
		for _, cu := range r.Culprits {
			if cu.Layer != "" {
				if v.failedIDs == nil {
					v.failedIDs = map[string]bool{}
				}
				v.failedIDs[cu.Layer] = true
			}
		}
	}
}

// layerMark is a row's mark before its switch: the switch changes the
// composed config at once, but mihomo runs it only after 应用, so a
// changed row says 未应用 with the accent dot the apply card uses; a row
// the last apply failed on says so in the error color (kubai R-9). The
// mark fades out when the change is applied.
func (a *app) layerMark(c *ui.Context, id string) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	switch {
	case v.failedIDs[id]:
		ui.Row(c.Key("mark-failed")).Gap(4).AlignItems(ui.Center).Shrink(0).Transition(fadeMotion).Children(func() {
			ui.Icon(c, iconError).FontSize(iconSmall).TextColor(t.Danger)
			ui.Text(c, T.RowApplyFailed).FontSize(plat.smallSize).TextColor(pal(t).badText).SingleLine()
		})
	case v.pendingIDs[id]:
		ui.Row(c.Key("mark-pending")).Gap(6).AlignItems(ui.Center).Shrink(0).Transition(fadeMotion).Children(func() {
			dot(c, t.Accent, 6)
			ui.Text(c, T.RowUnapplied).FontSize(plat.smallSize).TextColor(t.TextMuted).SingleLine()
		})
	}
}

// dropLine is where a dragged override will land, as NSTableView draws
// it: a 2 pt accent line with a ring at its leading end, over the row's
// edge, so nothing moves while it shows (kubai R-21).
func dropLine(c *ui.Context, t *ui.Theme, below bool) {
	line := ui.Row(c.Key("drop")).Absolute().Left(groupPadX - 4).Right(groupPadX).Height(6).
		AlignItems(ui.Center).PassThrough().AccessHidden(true).Transition(fadeMotion)
	if below {
		line.Bottom(0)
	} else {
		line.Top(0)
	}
	line.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Border(1.5, t.Accent).Shrink(0)
		ui.Box(c).Height(2).Radius(1).Background(t.Accent).Grow(1)
	})
}

// layerMeta is the line under a layer's name: its state, never its ID.
// A stored layer says its size (and 基础 for takeover's base ones);
// coexistence, the tailnet it follows; leak guard, the groups it
// protects, or that none are chosen (which is why its switch is off).
func (a *app) layerMeta(l rpc.Layer, s status.Snapshot) string {
	T := i18n.T
	switch {
	case l.ID == coexistLayerID:
		tn := tailnetOf(s)
		if tn == nil || len(tn.Peers) == 0 {
			if tn != nil && tn.Suffix != "" {
				return tn.Suffix
			}
			return T.CoexistNoTailnet
		}
		text := i18n.F(T.CoexistDevicesN, len(tn.Peers))
		if tn.Suffix != "" {
			text += " · " + tn.Suffix
		}
		return text
	case l.Managed:
		groups := a.guardGroups()
		if len(groups) == 0 {
			return T.GuardNoGroups
		}
		return i18n.F(T.GuardMeta, strings.Join(groups, T.ListSep))
	}
	// What it does (3 条规则 · 改 dns), once its content is read; the size
	// is only the line's tooltip.
	text := " " // holds the line while the content loads
	if sum, ok := a.hd.cfg.sums[summaryKey(l)]; ok {
		text = sum.text()
	}
	if l.Stage == stageBase {
		text = T.GroupBase + " · " + strings.TrimSpace(text)
	}
	return text
}

// layerSwitch is a layer's switch, at the row's trailing edge. Leak
// guard is on while groups are chosen: it turns off by clearing them
// (kept for this session, so turning it on again brings them back), and
// stays off and disabled while there is nothing to protect (its row says
// so: 未选择策略组).
func (a *app) layerSwitch(c *ui.Context, l rpc.Layer, w bool) {
	T := i18n.T
	v := &a.hd.cfg
	on := l.Enabled
	sw := ui.Switch(c.Key("on"), &on).Label(T.LayerEnabled + " " + layerTitle(l)).Shrink(0)
	isGuard := l.Managed && l.ID != coexistLayerID
	switch {
	case isGuard:
		sw.Disabled(!w || (!l.Enabled && len(v.guardLast) == 0))
	default:
		sw.Disabled(!w)
	}
	if !sw.Changed() {
		return
	}
	if isGuard {
		groups := []string{}
		if on {
			groups = v.guardLast
		} else {
			v.guardLast = slices.Clone(a.guardGroups())
		}
		a.setGuardGroups(groups)
		return
	}
	a.setLayerEnabled(l, on)
}

// tailnetOf is the tailnet the helper last read, nil when it has none.
func tailnetOf(s status.Snapshot) *rpc.TailnetStatus {
	if st := s.Helper.Status; st != nil {
		return &st.Tailnet
	}
	return nil
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
		if m.Item(T.LayerRevert).Disabled(!w).Chosen() {
			a.revertLayer(l)
		}
	}
	m.Separator()
	if m.Item(T.CopyID).Chosen() {
		copyText(c, l.ID)
	}
	if !l.Managed {
		m.Separator()
		if m.Item(T.Remove).Danger().Disabled(!w).Chosen() {
			a.removeLayer(l.ID, layerTitle(l))
		}
	}
}

// newLayerRow is the row of a layer being made: its title and the editor,
// at the end of the list, coming in as a new row does.
func (a *app) newLayerRow(c *ui.Context, s status.Snapshot, w bool) {
	T := i18n.T
	v := &a.hd.cfg
	ui.Column(c).Transition(rowMotion).Clip().MarginX(-groupPadX).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).PaddingX(groupPadX).MinHeight(layerRowH).Children(func() {
			disclosure(c, true).Shrink(0)
			ui.Text(c, T.LayerNew).FontWeight(500).Grow(1).SingleLine()
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
				a.hd.msg, a.hd.msgErr = helperError(err), true
				a.invalidate()
				return
			}
			if v.ed != nil && v.ed.Buffer.Modified() {
				return // keep the user's unsaved edits
			}
			text := lc.Content
			if lc.Managed {
				text = generatedText(text)
			}
			v.ed = &editor.State{Buffer: yamlBuffer(text), ReadOnly: lc.Managed, Label: lc.Name}
			a.invalidate()
		})
	}()
}

// layersLoaded keeps the editor in step with a reloaded layer list.
func (a *app) layersLoaded() {
	a.loadLayerSummaries()
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
	v.edIDAuto, v.edAdv = true, false
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
	ed := rowBody(c)
	if v.reveal {
		v.reveal = false
		ed.ScrollIntoView(ui.ScrollIntoViewOptions{Animation: &rowMove})
	}
	ed.Children(func() {
		{
			// The name is what people give; the ID (the file's name in
			// the helper's store) follows it and waits under 高级 (N13).
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, T.LayerName).FontSize(plat.calloutSize).TextColor(t.TextMuted)
				ui.TextInput(c.Key("ed-name"), &v.edName).Label(T.LayerName)
			})
			if v.isNew && v.edIDAuto {
				v.edID = layerIDFor(v.edName, a.hd.layers)
			}
			if linkDisclosure(c.Key("ed-adv"), v.edAdv, T.LayerAdvanced).Clicked() {
				v.edAdv = !v.edAdv
			}
			if v.edAdv {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, T.LayerID).FontSize(plat.calloutSize).TextColor(t.TextMuted)
					before := v.edID
					in := ui.TextInput(c.Key("ed-id"), &v.edID).Label(T.LayerID).Placeholder("my-rules")
					if !v.isNew {
						in.Disabled(true)
					} else if v.edID != before {
						v.edIDAuto = false
					}
				})
			}
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
					// The labels name the apps; the syntax each means is
					// in the ⓘ (kubai R-22).
					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						ui.Text(c, T.LayerFormat).FontSize(plat.calloutSize).TextColor(t.TextMuted)
						infoTip(c, T.LayerFormat, T.LayerFormatInfo, nil)
					})
					ui.Segmented(c.Key("ed-format"), &v.edFormat, T.FormatPartyName, T.FormatVergeName)
				})
			})
		}
		if v.ed == nil {
			// Hold the editor's height while it loads, so the page does
			// not jump when the text comes (review-b 017 #6).
			a.loadingBox(c, "layer-editor", 120)
			return
		}
		// The notes' lines are marked in the editor itself: a dot in the
		// gutter and a tinted row (kubai R-14).
		notes, blocked := a.layerNotes()
		markNoteLines(t, v.ed, notes)
		codeFrame(c, editorHeight(v.ed.Buffer.LineCount()), func() { codeView(c.Key("ed:"+v.sel), v.ed).Grow(1) })
		a.drawLintNotes(c, notes)
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
				// A state, not a problem: the accent dot of every change
				// not yet in effect (kubai R-14, R-9).
				ui.Row(c).Gap(6).AlignItems(ui.Center).Transition(fadeMotion).Children(func() {
					dot(c, t.Accent, 6)
					ui.Text(c, T.Unsaved).FontSize(plat.calloutSize).TextColor(t.TextMuted).SingleLine()
				})
			}
			ui.Spacer(c)
			if !v.isNew {
				if i := indexLayer(a.hd.layers, v.sel); i >= 0 {
					if _, ok := a.revertable(a.hd.layers[i]); ok && pushButton(c.Key("ed-revert"), T.LayerRevert).Disabled(!w).Clicked() {
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

// rowBody is the part of an open layer row under its head: indented to
// the name's column (groupPadX + the chevron's 14 + the gap's 10), so the body
// lines up with the text above it.
func rowBody(c *ui.Context) ui.Element {
	// Under the title: past the chevron (14) and the head's gap (10).
	return ui.Column(c).Gap(gapRow).Padding(4, groupPadX, 16, groupPadX+24)
}

// managedBody is what an open managed layer does, in words and lists:
// what coexistence adds for the tailnet, which groups leak guard
// protects (and where to choose them). The generated YAML is one click
// away, sized to its text, without Twinnel's English header comment.
func (a *app) managedBody(c *ui.Context, l rpc.Layer, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	body := rowBody(c)
	if v.reveal {
		v.reveal = false
		body.ScrollIntoView(ui.ScrollIntoViewOptions{Animation: &rowMove})
	}
	body.Children(func() {
		if l.ID != coexistLayerID {
			a.guardBody(c, s, w)
			return
		}
		tn := tailnetOf(s)
		if tn == nil || (len(tn.Peers) == 0 && tn.Suffix == "") {
			ui.Text(c, T.CoexistNoTailnet).TextColor(t.TextMuted)
		} else {
			kvMono(c, T.CoexistRanges, config.DefaultTailnetIPv4+T.ListSep+config.DefaultTailnetIPv6)
			if tn.Suffix != "" {
				kvMono(c, T.CoexistDNS, tn.Suffix)
			}
			if len(tn.Peers) > 0 {
				kvWith(c, T.CoexistDevices, func() {
					ui.Text(c, strings.Join(tn.Peers, T.ListSep)).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
				})
			}
		}
		gen := linkDisclosure(c.Key("gen"), v.showGen, T.ShowGenerated)
		if gen.Clicked() {
			v.showGen = !v.showGen
		}
		if !v.showGen {
			return
		}
		if v.ed == nil {
			a.loadingBox(c, "managed-gen", 120)
			return
		}
		codeFrame(c, generatedHeight(v.ed.Buffer.String()), func() { codeView(c.Key("gen:"+v.sel), v.ed).Grow(1) })
	})
}

// guardBody is leak guard's view: the protected groups as tags, or the
// empty row, and the menu that chooses them (the groups that have DIRECT
// to remove, ticked when protected; each pick takes effect at once).
func (a *app) guardBody(c *ui.Context, s status.Snapshot, w bool) {
	t := c.Theme()
	T := i18n.T
	groups := a.guardGroups()
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(28).Children(func() {
		ui.Text(c, T.GuardGroups).FontSize(plat.calloutSize).TextColor(t.TextMuted).Shrink(0)
		ui.Spacer(c)
		cands := guardCandidates(s.Mihomo.Groups, groups)
		pushMenu(c.Key("guard-pick"), T.GuardPick, func(m *ui.Menu) {
			if len(cands) == 0 {
				m.Item(T.GuardNoCandidates).Disabled(true)
				return
			}
			for _, g := range cands {
				on := slices.Contains(groups, g)
				if m.Item(g).Checked(on).Disabled(!w).Chosen() {
					next := slices.DeleteFunc(slices.Clone(groups), func(x string) bool { return x == g })
					if !on {
						next = append(next, g)
					}
					a.setGuardGroups(next)
				}
			}
		}).Disabled(!w)
	})
	if len(groups) == 0 {
		ui.Text(c, T.GuardNone).TextColor(t.TextMuted)
		return
	}
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, g := range groups {
			tag(c, g)
		}
	})
}

// guardCandidates are the groups leak guard can protect: those of the
// running config that have DIRECT as a member, in config order, and the
// ones already protected (even when the running config lost them).
func guardCandidates(all []status.Group, protected []string) []string {
	var out []string
	for _, g := range all {
		if g.Hidden || g.Name == "GLOBAL" {
			continue
		}
		if slices.ContainsFunc(g.Members, func(m status.Member) bool { return m.Name == "DIRECT" }) || slices.Contains(protected, g.Name) {
			out = append(out, g.Name)
		}
	}
	for _, p := range protected {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// guardGroups are the groups leak guard protects now (settings).
func (a *app) guardGroups() []string {
	if set := a.hd.settings; set != nil {
		return set.LeakGuardGroups
	}
	return nil
}

// setGuardGroups saves leak guard's groups (settings.update). It reads
// as the layer's switch: quiet, the row shows it.
func (a *app) setGuardGroups(groups []string) {
	label := fmt.Sprintf(i18n.T.LayerToggle, i18n.T.TakeoverLayerName("leak-guard", nil, "Leak guard"))
	a.helperOp(label, 30*time.Second, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodSettingsUpdate, rpc.SettingsUpdate{LeakGuardGroups: &groups}, nil)
	}, nil)
}

// generatedHeight is a read-only YAML view's height for text: its lines
// at the editor's 13 × 1.5, plus its insets, at most 360.
func generatedHeight(text string) float32 {
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	return min(float32(lines)*codeLineH+16, 360)
}

// generatedText is a managed layer's YAML as shown: Twinnel's header
// comment (English, for the file) is left out.
func generatedText(content string) string {
	lines := strings.Split(content, "\n")
	i := 0
	for i < len(lines) && (strings.HasPrefix(strings.TrimSpace(lines[i]), "#") || strings.TrimSpace(lines[i]) == "") {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// removeLayer deletes a stored layer at once; its toast offers 撤销,
// which writes it back with its text (kubai R-8).
func (a *app) removeLayer(id, name string) {
	T := i18n.T
	v := &a.hd.cfg
	a.reversibleOp(T.Remove+" "+id, i18n.F(T.DoneLayerDeleted, name), func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodLayerDelete, rpc.IDParams{ID: id}, nil)
	}, func(err error) {
		if err == nil && v.sel == id {
			v.sel, v.ed = "", nil
		}
	})
}

// layerNotes are the edited layer's notes: the helper's refusal of the
// last save first (at its line when it has one), then the merge-directive
// notes; blocked reports an error among them (the save then waits).
func (a *app) layerNotes() (notes []status.LintNote, blocked bool) {
	v := &a.hd.cfg
	key := lintKey{v.ed.Buffer, v.ed.Buffer.Version(), v.edFormat}
	if key != v.lintKey {
		v.lintKey, v.lint = key, nil
		if layerFormats[v.edFormat] == formatMerge {
			v.lint = status.LintOverride(v.ed.Buffer.String())
		}
	}
	notes = v.lint
	if m := v.saveErr; m != nil && v.saveErrBuf == v.ed.Buffer.Version() {
		text, raw := i18n.T.MsgText(*m, false)
		if raw {
			text = m.Text // a refusal Twinnel has no words for: the helper's own, here at the editor
		}
		_, line := layerErrorLine(*m)
		notes = append([]status.LintNote{{Level: status.LevelError, Line: line, Text: text}}, notes...)
	}
	for _, n := range notes {
		if n.Level == status.LevelError {
			blocked = true
		}
	}
	return notes, blocked
}

// noteLook is a note's glyph and color: an error in the danger color, a
// likely mistake in the warning one, a reminder of what a key does as
// information, muted (not the minus that means "skipped" elsewhere,
// unslop R-19).
func noteLook(t *ui.Theme, l status.Level) (*ui.SVG, ui.Color, ui.Color) {
	switch l {
	case status.LevelError:
		return iconError, t.Danger, pal(t).badText
	case status.LevelWarn:
		return iconWarning, t.Warning, pal(t).warnText
	}
	return iconAbout, t.TextMuted, t.TextMuted
}

// markNoteLines marks the lines the warnings and errors name in the
// editor: a dot of the note's color in the gutter and a light tint on
// the row, so the note under the editor and its line read as one (kubai
// R-14). Reminders stay unmarked.
func markNoteLines(t *ui.Theme, st *editor.State, notes []status.LintNote) {
	st.GutterMarks, st.LineBackgrounds = nil, nil
	n := st.Buffer.LineCount()
	for _, note := range notes {
		if note.Line < 1 || note.Line > n || note.Level < status.LevelWarn {
			continue
		}
		_, col, _ := noteLook(t, note.Level)
		st.GutterMarks = append(st.GutterMarks, editor.GutterMark{Line: note.Line - 1, Color: col})
		st.LineBackgrounds = append(st.LineBackgrounds, editor.LineBackground{First: note.Line - 1, Last: note.Line - 1, Color: col.Alpha(0.1)})
	}
}

// drawLintNotes draws the notes under the editor. A note with a line is
// a button that puts the caret on it.
func (a *app) drawLintNotes(c *ui.Context, notes []status.LintNote) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	for i, n := range notes {
		if i == 8 {
			ui.Textf(c, "… +%d", len(notes)-8).FontSize(plat.calloutSize).TextColor(t.TextMuted)
			break
		}
		icon, icol, tcol := noteLook(t, n.Level)
		text := n.Text
		if n.Line > 0 {
			text = fmt.Sprintf(T.LintLine, n.Line) + text
		}
		row := ui.Row(c.Key(fmt.Sprint("note", i))).Gap(8).AlignItems(ui.Start)
		if n.Line > 0 && v.ed != nil {
			label := fmt.Sprintf(T.LintGoTo, n.Line)
			row.Focusable().Role(ui.RoleButton).Label(label + T.LabelSep + n.Text).Tooltip(label)
			if row.Clicked() {
				v.ed.GoTo(editor.Pos{Line: n.Line - 1})
			}
		}
		row.Children(func() {
			ui.Icon(c, icon).FontSize(iconInline).TextColor(icol).MarginY(1)
			ui.Text(c, text).FontSize(plat.calloutSize).TextColor(tcol).Grow(1).Wrap().LineHeight(lineText)
		})
	}
}

// layerIDFor derives a new override's ID from its name: lower-case ASCII
// letters and digits joined by "-", else "override"; a number is added
// when another override has it.
func layerIDFor(name string, layers []rpc.Layer) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	base := b.String()
	if len(base) > 48 {
		base = strings.TrimRight(base[:48], "-")
	}
	if base == "" || base == coexistLayerID || base == leakGuardLayerID {
		base = "override"
	}
	taken := func(id string) bool { return indexLayer(layers, id) >= 0 }
	id := base
	for n := 2; taken(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
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
			m := helperMsg(err)
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

// applyCard is the top of the page: where the overrides stand against
// what mihomo runs, and the one place to act on it. Its first row is the
// state (与运行中的配置一致 / N 处改动未应用 / the drift after a
// rollback) with its actions at the right: 应用 is the primary button
// only when there is something to apply, and disabled otherwise (the
// state says why). 预览 opens the check and the diff inline, under it.
func (a *app) applyCard(c *ui.Context, s status.Snapshot) {
	d := &a.hd
	v := &d.cfg
	dr := driftOf(d)
	if dr == nil && a.activeSubscription() == "" && v.preview == nil && v.applied == nil && v.prevErr == "" {
		return // nothing composes yet: the subscription card says what is missing
	}
	t := c.Theme()
	T := i18n.T
	w := a.helperWrites(s)
	busy := d.busy != ""
	groupBox(c, t).Key("apply").Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).MinHeight(28).Children(func() {
			switch {
			case dr != nil:
				a.driftState(c, s, *dr, w)
				return
			case d.unapplied:
				text, last, ch, sub := a.pendingText()
				if v.preview != nil && v.prevFor != text {
					// The changes moved on: the preview no longer shows them.
					v.preview, v.diff, v.diffErr = nil, nil, ""
				}
				dot(c, t.Accent, 8).Shrink(0)
				ui.Text(c, text).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
				if last != nil && last.Layers != nil && (len(ch) > 0 || sub) &&
					pushButton(c.Key("discard"), T.Discard).Disabled(!w || busy).Clicked() {
					a.discardChanges(*last)
				}
			case d.unappliedErr:
				ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText).Shrink(0)
				ui.Text(c, T.StateUnknown).Grow(1).Shrink(1).Wrap().LineHeight(lineText)
			default:
				ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, T.StateInSync).TextColor(t.TextMuted).Grow(1).Shrink(1).SingleLine()
			}
			work := d.unapplied || d.unappliedErr
			if !work && v.preview != nil && v.diff != nil {
				// The last change went away: nothing left to preview.
				v.preview, v.diff, v.diffErr = nil, nil, ""
			}
			// With nothing to apply there is nothing to preview: 预览 waits
			// as 应用 does, the state says why (kubai R-13).
			open := v.preview != nil || v.prevErr != ""
			label := T.Preview
			if open {
				label = T.PreviewHide
			}
			pv := a.opButton(c.Key("preview"), iconPreview, label, false, T.Preview).Disabled(!s.Helper.Connected() || busy || (!work && !open))
			if !work && !open {
				pv.Tooltip(T.StateInSync)
			}
			if pv.Clicked() {
				if open {
					v.preview, v.diff, v.diffErr, v.prevErr = nil, nil, "", ""
				} else {
					a.runPreview(s)
				}
			}
			ap := a.opButton(c.Key("apply"), iconOK, T.Apply, work, T.Apply).Disabled(!w || !work)
			if !work {
				ap.Tooltip(T.NothingToApply)
			}
			if ap.Clicked() {
				a.runApply()
			}
		})
		if v.preview != nil || v.diffErr != "" || v.diff != nil || v.prevErr != "" {
			ui.Divider(c)
			a.previewBody(c)
		}
		if r := v.applied; r != nil && dr == nil { // a rollback since: the drift says what runs
			ui.Divider(c)
			a.applySummary(c, *r)
		}
	})
}

// previewBody is a preview, under the apply card's state: what failed
// when the check did not pass, the diff against what mihomo runs, and
// the 说明 the apply records (H6), filled from what changed.
func (a *app) previewBody(c *ui.Context) {
	t := c.Theme()
	T := i18n.T
	v := &a.hd.cfg
	if v.prevErr != "" {
		// A preview that could not run says so here, under the button
		// that ran it, not at the top of the page (unslop R-22).
		errorNote(c, v.prevErr)
	}
	if p := v.preview; p != nil {
		if !p.Valid {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconError).FontSize(iconInline).TextColor(t.Danger)
				ui.Text(c, T.ValidFail).TextColor(pal(t).badText)
			})
			a.culpritNotes(c, p.Culprits)
			validateErrors(c, p.ValidateErrors)
		}
		for i, m := range resultMessages(*p, nil) {
			msgNote(c.Key(fmt.Sprint("cw", i)), m, true)
		}
	}
	if v.diffErr != "" {
		ui.Text(c, v.diffErr).FontSize(plat.calloutSize).TextColor(t.TextMuted).Wrap().LineHeight(lineText)
	}
	same := false
	if v.diff != nil {
		add, del := v.diff.Stats()
		if same = add+del == 0; same {
			// Nothing would change: say so, without an empty diff or a
			// 说明 to fill (kubai R-13).
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconOK).FontSize(iconInline).TextColor(t.TextMuted)
				ui.Text(c, T.StateInSync).TextColor(t.TextMuted)
			})
		} else {
			num(ui.Text(c, i18n.F(T.DiffStats, add, del)).FontSize(plat.calloutSize).TextColor(t.TextMuted))
			codeFrame(c, 380, func() { diffView(c.Key("preview-diff"), v.diff).Grow(1) })
		}
	}
	if v.preview != nil && !same {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, T.ApplyReason).FontSize(plat.calloutSize).TextColor(t.TextMuted)
			ui.TextInput(c.Key("reason"), &v.reason).Label(T.ApplyReason).Placeholder(T.ReasonOptional)
		})
	}
}

// culpritNotes say which override (or the subscription) holds the name
// mihomo -t complained about, with a way to it (walkthrough 2, N5).
func (a *app) culpritNotes(c *ui.Context, cs []rpc.Culprit) {
	t := c.Theme()
	T := i18n.T
	for i, cu := range cs {
		text := i18n.F(T.CulpritSub, cu.Name)
		var layer *rpc.Layer
		if cu.Layer != "" {
			title := cu.Title
			for j := range a.hd.layers {
				if a.hd.layers[j].ID == cu.Layer {
					layer = &a.hd.layers[j]
					title = layerTitle(*layer)
				}
			}
			text = i18n.F(T.CulpritLayer, title, cu.Name)
		}
		ui.Row(c.Key(fmt.Sprint("culprit", i))).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(pal(t).warnText).Shrink(0)
			ui.Text(c, text).FontSize(plat.calloutSize).Wrap().LineHeight(lineText).Grow(1).Shrink(1)
			if layer != nil {
				l := *layer
				if actionLink(c.Key("open"), T.CulpritOpen).Clicked() {
					a.showPage(pageConfig)
					a.selectLayer(l)
				}
			}
		})
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
		// One mark for the outcome (unslop R-23): a dot when it went
		// through, a hollow ring when it failed; the reason under it
		// says why, in the normal text color.
		if r.Error != "" {
			ui.Box(c).Size(8, 8).Radius(4).Border(1.5, col).Shrink(0)
		} else {
			dot(c, col, 8)
		}
		ui.Text(c, head).FontWeight(500)
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
		impactDetail(c, r.Impact)
	}
	for i, m := range resultMessages(r.ComposeResult, r.Impact) {
		msgNote(c.Key(fmt.Sprint("aw", i)), m, true)
	}
	if r.Error != "" {
		// What failed, in words (U53); mihomo -t's own lines under it.
		m := errorMsg(r.Error, r.ErrorMsg)
		reasonLine(c.Key("aerr"), m)
		if r.RolledBack && m.Code != "reload-failed" && m.Code != "verify-failed" { // those say it themselves
			small(c, T.ApplyFailedRestore).Margin(0, 0, 0, 16)
		}
		a.culpritNotes(c, r.Culprits)
		validateErrors(c, r.ValidateErrors)
	}
}

// reasonLine is a failure's reason under its mark: the message in the
// UI language, in the normal text color (the mark already says it
// failed), indented to the head's text; the helper's own words stay in
// the tooltip and 复制原文.
func reasonLine(c *ui.Context, m rpc.Msg) ui.Element {
	text, raw := i18n.T.MsgText(m, false)
	el := ui.Text(c, text).FontSize(plat.calloutSize).Wrap().LineHeight(lineText).Selectable().Margin(0, 0, 0, 16)
	if raw && m.Text != "" {
		el.Tooltip(m.Text)
	}
	if m.Text != "" {
		el.ContextMenu(func(menu *ui.Menu) {
			if menu.Item(i18n.T.MsgCopyRaw).Chosen() {
				copyText(c, m.Text)
			}
		})
	}
	return el
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
			impactDetail(c, im)
		})
	})
}

// impactDetail draws the reasons a reload interrupts the network, and what
// only a restart applies, from the impact's fields (U57: no English notes).
func impactDetail(c *ui.Context, im *rpc.ReloadImpact) {
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
		v.diff, v.diffErr, v.prevErr = nil, "", ""
		if err != nil {
			v.preview = nil
			v.prevErr, a.hd.msg, a.hd.msgErr = a.hd.msg, "", false // shown in the apply card
			return
		}
		v.preview = &res
		v.prevFor, _, _, _ = a.pendingText()
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
		v.preview, v.diff, v.diffErr = nil, nil, "" // what it showed is applied, or what failed says why
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
