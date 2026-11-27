package main

// 导入配置: the sheet on 订阅与覆写 that brings subscriptions and overrides
// from another machine's export, a subscription link list or an override
// file (config.import). It opens on the helper's dry run, asks about ids
// already in use, and imports on 导入. Nothing is applied: the page's
// apply card then shows what changed.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// importView is the sheet's state. data is the file as read (it may hold
// subscription URLs): dropped when the sheet closes.
type importView struct {
	open   bool
	name   string
	data   []byte
	plan   *rpc.ConfigImportResult
	choice map[string]*int // conflict key → importChoices index
}

// importChoices are a conflict's segments, in order.
var importChoices = []string{rpc.ImportReplace, rpc.ImportSkip, rpc.ImportRename}

// importDefault is the preselected segment: keep what is here.
const importDefault = 1

func importKey(it rpc.ImportItem) string { return it.Kind + ":" + it.ID }

// openImport picks a file and opens the sheet on the helper's dry run.
// The fresh-setup flow can call it too.
func (a *app) openImport() {
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: parent,
			Title:  i18n.Cur().ImportPick,
			Filters: []mygo.FileFilter{{Name: i18n.Cur().ImportFiles,
				Extensions: []string{"zip", "yaml", "yml", "txt", "conf", "list"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := readImport(paths[0])
		mygo.RunOnMain(func() {
			if err != nil {
				a.hd.msg, a.hd.msgErr = i18n.Cur().ErrorPrefix+err.Error(), true
				return
			}
			a.planImport(filepath.Base(paths[0]), data)
		})
	}()
}

// readImport reads at most bundle.MaxFile bytes; config.import refuses
// a larger file with its own words.
func readImport(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, bundle.MaxFile+1))
}

// planImport runs the dry run and opens the sheet on its plan.
func (a *app) planImport(name string, data []byte) {
	T := i18n.T
	var plan rpc.ConfigImportResult
	a.helperOp(T.ImportTitle, time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodConfigImport, rpc.ConfigImportParams{Data: data, Name: name, DryRun: true}, &plan)
	}, func(err error) {
		if err != nil {
			return
		}
		v := &a.hd.cfg.imp
		*v = importView{open: true, name: name, data: data, plan: &plan, choice: map[string]*int{}}
		for _, it := range plan.Items {
			if it.Action == rpc.ImportConflict {
				i := importDefault
				v.choice[importKey(it)] = &i
			}
		}
	})
}

// importResolve is the sheet's choices as config.import's resolve.
func importResolve(v *importView) map[string]string {
	out := map[string]string{}
	for k, i := range v.choice {
		if *i >= 0 && *i < len(importChoices) {
			out[k] = importChoices[*i]
		}
	}
	return out
}

// importWrites counts the items the import stores with resolve.
func importWrites(plan *rpc.ConfigImportResult, resolve map[string]string) int {
	n := 0
	for _, it := range plan.Items {
		act := it.Action
		if act == rpc.ImportConflict {
			act = resolve[importKey(it)]
		}
		if act == rpc.ImportAdd || act == rpc.ImportReplace || act == rpc.ImportRename {
			n++
		}
	}
	return n
}

func (a *app) closeImport() { a.hd.cfg.imp = importView{} }

// runImport imports with the sheet's choices.
func (a *app) runImport() {
	T := i18n.T
	v := &a.hd.cfg.imp
	p := rpc.ConfigImportParams{Data: v.data, Name: v.name, Resolve: importResolve(v), OnConflict: rpc.ImportSkip}
	var r rpc.ConfigImportResult
	a.closeImport()
	a.helperOp(T.ImportDo, 2*time.Minute, func(ctx context.Context, hc *status.HelperConn) error {
		return hc.Call(ctx, rpc.MethodConfigImport, p, &r)
	}, func(err error) {
		if err == nil {
			a.toast = i18n.F(i18n.T.ImportDone, r.Stored)
		}
	})
}

// importSheet draws the sheet while it is open.
func (a *app) importSheet(c *ui.Context, s status.Snapshot) {
	v := &a.hd.cfg.imp
	if !v.open || v.plan == nil {
		return
	}
	open := true
	ui.Modal(c.Key("import-sheet"), &open, func() {
		a.importBody(c, s, v)
	})
	if !open { // Esc or a click outside
		a.closeImport()
	}
}

