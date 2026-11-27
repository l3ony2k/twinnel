package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/version"
)

// Export builds the export zip (config.export). Subscription URLs and
// downloads are in it unless p.NoSubscriptions; never log the result.
func (e *Engine) Export(ctx context.Context, p rpc.ConfigExportParams) (rpc.ConfigExport, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.ConfigExport{}, err
	}
	b := &bundle.Bundle{LeakGuardGroups: st.LeakGuardGroups}
	if live, err := e.ConfigText(ctx, rpc.ConfigGetParams{Source: rpc.ConfigLive}); err == nil {
		b.Config = live.Content
	} else if re := (*rpc.Error)(nil); !errors.As(err, &re) || re.Code != rpc.CodeNotFound {
		return rpc.ConfigExport{}, err // a fresh machine has no config yet; anything else is an error
	}
	for _, l := range st.Layers {
		c, err := e.Store.ReadLayer(l.ID)
		if err != nil && !errors.Is(err, store.ErrNotExist) {
			return rpc.ConfigExport{}, err
		}
		b.Layers = append(b.Layers, bundle.Layer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format, Enabled: l.Enabled,
			NameCode: l.NameCode, NameArgs: l.NameArgs, Content: string(c)})
	}
	if !p.NoSubscriptions {
		for _, s := range st.Subscriptions {
			u, _ := e.Secrets.Get(store.SubscriptionURLKey(s.ID))
			c, _ := e.Store.ReadSubscription(s.ID)
			if u == "" && len(c) == 0 {
				continue // nothing to bring: neither a URL nor a download
			}
			b.Subscriptions = append(b.Subscriptions, bundle.Subscription{ID: s.ID, Name: s.Name, Active: s.Active,
				IntervalMinutes: s.IntervalMinutes, URL: u, Content: string(c)})
		}
	}
	out := rpc.ConfigExport{Layers: len(b.Layers), Subscriptions: len(b.Subscriptions)}
	write := func() ([]byte, error) {
		var buf bytes.Buffer
		err := bundle.Write(&buf, b, e.now(), version.Version)
		return buf.Bytes(), err
	}
	z, err := write()
	if err == nil && len(z) > bundle.MaxFile {
		// The downloads make it too big to import again: keep the URLs.
		kept := b.Subscriptions[:0:0]
		for _, s := range b.Subscriptions {
			s.Content = ""
			if s.URL != "" {
				kept = append(kept, s)
			}
		}
		b.Subscriptions, out.Subscriptions, out.NoContent = kept, len(kept), true
		z, err = write()
	}
	if err != nil {
		return rpc.ConfigExport{}, err
	}
	if len(z) > bundle.MaxFile {
		return rpc.ConfigExport{}, rpc.Errorf(rpc.CodeFailed, "the export is larger than %d MiB", bundle.MaxFile>>20)
	}
	out.Zip = z
	return out, nil
}

// importPlan is what an import does against st.
type importPlan struct {
	res       rpc.ConfigImportResult
	layers    []*bundle.Layer        // parallel to the layer items in res.Items
	subs      []*bundle.Subscription // parallel to the subscription items
	layerIdx  []int                  // index of each layer's item in res.Items
	subIdx    []int
	activate  string // new id of the subscription to make active
	conflicts int
}

// Import stores what a file brings (config.import). See rpc.MethodConfigImport.
func (e *Engine) Import(ctx context.Context, p rpc.ConfigImportParams) (rpc.ConfigImportResult, error) {
	if err := checkResolution(p.OnConflict); err != nil {
		return rpc.ConfigImportResult{}, err
	}
	for k, v := range p.Resolve {
		if err := checkResolution(v); err != nil {
			return rpc.ConfigImportResult{}, rpc.ErrInvalid("resolve %s: %v", k, err)
		}
	}
	b, err := bundle.Parse(p.Name, p.Data)
	if err != nil {
		return rpc.ConfigImportResult{}, rpc.ErrInvalid("%v", err)
	}
	if p.DryRun {
		st, err := e.Store.Settings()
		if err != nil {
			return rpc.ConfigImportResult{}, err
		}
		pl, err := e.planImport(b, st, p)
		if err != nil {
			return rpc.ConfigImportResult{}, err
		}
		pl.res.DryRun = true
		return pl.res, nil
	}
	var pl *importPlan
	_, err = e.Store.UpdateSettings(func(st *store.Settings) error {
		// Planned again under the lock, so what is written matches the
		// settings it is written into.
		var err error
		if pl, err = e.planImport(b, st, p); err != nil {
			return err
		}
		if pl.conflicts > 0 {
			data, _ := json.Marshal(pl.res)
			return &rpc.Error{Code: rpc.CodeConflict, Message: "the file has items whose ids are taken here: choose replace, skip or rename for each", Data: data}
		}
		return e.storeImport(st, pl)
	})
	if err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return rpc.ConfigImportResult{}, re
		}
		return rpc.ConfigImportResult{}, err
	}
	if pl.res.Stored > 0 || pl.res.Activated != "" {
		e.publish(rpc.EventStateChanged, map[string]any{"import": pl.res.Source, "stored": pl.res.Stored})
	}
	return pl.res, nil
}

