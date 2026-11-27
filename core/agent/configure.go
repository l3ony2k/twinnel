package agent

import (
	"errors"
	"fmt"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// Configure tools: change the stored configuration (off by default).
// Edits to overrides and subscriptions are stored only; nothing reaches
// mihomo until apply, which shows its preview first.

func (s *Server) registerConfigure() {
	addTool(s, rpc.TierConfigure, "layer_put", "Save override",
		`Creates or replaces an override (layer): a YAML patch on top of the subscription, in Clash Party merge
syntax (format "merge", default: keys replace, +rules / rules+ prepend / append, <key>! forces) or Clash
Verge Rev "verge" syntax (prepend-rules, append-rules, ...). Stored only: call preview, then apply.
Undo: layer_put with the old content (layers id=... shows it) or layer_remove for a new one.`,
		toolOpts{idempotent: true}, s.layerPut)

	addTool(s, rpc.TierConfigure, "layer_remove", "Remove override",
		`Deletes a stored override. Managed overrides (Tailscale coexist, leak guard) cannot be removed.
Stored only: call preview, then apply. Undo: layer_put with its content (read it first with layers id=...).`,
		toolOpts{destructive: true}, s.layerRemove)

	addTool(s, rpc.TierConfigure, "layer_enable", "Enable or disable override",
		`Turns an override on or off without deleting it. Stored only: call preview, then apply.
Undo: layer_enable with the opposite value.`,
		toolOpts{idempotent: true}, s.layerEnable)

	addTool(s, rpc.TierConfigure, "layer_order", "Reorder overrides",
		`Sets the order overrides apply in (ids: every stored override id, first applies first; base-stage
overrides still apply before user ones). Stored only: call preview, then apply. Undo: layer_order with
the previous order in the result.`,
		toolOpts{idempotent: true}, s.layerOrder)

	addTool(s, rpc.TierConfigure, "sub_put", "Save subscription",
		`Creates or changes a subscription: name, URL (http/https; kept in the system keychain and never
shown again) and update interval in minutes (0: manual). An empty url keeps the stored one. Stored only:
refresh_subscription downloads it; apply uses it when it is active.`,
		toolOpts{idempotent: true, openWorld: true}, s.subPut)

	addTool(s, rpc.TierConfigure, "sub_activate", "Activate subscription",
		`Makes a subscription the one Twinnel composes from. Stored only: call preview, then apply.
Undo: sub_activate with the previous id in the result.`,
		toolOpts{idempotent: true}, s.subActivate)

	addTool(s, rpc.TierConfigure, "sub_remove", "Remove subscription",
		`Deletes a subscription and its stored URL. The active subscription cannot be removed while it is in
use. Not undoable: the URL is gone (the person has to enter it again).`,
		toolOpts{destructive: true}, s.subRemove)

	addTool(s, rpc.TierConfigure, "apply", "Apply configuration",
		`Composes the subscription and overrides and hot-reloads mihomo with the result. Two steps, always:
call apply without preview_hash to get the preview (semantic diff against the running config, mihomo -t
validation, reload impact) and show it to the person; then call apply again with that preview_hash to
apply exactly that. A config that fails validation is never written; one that breaks mihomo after the
reload is rolled back. The result has the new history id and the id to roll back to.
Undo: history_rollback with rollback_id.`,
		toolOpts{destructive: true}, s.apply)

	addTool(s, rpc.TierConfigure, "history_rollback", "Roll back config",
		`Puts a history entry's config back and hot-reloads mihomo (the overrides stay as they are; status
then reports drift until the next apply). The current config stays in history. Undo: history_rollback
with rollback_id in the result.`,
		toolOpts{destructive: true}, s.historyRollback)

	addTool(s, rpc.TierConfigure, "settings_update", "Change settings",
		`Changes Twinnel's stored settings: leak_guard_groups (proxy groups that must never go DIRECT),
tailscale_coexist (the managed layer that keeps Tailscale working under mihomo's TUN and DNS),
tailscale_resolver, history_keep (1..1000), kernel_channel (stable or alpha) and auto_apply (re-apply
after subscription or tailnet changes). Only the fields given change. Leak guard and coexist take
effect at the next apply. The agent switches cannot be changed here. Undo: settings_update with the
previous values in the result.`,
		toolOpts{idempotent: true}, s.settingsUpdate)
}

type layerPutIn struct {
	ID      string `json:"id" jsonschema:"override id: letters, digits, - and _"`
	Name    string `json:"name,omitempty" jsonschema:"display name"`
	Stage   string `json:"stage,omitempty" jsonschema:"base or user (default user)"`
	Format  string `json:"format,omitempty" jsonschema:"merge (default) or verge"`
	Enabled *bool  `json:"enabled,omitempty" jsonschema:"default true for a new override, unchanged otherwise"`
	Content string `json:"content" jsonschema:"the YAML patch"`
}

func (s *Server) layerPut(c *call, in layerPutIn) (any, error) {
	var old rpc.LayerContent
	existed := c.rpc(rpc.MethodLayerGet, rpc.IDParams{ID: in.ID}, &old) == nil
	if existed && old.Managed {
		return nil, fmt.Errorf("%s is managed by Twinnel; change it through settings_update", in.ID)
	}
	var l rpc.Layer
	if err := c.rpc(rpc.MethodLayerPut, rpc.LayerPutParams{ID: in.ID, Name: in.Name, Stage: in.Stage, Format: in.Format, Enabled: in.Enabled, Content: in.Content}, &l); err != nil {
		return nil, err
	}
	if existed {
		c.undo = "layer_put id=" + in.ID + " with its previous content"
	} else {
		c.undo = "layer_remove id=" + in.ID
	}
	return map[string]any{"layer": l, "created": !existed, "next": "preview, then apply", "undo": c.undo}, nil
}

type idIn struct {
	ID string `json:"id"`
}

func (s *Server) layerRemove(c *call, in idIn) (any, error) {
	var old rpc.LayerContent
	if err := c.rpc(rpc.MethodLayerGet, rpc.IDParams{ID: in.ID}, &old); err != nil {
		return nil, err
	}
	if old.Managed {
		return nil, fmt.Errorf("%s is managed by Twinnel; turn it off through settings_update", in.ID)
	}
	if err := c.rpc(rpc.MethodLayerDelete, rpc.IDParams{ID: in.ID}, nil); err != nil {
		return nil, err
	}
	c.undo = "layer_put id=" + in.ID + " with the content returned here"
	return map[string]any{"removed": in.ID, "content": RedactYAML(old.Content), "name": old.Name, "stage": old.Stage,
		"format": old.Format, "next": "preview, then apply", "undo": c.undo}, nil
}

type layerEnableIn struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) layerEnable(c *call, in layerEnableIn) (any, error) {
	if err := c.rpc(rpc.MethodLayerSetEnabled, rpc.LayerEnableParams{ID: in.ID, Enabled: in.Enabled}, nil); err != nil {
		return nil, err
	}
	c.undo = fmt.Sprintf("layer_enable id=%s enabled=%v", in.ID, !in.Enabled)
	return map[string]any{"id": in.ID, "enabled": in.Enabled, "next": "preview, then apply", "undo": c.undo}, nil
}