func (a *app) importBody(c *ui.Context, s status.Snapshot, v *importView) {
	t := c.Theme()
	T := i18n.T
	plan := v.plan
	ui.Column(c).Gap(12).Width(480).MaxWidth(560).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T.ImportTitle).FontSize(plat.headingSize).FontWeight(plat.headingWeight)
			infoTipAbout(c, T.ImportTitle, T.ImportTip, nil)
		})
		src := T.ImportSource[plan.Source]
		if src == "" {
			src = plan.Source
		}
		small(c, v.name+" · "+src).SingleLine().Shrink(1).Tooltip(v.name)

		ui.Scroll(c.Key("import-items")).MaxHeight(320).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				a.importGroup(c, v, rpc.ImportKindSubscription, T.Subscriptions)
				a.importGroup(c, v, rpc.ImportKindLayer, T.Layers)
			})
		})
		if plan.Activated != "" {
			name := plan.Activated
			for _, it := range plan.Items {
				if it.Kind == rpc.ImportKindSubscription && it.NewID == plan.Activated {
					name = it.Name
				}
			}
			infoNote(c, i18n.F(T.ImportBecomeActive, name))
		}
		for _, w := range plan.Warnings {
			if text := T.ImportWarn[w]; text != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconWarning).FontSize(iconInline).TextColor(t.TextMuted).MarginY(1)
					ui.Text(c, text).FontSize(plat.calloutSize).TextColor(t.TextMuted).Grow(1).Wrap().LineHeight(lineText)
				})
			}
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if pushButton(c.Key("import-cancel"), T.Cancel).Clicked() {
				a.closeImport()
			}
			can := a.helperWrites(s) && (importWrites(plan, importResolve(v)) > 0 || plan.Activated != "")
			if ui.PrimaryButton(c.Key("import-do"), T.ImportDo).Disabled(!can).Clicked() {
				a.runImport()
			}
		})
	})
}

// importGroup lists one kind's items under its heading.
func (a *app) importGroup(c *ui.Context, v *importView, kind, heading string) {
	t := c.Theme()
	T := i18n.T
	var items []rpc.ImportItem
	for _, it := range v.plan.Items {
		if it.Kind == kind {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return
	}
	ui.Text(c, heading).FontSize(plat.calloutSize).FontWeight(600).TextColor(t.TextMuted).Margin(6, 0, 2)
	for _, it := range items {
		ui.Column(c.Key("imp:" + importKey(it))).Gap(4).PaddingY(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(22).Children(func() {
				ui.Text(c, it.Name).FontWeight(600).SingleLine().Shrink(1).Tooltip(it.Name)
				for _, m := range importMeta(it) {
					meta(c, m)
				}
				ui.Spacer(c)
				if it.Action != rpc.ImportConflict {
					tag(c, T.ImportAction[it.Action]).Tooltip(it.Reason)
				}
			})
			if ch := v.choice[importKey(it)]; ch != nil {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					small(c, i18n.F(T.ImportExisting, it.Existing)).SingleLine().Shrink(1).Grow(1)
					segmented(c.Key("choice"), ch, T.ImportAction[rpc.ImportReplace], T.ImportAction[rpc.ImportSkip], T.ImportAction[rpc.ImportRename])
				})
			}
		})
	}
}

// importMeta is an item's quiet details: format, off, active, not downloaded.
func importMeta(it rpc.ImportItem) []string {
	T := i18n.T
	var out []string
	switch it.Kind {
	case rpc.ImportKindLayer:
		if it.Action == rpc.ImportGenerated || it.Action == rpc.ImportInvalid {
			return nil
		}
		if it.Format == "verge" {
			out = append(out, T.FormatVergeName)
		}
		if !it.Enabled {
			out = append(out, T.ImportOff)
		}
	case rpc.ImportKindSubscription:
		if it.Active {
			out = append(out, T.SubActive)
		}
		if it.HasURL && !it.HasContent {
			out = append(out, T.ImportURLOnly)
		}
	}
	return out
}
