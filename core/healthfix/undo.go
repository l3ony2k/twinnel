package healthfix

// How a fix that writes the config records what it changes, and how the
// undo puts it back. One path for every config fix: the stored layers and
// settings it writes are read before the write (LayerBefore, Settings in
// the Undo), the undo writes them back and applies, so the layer store and
// the running config end in the same pre-fix state. A history rollback
// alone would change only the running config and leave the store on the
// fix, which the next apply then brings back.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// configChange is what a config fix writes before it applies.
type configChange struct {
	layers   []rpc.LayerPutParams
	settings *rpc.SettingsUpdate
	// then runs after the writes, before the apply (coexist: read the
	// tailnet now).
	then func(ctx context.Context) error
}

// changeConfig records the layers and settings ch writes as they are now,
// writes them, applies, and returns the undo (nil when nothing stored
// changed). A failed step puts back what was written; the helper keeps
// the running config of a failed apply, so both stay as before.
func (f *Fixer) changeConfig(ctx context.Context, p Plan, ch configChange) (*Undo, error) {
	u := &Undo{Action: p.Action}
	var err error
	if u.Layers, err = f.layersNow(ctx, layerIDs(ch.layers)); err != nil {
		return nil, err
	}
	if ch.settings != nil {
		if u.Settings, err = f.settingsNow(ctx, ch.settings); err != nil {
			return nil, err
		}
	}
	err = f.writeConfig(ctx, ch.layers, u.Layers, ch.settings)
	if err == nil && ch.then != nil {
		err = ch.then(ctx)
	}
	if err == nil {
		err = f.apply(ctx, p.Check)
	}
	if err != nil {
		if perr := f.putBack(ctx, u.Layers, u.Settings); perr != nil {
			err = fmt.Errorf("%w (putting the config back: %v)", err, perr)
		}
		return nil, err
	}
	if len(u.Layers) == 0 && u.Settings == nil {
		return nil, nil
	}
	return u, nil
}

// writeConfig writes the fix's settings and layers. An override that
// exists keeps its name (one adopted or named by the user).
func (f *Fixer) writeConfig(ctx context.Context, layers []rpc.LayerPutParams, before []LayerBefore, settings *rpc.SettingsUpdate) error {
	if settings != nil {
		if err := f.Helper.Call(ctx, rpc.MethodSettingsUpdate, *settings, nil); err != nil {
			return err
		}
	}
	for i, l := range layers {
		if !before[i].Absent {
			l.Name = ""
		}
		if err := f.Helper.Call(ctx, rpc.MethodLayerPut, l, &rpc.Layer{}); err != nil {
			return err
		}
	}
	return nil
}

func layerIDs(ls []rpc.LayerPutParams) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}

// layersNow reads the stored layers ids as they are now.
func (f *Fixer) layersNow(ctx context.Context, ids []string) ([]LayerBefore, error) {
	var out []LayerBefore
	for _, id := range ids {
		var lc rpc.LayerContent
		err := f.helper().Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: id}, &lc)
		var re *rpc.Error
		switch {
		case errors.As(err, &re) && re.Code == rpc.CodeNotFound:
			out = append(out, LayerBefore{ID: id, Absent: true})
		case err != nil:
			return nil, fmt.Errorf("layer %s: %w", id, err)
		default:
			out = append(out, LayerBefore{ID: id, Name: lc.Name, Stage: lc.Stage, Format: lc.Format, Enabled: lc.Enabled, Content: lc.Content})
		}
	}
	return out, nil
}

// settingsNow is the update that puts back the fields of u as they are
// now (a nil list as an empty one, so the update clears it).
func (f *Fixer) settingsNow(ctx context.Context, u *rpc.SettingsUpdate) (*rpc.SettingsUpdate, error) {
	var st rpc.Settings
	if err := f.helper().Call(ctx, rpc.MethodSettingsGet, rpc.Empty{}, &st); err != nil {
		return nil, err
	}
	return settingsLike(u, st)
}