func checkResolution(v string) error {
	switch v {
	case "", rpc.ImportAsk, rpc.ImportReplace, rpc.ImportSkip, rpc.ImportRename:
		return nil
	}
	return rpc.ErrInvalid("unknown conflict resolution %q (ask, replace, skip, rename)", v)
}

// resolution is the action for a conflicting item, or "" to ask.
func resolution(p rpc.ConfigImportParams, kind, id string) string {
	r, ok := p.Resolve[kind+":"+id]
	if !ok {
		r = p.OnConflict
	}
	if r == rpc.ImportAsk {
		r = ""
	}
	return r
}

// planImport decides each item's action against st. It reads the store
// (layer contents, URLs) but writes nothing.
func (e *Engine) planImport(b *bundle.Bundle, st *store.Settings, p rpc.ConfigImportParams) (*importPlan, error) {
	pl := &importPlan{res: rpc.ConfigImportResult{Source: b.Source, Version: b.Version, App: b.App, Items: []rpc.ImportItem{},
		LeakGuardGroups: b.LeakGuardGroups}}
	// Ids in use: local ones and every id the file brings, so a rename
	// never lands on an item imported in the same go.
	layerTaken := map[string]bool{CoexistLayerID: true, LeakGuardLayerID: true}
	for _, l := range st.Layers {
		layerTaken[l.ID] = true
	}
	for _, l := range b.Layers {
		layerTaken[l.ID] = true
	}
	subTaken := map[string]bool{}
	for _, s := range st.Subscriptions {
		subTaken[s.ID] = true
	}
	for _, s := range b.Subscriptions {
		subTaken[s.ID] = true
	}

	for i := range b.Layers {
		l := &b.Layers[i]
		it := rpc.ImportItem{Kind: rpc.ImportKindLayer, ID: l.ID, Name: l.Name, NewID: l.ID, Stage: l.Stage, Format: l.Format,
			Enabled: l.Enabled, Size: len(l.Content)}
		j := st.LayerIndex(l.ID)
		generated := reservedLayer(l.ID) || l.Stage == store.StageBase
		var bad error
		if !generated {
			bad = checkLayerContent(l.Format, l.Content)
		}
		switch {
		case generated:
			it.Action, it.NewID, it.Reason = rpc.ImportGenerated, "", "rebuilt on this machine"
		case bad != nil:
			it.Action, it.NewID, it.Reason = rpc.ImportInvalid, "", bad.Error()
		case j < 0:
			it.Action = rpc.ImportAdd
		default:
			loc := st.Layers[j]
			it.Existing = loc.Name
			cur, _ := e.Store.ReadLayer(l.ID)
			if string(cur) == l.Content && loc.Format == l.Format && loc.Name == l.Name && loc.Enabled == l.Enabled && loc.Stage == l.Stage {
				it.Action = rpc.ImportUnchanged
				break
			}
			it.Action = rpc.ImportConflict
			switch r := resolution(p, rpc.ImportKindLayer, l.ID); r {
			case "":
				pl.conflicts++
			case rpc.ImportRename:
				it.Action, it.NewID = r, bundle.FreeID(l.ID, func(id string) bool { return layerTaken[id] })
				layerTaken[it.NewID] = true
			default:
				it.Action = r
			}
		}
		if it.Action == rpc.ImportSkip || it.Action == rpc.ImportUnchanged {
			it.NewID = l.ID
		}
		pl.layerIdx = append(pl.layerIdx, len(pl.res.Items))
		pl.layers = append(pl.layers, l)
		pl.res.Items = append(pl.res.Items, it)
	}

	plainHTTP, noContent := false, false
	for i := range b.Subscriptions {
		s := &b.Subscriptions[i]
		it := rpc.ImportItem{Kind: rpc.ImportKindSubscription, ID: s.ID, Name: s.Name, NewID: s.ID, Active: s.Active,
			HasURL: s.URL != "", HasContent: s.Content != "", IntervalMinutes: s.IntervalMinutes, Size: len(s.Content)}
		var bad error
		if s.URL != "" {
			bad = checkSubscriptionURL(s.URL)
		}
		if bad == nil && s.Content != "" {
			bad = checkSubscriptionContent([]byte(s.Content))
		}
		j := st.SubscriptionIndex(s.ID)
		switch {
		case bad != nil:
			it.Action, it.NewID, it.Reason = rpc.ImportInvalid, "", bad.Error()
		case j < 0:
			it.Action = rpc.ImportAdd
		default:
			loc := st.Subscriptions[j]
			it.Existing = loc.Name
			curURL, _ := e.Secrets.Get(store.SubscriptionURLKey(s.ID))
			sameContent := s.Content == "" || store.Hash([]byte(s.Content)) == loc.Hash
			if curURL == s.URL && sameContent && loc.Name == s.Name && loc.IntervalMinutes == importInterval(s.IntervalMinutes) {
				it.Action = rpc.ImportUnchanged
				break
			}
			it.Action = rpc.ImportConflict
			switch r := resolution(p, rpc.ImportKindSubscription, s.ID); r {
			case "":
				pl.conflicts++
			case rpc.ImportRename:
				it.Action, it.NewID = r, bundle.FreeID(s.ID, func(id string) bool { return subTaken[id] })
				subTaken[it.NewID] = true
			default:
				it.Action = r
			}
		}
		if stores(it.Action) {
			plainHTTP = plainHTTP || plainHTTPWarning(s.URL) != ""
			noContent = noContent || s.Content == ""
		}
		pl.subIdx = append(pl.subIdx, len(pl.res.Items))
		pl.subs = append(pl.subs, s)
		pl.res.Items = append(pl.res.Items, it)
	}

	// Activation: only when nothing is active here, the file's active
	// subscription (or its first) becomes active.
	if _, ok := st.ActiveSubscription(); !ok {
		first := ""
		for k, s := range pl.subs {
			it := pl.res.Items[pl.subIdx[k]]
			if !stores(it.Action) && it.Action != rpc.ImportUnchanged && it.Action != rpc.ImportSkip {
				continue
			}
			if first == "" {
				first = it.NewID
			}
			if s.Active && pl.activate == "" {
				pl.activate = it.NewID
			}
		}
		if pl.activate == "" {
			pl.activate = first
		}
		pl.res.Activated = pl.activate
	}

	pl.res.Conflicts = pl.conflicts
	if b.HasSecrets() {
		pl.res.Warnings = append(pl.res.Warnings, rpc.ImportWarnSecrets)
	}
	if plainHTTP {
		pl.res.Warnings = append(pl.res.Warnings, rpc.ImportWarnPlainHTTP)
	}
	if noContent {
		pl.res.Warnings = append(pl.res.Warnings, rpc.ImportWarnNoContent)
	}
	if len(st.Subscriptions) == 0 && pl.activate == "" {
		pl.res.Warnings = append(pl.res.Warnings, rpc.ImportWarnNoSubscriptions)
	}
	return pl, nil
}