type orderIn struct {
	IDs []string `json:"ids" jsonschema:"every stored override id, in the new order"`
}

func (s *Server) layerOrder(c *call, in orderIn) (any, error) {
	var ls []rpc.Layer
	if err := c.rpc(rpc.MethodLayerList, nil, &ls); err != nil {
		return nil, err
	}
	var prev []string
	for _, l := range ls {
		if !l.Managed {
			prev = append(prev, l.ID)
		}
	}
	if err := c.rpc(rpc.MethodLayerReorder, rpc.ReorderParams{IDs: in.IDs}, nil); err != nil {
		return nil, err
	}
	c.undo = fmt.Sprintf("layer_order ids=%v", prev)
	return map[string]any{"order": in.IDs, "previous": prev, "next": "preview, then apply", "undo": c.undo}, nil
}

type subPutIn struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	URL             string `json:"url,omitempty" jsonschema:"http(s) URL; empty keeps the stored one"`
	IntervalMinutes *int   `json:"interval_minutes,omitempty" jsonschema:"update interval; 0 = manual only"`
}

func (s *Server) subPut(c *call, in subPutIn) (any, error) {
	s.addSecret(in.URL) // never echoed, not even in an error
	var sub rpc.Subscription
	if err := c.rpc(rpc.MethodSubPut, rpc.SubPutParams{ID: in.ID, Name: in.Name, URL: in.URL, IntervalMinutes: in.IntervalMinutes}, &sub); err != nil {
		return nil, err
	}
	return map[string]any{"subscription": sub, "next": "refresh_subscription to download it"}, nil
}

