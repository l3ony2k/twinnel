package engine

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"github.com/l3ony2k/twinnel/core/takeover"
)

func reservedLayer(id string) bool { return id == CoexistLayerID || id == LeakGuardLayerID }

// Layers lists stored layers in order, then the managed ones.
func (e *Engine) Layers() ([]rpc.Layer, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return nil, err
	}
	out := []rpc.Layer{}
	for _, l := range st.Layers {
		b, _ := e.Store.ReadLayer(l.ID)
		out = append(out, rpc.Layer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format, Enabled: l.Enabled, Size: len(b),
			Hash: store.Hash(b), NameCode: l.NameCode, NameArgs: l.NameArgs})
	}
	out = append(out,
		rpc.Layer{ID: CoexistLayerID, Name: "Tailscale coexist", Stage: "coexist", Format: store.FormatMerge, Enabled: st.Tailscale.Coexist, Managed: true,
			NameCode: takeover.LayerNameCoexist},
		rpc.Layer{ID: LeakGuardLayerID, Name: "Leak guard", Stage: "guard", Format: "transform", Enabled: len(st.LeakGuardGroups) > 0, Managed: true,
			NameCode: takeover.LayerNameLeakGuard},
	)
	return out, nil
}

// Layer returns one layer with its content. For the managed coexist layer
// the content is generated from the current tailnet cache.
func (e *Engine) Layer(ctx context.Context, id string) (rpc.LayerContent, error) {
	ls, err := e.Layers()
	if err != nil {
		return rpc.LayerContent{}, err
	}
	i := slices.IndexFunc(ls, func(l rpc.Layer) bool { return l.ID == id })
	if i < 0 {
		return rpc.LayerContent{}, rpc.ErrNotFound("layer", id)
	}
	out := rpc.LayerContent{Layer: ls[i]}
	switch id {
	case CoexistLayerID:
		st, err := e.Store.Settings()
		if err != nil {
			return out, err
		}
		l, err := e.coexistLayer(ctx, st)
		if err != nil {
			return out, err
		}
		out.Content = string(l.Override)
	case LeakGuardLayerID:
		st, err := e.Store.Settings()
		if err != nil {
			return out, err
		}
		out.Content = "# Removes DIRECT from these proxy groups; a group left empty becomes REJECT.\n"
		for _, g := range st.LeakGuardGroups {
			out.Content += "# - " + g + "\n"
		}
	default:
		b, err := e.Store.ReadLayer(id)
		if err != nil {
			return out, err
		}
		out.Content = string(b)
	}
	out.Size = len(out.Content)
	return out, nil
}

// checkLayerContent parses a layer the way compose will use it.
func checkLayerContent(format, content string) error {
	b := []byte(content)
	if format == store.FormatVerge {
		var err error
		if b, err = config.ImportVergeMerge(b); err != nil {
			return err
		}
	}
	_, err := config.MergeBytes([]byte("{}\n"), b)
	return err
}

// PutLayer creates or replaces a stored layer. New layers go to the end.
func (e *Engine) PutLayer(p rpc.LayerPutParams) (rpc.Layer, error) {
	if !store.ValidID(p.ID) {
		return rpc.Layer{}, rpc.ErrInvalid("invalid id %q", p.ID)
	}
	if reservedLayer(p.ID) {
		return rpc.Layer{}, rpc.ErrInvalid("%q is a managed layer", p.ID)
	}
	if p.Stage != "" && p.Stage != store.StageBase && p.Stage != store.StageUser {
		return rpc.Layer{}, rpc.ErrInvalid("stage must be %q or %q", store.StageBase, store.StageUser)
	}
	if p.Format != "" && p.Format != store.FormatMerge && p.Format != store.FormatVerge {
		return rpc.Layer{}, rpc.ErrInvalid("format must be %q or %q", store.FormatMerge, store.FormatVerge)
	}
	var meta store.Layer
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		i := st.LayerIndex(p.ID)
		if i < 0 {
			st.Layers = append(st.Layers, store.Layer{ID: p.ID, Name: p.ID, Stage: store.StageUser, Format: store.FormatMerge, Enabled: true})
			i = len(st.Layers) - 1
		}
		l := &st.Layers[i]
		if p.Name != "" && p.Name != l.Name {
			l.Name, l.NameCode, l.NameArgs = p.Name, "", nil
		}
		if p.Stage != "" {
			l.Stage = p.Stage
		}
		if p.Format != "" {
			l.Format = p.Format
		}
		if p.Enabled != nil {
			l.Enabled = *p.Enabled
		}
		if err := checkLayerContent(l.Format, p.Content); err != nil {
			return rpc.ErrInvalid("layer %s: %v", p.ID, err)
		}
		// Written under the settings lock so metadata and content agree.
		if err := e.Store.WriteLayer(p.ID, []byte(p.Content)); err != nil {
			return err
		}
		meta = *l
		return nil
	})
	if err != nil {
		return rpc.Layer{}, err
	}
	e.publish(rpc.EventStateChanged, map[string]string{"layer": p.ID, "op": "saved", "name": meta.Name})
	return rpc.Layer{ID: meta.ID, Name: meta.Name, Stage: meta.Stage, Format: meta.Format, Enabled: meta.Enabled, Size: len(p.Content),
		NameCode: meta.NameCode, NameArgs: meta.NameArgs}, nil
}