// stores reports whether an action writes the item.
func stores(action string) bool {
	return action == rpc.ImportAdd || action == rpc.ImportReplace || action == rpc.ImportRename
}

// importInterval keeps an interval subscription.put would take: 0
// (manual) or at least 5 minutes.
func importInterval(m int) int {
	if m <= 0 {
		return 0
	}
	return max(m, 5)
}

// storeImport writes a conflict-free plan into st (under the settings
// lock) and the store. New layers go to the end in the file's order; a
// replaced one keeps its place.
func (e *Engine) storeImport(st *store.Settings, pl *importPlan) error {
	now := e.now().UTC()
	for k, l := range pl.layers {
		it := &pl.res.Items[pl.layerIdx[k]]
		if !stores(it.Action) {
			continue
		}
		if err := e.Store.WriteLayer(it.NewID, []byte(l.Content)); err != nil {
			return err
		}
		meta := store.Layer{ID: it.NewID, Name: l.Name, Stage: store.StageUser, Format: l.Format, Enabled: l.Enabled,
			NameCode: l.NameCode, NameArgs: slices.Clone(l.NameArgs)}
		if j := st.LayerIndex(it.NewID); j >= 0 {
			st.Layers[j] = meta
		} else {
			st.Layers = append(st.Layers, meta)
		}
		pl.res.Stored++
	}
	for k, s := range pl.subs {
		it := &pl.res.Items[pl.subIdx[k]]
		if !stores(it.Action) {
			continue
		}
		key := store.SubscriptionURLKey(it.NewID)
		if s.URL != "" {
			if err := e.Secrets.Set(key, s.URL); err != nil {
				return err
			}
		} else if it.Action == rpc.ImportReplace {
			e.Secrets.Delete(key) // the file's subscription has no URL: the old one must not refresh over it
		}
		j := st.SubscriptionIndex(it.NewID)
		if j < 0 {
			st.Subscriptions = append(st.Subscriptions, store.Subscription{ID: it.NewID})
			j = len(st.Subscriptions) - 1
		}
		sub := &st.Subscriptions[j]
		sub.Name, sub.IntervalMinutes = s.Name, importInterval(s.IntervalMinutes)
		if s.Content != "" {
			if err := e.Store.WriteSubscription(it.NewID, []byte(s.Content)); err != nil {
				return err
			}
			h := store.Hash([]byte(s.Content))
			if h != sub.Hash {
				sub.LastChange = now
			}
			sub.Hash, sub.Size, sub.LastFetch, sub.LastError = h, len(s.Content), now, ""
		} else if it.Action == rpc.ImportReplace {
			sub.LastError = ""
		}
		pl.res.Stored++
	}
	if pl.activate != "" {
		for i := range st.Subscriptions {
			st.Subscriptions[i].Active = st.Subscriptions[i].ID == pl.activate
		}
	}
	return nil
}