func (s *Server) subActivate(c *call, in idIn) (any, error) {
	var subs []rpc.Subscription
	if err := c.rpc(rpc.MethodSubList, nil, &subs); err != nil {
		return nil, err
	}
	prev := ""
	for _, x := range subs {
		if x.Active {
			prev = x.ID
		}
	}
	if err := c.rpc(rpc.MethodSubActivate, rpc.IDParams{ID: in.ID}, nil); err != nil {
		return nil, err
	}
	if prev != "" {
		c.undo = "sub_activate id=" + prev
	}
	return map[string]any{"active": in.ID, "previous": prev, "next": "preview, then apply", "undo": c.undo}, nil
}

func (s *Server) subRemove(c *call, in idIn) (any, error) {
	if err := c.rpc(rpc.MethodSubDelete, rpc.IDParams{ID: in.ID}, nil); err != nil {
		return nil, err
	}
	return map[string]any{"removed": in.ID}, nil
}

type applyIn struct {
	PreviewHash string `json:"preview_hash,omitempty" jsonschema:"the preview_hash of the preview the person saw; empty returns the preview"`
	Reason      string `json:"reason,omitempty" jsonschema:"why, in a few words (kept in history)"`
}

func (s *Server) apply(c *call, in applyIn) (any, error) {
	p, err := c.previewNow()
	if err != nil {
		return nil, err
	}
	if in.PreviewHash == "" {
		return map[string]any{
			"applied": false, "preview": p,
			"next": "show the diff to the person; to apply exactly this, call apply with preview_hash=" + p.Hash,
		}, nil
	}
	if in.PreviewHash != p.Hash {
		return nil, fmt.Errorf("the configuration changed since that preview (now %s): call apply without preview_hash and show the new preview", p.Hash)
	}
	if !p.Valid {
		return nil, errors.New("the composed config fails mihomo -t; fix the override the preview names (culprits) and preview again")
	}
	rollback := c.latestHistory()
	reason := in.Reason
	if reason == "" {
		reason = "agent apply"
	}
	var r rpc.ApplyResult
	if err := c.rpc(rpc.MethodComposeApply, rpc.ApplyParams{Reason: clientReason(c.client, reason)}, &r); err != nil {
		return nil, err
	}
	out := applySummary(&r)
	out["applied"] = r.Written || r.Reloaded
	if rollback != "" && r.Written {
		out["rollback_id"] = rollback
		c.undo = "history_rollback id=" + rollback
		out["undo"] = c.undo
	}
	return out, nil
}

// clientReason marks a history reason as an agent's.
func clientReason(client, reason string) string {
	if client == "" {
		client = "agent"
	}
	return reason + " (via " + client + ")"
}

type rollbackIn struct {
	ID string `json:"id" jsonschema:"history id (history lists them)"`
}

func (s *Server) historyRollback(c *call, in rollbackIn) (any, error) {
	prev := c.latestHistory()
	var r rpc.ApplyResult
	if err := c.rpc(rpc.MethodHistoryRollback, rpc.IDParams{ID: in.ID}, &r); err != nil {
		return nil, err
	}
	out := applySummary(&r)
	if prev != "" {
		out["rollback_id"] = prev
		c.undo = "history_rollback id=" + prev
		out["undo"] = c.undo
	}
	return out, nil
}

type settingsIn struct {
	LeakGuardGroups   *[]string `json:"leak_guard_groups,omitempty"`
	TailscaleCoexist  *bool     `json:"tailscale_coexist,omitempty"`
	TailscaleResolver *string   `json:"tailscale_resolver,omitempty"`
	HistoryKeep       *int      `json:"history_keep,omitempty"`
	KernelChannel     *string   `json:"kernel_channel,omitempty"`
	AutoApply         *bool     `json:"auto_apply,omitempty"`
}

func (s *Server) settingsUpdate(c *call, in settingsIn) (any, error) {
	var before rpc.Settings
	if err := c.rpc(rpc.MethodSettingsGet, nil, &before); err != nil {
		return nil, err
	}
	// No Agent field: an agent never widens its own tiers.
	u := rpc.SettingsUpdate{LeakGuardGroups: in.LeakGuardGroups, TailscaleCoexist: in.TailscaleCoexist,
		TailscaleResolver: in.TailscaleResolver, HistoryKeep: in.HistoryKeep, KernelChannel: in.KernelChannel, AutoApply: in.AutoApply}
	var after rpc.Settings
	if err := c.rpc(rpc.MethodSettingsUpdate, u, &after); err != nil {
		return nil, err
	}
	before.Agent, after.Agent = rpc.AgentSettings{}, rpc.AgentSettings{}
	c.undo = "settings_update with the previous values"
	return map[string]any{"settings": after, "previous": before, "next": "preview, then apply (leak guard and coexist)", "undo": c.undo}, nil
}
