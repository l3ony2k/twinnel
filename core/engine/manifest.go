package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// The layer manifest of applied history entries (U41): what a config was
// composed from, so a rollback can tell when mihomo runs something the
// overrides do not describe (Settings.Drift), and the overrides can be set
// back to an entry (layer.restore).

// manifest describes the overrides st composes. With keep, each stored
// layer's content is kept under history/layers/ so it can be restored.
func (e *Engine) manifest(st *store.Settings, keep bool) (*store.LayerManifest, error) {
	m := &store.LayerManifest{Layers: []store.ManifestLayer{}, Coexist: st.Tailscale.Coexist, LeakGuard: slices.Clone(st.LeakGuardGroups)}
	if sub, ok := st.ActiveSubscription(); ok {
		m.Subscription = sub.ID
	}
	for _, l := range st.Layers {
		b, err := e.Store.ReadLayer(l.ID)
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", l.ID, err)
		}
		h := store.Hash(b)
		if keep {
			if h, err = e.Store.SaveLayerBlob(b); err != nil {
				return nil, fmt.Errorf("keep layer %s: %w", l.ID, err)
			}
		}
		ml := store.ManifestLayer{Layer: l, Hash: h}
		ml.NameArgs = slices.Clone(l.NameArgs)
		m.Layers = append(m.Layers, ml)
	}
	return m, nil
}

func toRPCManifest(m *store.LayerManifest) *rpc.LayerManifest {
	if m == nil {
		return nil
	}
	out := &rpc.LayerManifest{Subscription: m.Subscription, Layers: []rpc.ManifestLayer{}, Coexist: m.Coexist, LeakGuard: m.LeakGuard}
	for _, l := range m.Layers {
		out.Layers = append(out.Layers, rpc.ManifestLayer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format, Enabled: l.Enabled,
			Hash: l.Hash, NameCode: l.NameCode, NameArgs: l.NameArgs})
	}
	return out
}

func toRPCDrift(d *store.Drift) *rpc.Drift {
	if d == nil {
		return nil
	}
	return &rpc.Drift{Since: d.Since, HistoryID: d.HistoryID, Manifest: d.Manifest}
}

// setDrift records (or clears, with d nil) that mihomo runs a history
// entry the overrides do not compose.
func (e *Engine) setDrift(d *store.Drift) {
	changed := false
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		if (st.Drift == nil) == (d == nil) && (d == nil || *st.Drift == *d) {
			return nil
		}
		st.Drift, changed = d, true
		return nil
	})
	if err != nil {
		e.log().Warn("drift", "err", err)
		return
	}
	if changed {
		e.publish(rpc.EventStateChanged, map[string]any{"drift": d != nil})
	}
}

// RestoreLayers sets the overrides back to a history entry's manifest:
// every stored layer (content, name, stage, format, switch and order), the
// managed layers' switches and the active subscription; layers the entry
// did not have are deleted. With IDs, only those layers are set back, in
// place. Nothing is applied; the drift mark is cleared when everything was
// restored.
func (e *Engine) RestoreLayers(ctx context.Context, p rpc.LayerRestoreParams) (rpc.LayerRestoreResult, error) {
	out := rpc.LayerRestoreResult{Restored: []string{}}
	h, ok, err := e.Store.FindHistory(p.HistoryID)
	if err != nil {
		return out, err
	}
	if !ok {
		return out, rpc.ErrNotFound("history entry", p.HistoryID)
	}
	m := h.Layers
	if m == nil {
		return out, rpc.ErrInvalid("history entry %s has no layer manifest", p.HistoryID)
	}
	contents := map[string][]byte{}
	for _, l := range m.Layers {
		if len(p.IDs) > 0 && !slices.Contains(p.IDs, l.ID) {
			continue
		}
		b, err := e.Store.ReadLayerBlob(l.Hash)
		if err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "layer %s: the content of entry %s is gone: %v", l.ID, p.HistoryID, err)
		}
		contents[l.ID] = b
	}
	for _, id := range p.IDs {
		if _, ok := contents[id]; !ok && id != CoexistLayerID {
			return out, rpc.ErrInvalid("layer %s is not in history entry %s", id, p.HistoryID)
		}
	}
	all := len(p.IDs) == 0
	var removed []string
	_, err = e.Store.UpdateSettings(func(st *store.Settings) error {
		removed = nil
		if all {
			for _, l := range st.Layers {
				if _, ok := contents[l.ID]; !ok {
					removed = append(removed, l.ID)
				}
			}
			st.Layers = st.Layers[:0]
			for _, l := range m.Layers {
				st.Layers = append(st.Layers, l.Layer)
			}
			st.Tailscale.Coexist = m.Coexist
			st.LeakGuardGroups = slices.Clone(m.LeakGuard)
			if m.Subscription != "" && st.SubscriptionIndex(m.Subscription) >= 0 {
				for i := range st.Subscriptions {
					st.Subscriptions[i].Active = st.Subscriptions[i].ID == m.Subscription
				}
			}
			st.Drift = nil
		} else {
			for _, l := range m.Layers {
				if _, ok := contents[l.ID]; !ok {
					continue
				}
				if i := st.LayerIndex(l.ID); i >= 0 {
					st.Layers[i] = l.Layer
				} else {
					st.Layers = append(st.Layers, l.Layer)
				}
			}
			if slices.Contains(p.IDs, CoexistLayerID) {
				st.Tailscale.Coexist = m.Coexist
			}
		}
		for id, b := range contents {
			if err := e.Store.WriteLayer(id, b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, id := range removed {
		if err := e.Store.DeleteLayer(id); err != nil && !errors.Is(err, store.ErrNotExist) {
			e.log().Warn("restore: delete layer", "id", id, "err", err)
		}
	}
	for _, l := range m.Layers {
		if _, ok := contents[l.ID]; ok {
			out.Restored = append(out.Restored, l.ID)
		}
	}
	out.Removed = removed
	e.publish(rpc.EventStateChanged, map[string]any{"restored": p.HistoryID, "at": e.now().UTC().Format(time.RFC3339)})
	return out, nil
}