// DeleteLayer removes a stored layer.
func (e *Engine) DeleteLayer(id string) error {
	if reservedLayer(id) {
		return rpc.ErrInvalid("%q is a managed layer; disable it in settings", id)
	}
	var name string
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		i := st.LayerIndex(id)
		if i < 0 {
			return rpc.ErrNotFound("layer", id)
		}
		name = st.Layers[i].Name
		st.Layers = append(st.Layers[:i], st.Layers[i+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	e.Store.DeleteLayer(id)
	e.publish(rpc.EventStateChanged, map[string]string{"layer": id, "op": "deleted", "name": name})
	return nil
}

// SetLayerEnabled toggles a stored layer.
func (e *Engine) SetLayerEnabled(id string, on bool) error {
	if reservedLayer(id) {
		return rpc.ErrInvalid("%q is a managed layer; use settings.update", id)
	}
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		i := st.LayerIndex(id)
		if i < 0 {
			return rpc.ErrNotFound("layer", id)
		}
		st.Layers[i].Enabled = on
		return nil
	})
	if err == nil {
		op := "off"
		if on {
			op = "on"
		}
		e.publish(rpc.EventStateChanged, map[string]string{"layer": id, "op": op})
	}
	return err
}

// ReorderLayers sets the order of stored layers; ids must list each stored
// layer exactly once.
func (e *Engine) ReorderLayers(ids []string) error {
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		if len(ids) != len(st.Layers) {
			return rpc.ErrInvalid("expected %d layer ids, got %d", len(st.Layers), len(ids))
		}
		out := make([]store.Layer, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			i := st.LayerIndex(id)
			if i < 0 || seen[id] {
				return rpc.ErrInvalid("unknown or repeated layer %q", id)
			}
			seen[id] = true
			out = append(out, st.Layers[i])
		}
		st.Layers = out
		return nil
	})
	if err == nil {
		e.publish(rpc.EventStateChanged, map[string]any{"layers": ids})
	}
	return err
}

// Settings returns the non-path settings.
func (e *Engine) Settings() (rpc.Settings, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.Settings{}, err
	}
	return toRPCSettings(st), nil
}

func toRPCSettings(st *store.Settings) rpc.Settings {
	g := st.LeakGuardGroups
	if g == nil {
		g = []string{}
	}
	return rpc.Settings{
		LeakGuardGroups: g, TailscaleCoexist: st.Tailscale.Coexist, TailscaleResolver: st.Tailscale.Resolver,
		HistoryKeep: st.HistoryKeep, KernelChannel: st.KernelChannel, AutoApply: *st.AutoApply,
		HealthEgress: st.HealthEgress,
		Drift:        toRPCDrift(st.Drift),
		Agent:        rpc.AgentSettings{MCP: st.Agent.MCP, Operate: st.Agent.OperateOn(), Configure: st.Agent.Configure},
	}
}

// UpdateSettings edits the non-path settings. Nothing is applied; call
// compose.apply afterwards.
func (e *Engine) UpdateSettings(u rpc.SettingsUpdate) (rpc.Settings, error) {
	st, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		if u.LeakGuardGroups != nil {
			var gs []string
			for _, g := range *u.LeakGuardGroups {
				if g == "" || len(g) > 128 {
					return rpc.ErrInvalid("invalid group name %q", g)
				}
				if !slices.Contains(gs, g) {
					gs = append(gs, g)
				}
			}
			st.LeakGuardGroups = gs
		}
		if u.TailscaleCoexist != nil {
			st.Tailscale.Coexist = *u.TailscaleCoexist
		}
		if u.TailscaleResolver != nil {
			r := *u.TailscaleResolver
			if len(r) > 256 || containsAny(r, "\n\r") {
				return rpc.ErrInvalid("invalid resolver")
			}
			if strings.EqualFold(strings.TrimSpace(r), "system") {
				return rpc.ErrInvalid("resolver \"system\" loops back into mihomo when mihomo is the system DNS; leave it empty for 100.100.100.100")
			}
			st.Tailscale.Resolver = r
		}
		if u.HistoryKeep != nil {
			if *u.HistoryKeep < 1 || *u.HistoryKeep > 1000 {
				return rpc.ErrInvalid("history_keep must be 1..1000")
			}
			st.HistoryKeep = *u.HistoryKeep
		}
		if u.KernelChannel != nil {
			if *u.KernelChannel != "stable" && *u.KernelChannel != "alpha" {
				return rpc.ErrInvalid("kernel_channel must be stable or alpha")
			}
			st.KernelChannel = *u.KernelChannel
		}
		if u.AutoApply != nil {
			v := *u.AutoApply
			st.AutoApply = &v
		}
		if u.HealthEgress != nil {
			st.HealthEgress = *u.HealthEgress
		}
		if a := u.Agent; a != nil {
			if a.MCP != nil {
				st.Agent.MCP = *a.MCP
			}
			if a.Operate != nil {
				v := *a.Operate
				st.Agent.Operate = &v
			}
			if a.Configure != nil {
				st.Agent.Configure = *a.Configure
			}
		}
		return nil
	})
	if err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return rpc.Settings{}, re
		}
		return rpc.Settings{}, err
	}
	e.publish(rpc.EventStateChanged, map[string]string{"settings": "updated"})
	if u.HealthEgress != nil {
		e.triggerHealth("settings") // the egress row follows the switch
	}
	return toRPCSettings(st), nil
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, d := range s {
			if c == d {
				return true
			}
		}
	}
	return false
}