// settingsLike is an update with the fields u sets, valued as in st
// (matched by JSON name).
func settingsLike(u *rpc.SettingsUpdate, st rpc.Settings) (*rpc.SettingsUpdate, error) {
	cur := map[string]reflect.Value{}
	sv := reflect.ValueOf(st)
	for i := 0; i < sv.NumField(); i++ {
		cur[jsonName(sv.Type().Field(i))] = sv.Field(i)
	}
	out := &rpc.SettingsUpdate{}
	uv, ov := reflect.ValueOf(u).Elem(), reflect.ValueOf(out).Elem()
	for i := 0; i < uv.NumField(); i++ {
		fv := uv.Field(i)
		if fv.Kind() != reflect.Pointer || fv.IsNil() {
			continue
		}
		name := jsonName(uv.Type().Field(i))
		v, ok := cur[name]
		if !ok || v.Type() != fv.Type().Elem() {
			return nil, fmt.Errorf("settings field %s has no value to put back", name)
		}
		p := reflect.New(v.Type())
		p.Elem().Set(v)
		if v.Kind() == reflect.Slice && v.IsNil() {
			p.Elem().Set(reflect.MakeSlice(v.Type(), 0, 0))
		}
		ov.Field(i).Set(p)
	}
	return out, nil
}

func jsonName(sf reflect.StructField) string {
	name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	if name == "" {
		return sf.Name
	}
	return name
}

// putBack writes layers and settings as recorded (no apply).
func (f *Fixer) putBack(ctx context.Context, layers []LayerBefore, settings *rpc.SettingsUpdate) error {
	h := f.helper()
	var errs []error
	for _, l := range layers {
		if l.Absent {
			err := h.Call(ctx, rpc.MethodLayerDelete, rpc.IDParams{ID: l.ID}, nil)
			var re *rpc.Error
			if err != nil && !(errors.As(err, &re) && re.Code == rpc.CodeNotFound) {
				errs = append(errs, err)
			}
			continue
		}
		on := l.Enabled
		if err := h.Call(ctx, rpc.MethodLayerPut, rpc.LayerPutParams{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format,
			Enabled: &on, Content: l.Content}, &rpc.Layer{}); err != nil {
			errs = append(errs, err)
		}
	}
	if settings != nil {
		if err := h.Call(ctx, rpc.MethodSettingsUpdate, *settings, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// restoreConfig puts layers and settings back and applies. If the apply
// fails, the store goes back to what it was before the undo (what still
// runs), so the two never disagree.
func (f *Fixer) restoreConfig(ctx context.Context, layers []LayerBefore, settings *rpc.SettingsUpdate) error {
	ids := make([]string, len(layers))
	for i, l := range layers {
		ids[i] = l.ID
	}
	now, err := f.layersNow(ctx, ids)
	if err != nil {
		return err
	}
	var setNow *rpc.SettingsUpdate
	if settings != nil {
		if setNow, err = f.settingsNow(ctx, settings); err != nil {
			return err
		}
	}
	err = f.putBack(ctx, layers, settings)
	if err == nil {
		err = f.apply(ctx, "undo")
	}
	if err != nil {
		if perr := f.putBack(ctx, now, setNow); perr != nil {
			err = fmt.Errorf("%w (keeping the fix: %v)", err, perr)
		}
	}
	return err
}

// legacyLayers are the stored layers an older version's history undo
// stood for, by action.
var legacyLayers = map[string][]string{
	health.FixTUNStack:           {health.TUNStackLayerID},
	health.FixControllerLoopback: {LoopbackLayerID},
}

// undoHistory is an older version's undo: the layers of the action come
// back from the history entry's manifest, then the config is applied.
func (f *Fixer) undoHistory(ctx context.Context, u *Undo) error {
	ids := legacyLayers[u.Action]
	if len(ids) == 0 {
		return fmt.Errorf("%s: %s", u.Action, tx(f.Lang).undoNothing)
	}
	now, err := f.layersNow(ctx, ids)
	if err != nil {
		return err
	}
	h := f.helper()
	if err := h.Call(ctx, rpc.MethodLayerRestore, rpc.LayerRestoreParams{HistoryID: u.HistoryID, IDs: ids}, &rpc.LayerRestoreResult{}); err != nil {
		return err
	}
	if err := f.apply(ctx, "undo"); err != nil {
		_ = f.putBack(ctx, now, nil)
		return err
	}
	return nil
}

// Undo puts back what Apply changed, then runs the checks again.
func (f *Fixer) Undo(ctx context.Context, u *Undo) (*health.Report, error) {
	if u == nil {
		return nil, errors.New("nothing to undo")
	}
	var err error
	layers := u.layers()
	switch {
	case u.Autostart != nil:
		err = f.undoAutostart(ctx, u.Autostart)
	case u.DNS:
		err = f.helper().Call(ctx, rpc.MethodDNSRestore, rpc.Empty{}, &rpc.DNSStatus{})
	case u.Mode != "":
		if f.Mihomo == nil {
			return nil, errors.New(tx(f.Lang).blockedMihomo)
		}
		err = f.Mihomo.SetMode(ctx, u.Mode)
	case u.ExitNode != "":
		if f.Tailscale == nil {
			return nil, errors.New(tx(f.Lang).blockedTailscale)
		}
		err = f.Tailscale.SetExitNode(ctx, u.ExitNode)
	case len(layers) > 0 || u.Settings != nil:
		if f.Helper == nil {
			return nil, errors.New(tx(f.Lang).blockedHelper)
		}
		err = f.restoreConfig(ctx, layers, u.Settings)
	case u.HistoryID != "":
		if f.Helper == nil {
			return nil, errors.New(tx(f.Lang).blockedHelper)
		}
		err = f.undoHistory(ctx, u)
	default:
		return nil, errors.New("nothing to undo")
	}
	if err != nil {
		return nil, err
	}
	return f.rerun(ctx)
}

// layers are the layers to put back, the older LayerID included.
func (u *Undo) layers() []LayerBefore {
	if u.LayerID != "" && len(u.Layers) == 0 {
		return []LayerBefore{{ID: u.LayerID, Absent: true}}
	}
	return u.Layers
}

// UndoPlan is an undo as previewed: its steps, and what it goes back to.
type UndoPlan struct {
	Action string   `json:"action"`
	Steps  []string `json:"steps"`
	// Warning says the undo returns to the state the check failed in.
	// Every undo does (the fix ran because the check failed), so every
	// client shows it before running the undo.
	Warning string `json:"warning"`
}

// UndoPreview words u for the user before it runs. It reads nothing.
func (f *Fixer) UndoPreview(u *Undo) UndoPlan {
	L := tx(f.Lang)
	p := UndoPlan{Action: u.Action}
	name := func(l LayerBefore) string {
		if l.Name != "" {
			return l.Name
		}
		return l.ID
	}
	switch {
	case u.Autostart != nil:
		p.Steps = []string{fmt.Sprintf(L.undoAutostart, strings.Join(u.Autostart.IDs, ", "))}
	case u.DNS:
		p.Steps = []string{L.undoDNS}
	case u.Mode != "":
		p.Steps = []string{fmt.Sprintf(L.undoMode, u.Mode)}
	case u.ExitNode != "":
		p.Steps = []string{fmt.Sprintf(L.undoExit, u.ExitNode)}
	case len(u.layers()) > 0 || u.Settings != nil:
		for _, l := range u.layers() {
			if l.Absent {
				p.Steps = append(p.Steps, fmt.Sprintf(L.undoLayerDelete, name(l)))
			} else {
				p.Steps = append(p.Steps, fmt.Sprintf(L.undoLayerBack, name(l)))
			}
		}
		if u.Settings != nil {
			p.Steps = append(p.Steps, L.undoSettings)
		}
		p.Steps = append(p.Steps, L.stepApply)
	case u.HistoryID != "":
		for _, id := range legacyLayers[u.Action] {
			p.Steps = append(p.Steps, fmt.Sprintf(L.undoLayerHistory, id, u.HistoryID))
		}
		p.Steps = append(p.Steps, L.stepApply)
	}
	p.Warning = L.undoFailsAgain
	if u.Reason != "" {
		if t, ok := health.FixTextFor(f.Lang, f.Platform, u.Reason); ok && t.Plain != "" {
			p.Warning = fmt.Sprintf(L.undoFailsReason, t.Plain)
		}
	}
	return p
}
