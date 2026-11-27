package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// HelperInventory answers helper.inventory (read-only): what the helper
// installed and keeps, with sizes. The app adds the pieces on its own
// side (the app, its settings, install backups, a user LaunchAgent).
func (e *Engine) HelperInventory(ctx context.Context) (rpc.Inventory, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.Inventory{}, err
	}
	var inv rpc.Inventory
	add := func(it rpc.InventoryItem) { inv.Items = append(inv.Items, it) }
	if exe, err := os.Executable(); err == nil {
		it := rpc.InventoryItem{Kind: rpc.InvHelperBinary, Path: exe}
		if fi, err := os.Stat(exe); err == nil {
			it.Size = fi.Size()
		}
		add(it)
	}
	if e.Services != nil && st.HelperLabel != "" {
		if s, err := e.Services.Status(ctx, st.HelperLabel); err == nil && s.Definition != "" {
			add(rpc.InventoryItem{Kind: rpc.InvHelperService, Path: s.Definition, Label: st.HelperLabel})
		}
	}
	if e.Socket != "" {
		add(rpc.InventoryItem{Kind: rpc.InvHelperSocket, Path: e.Socket})
	}
	add(rpc.InventoryItem{Kind: rpc.InvStateDir, Path: e.Store.Dir, Size: dirSize(e.Store.Dir)})
	part := func(kind, sub string, count int) {
		p := e.Store.Path(sub)
		if _, err := os.Stat(p); err != nil {
			return
		}
		add(rpc.InventoryItem{Kind: kind, Path: p, Size: dirSize(p), Count: count})
	}
	part(rpc.InvSubscriptions, "subscriptions", len(st.Subscriptions))
	part(rpc.InvLayers, "layers", len(st.Layers))
	n := 0
	if h, err := e.Store.History(); err == nil {
		n = len(h)
	}
	part(rpc.InvHistory, "history", n)
	ids, _ := e.Store.TakeoverIDs()
	part(rpc.InvTakeoverBackups, "takeover", len(ids))
	part(rpc.InvKernels, "kernels", 0)
	part(rpc.InvLogs, "logs", 0)
	if info, err := e.HelperInfo(ctx); err == nil && len(info.SecretKeys) > 0 {
		add(rpc.InventoryItem{Kind: rpc.InvSecrets, Names: info.SecretKeys})
	}
	if m := st.Mihomo; m.ServiceLabel != "" && e.Services != nil {
		if s, err := e.Services.Status(ctx, m.ServiceLabel); err == nil && s.Installed {
			kind := rpc.InvMihomoService
			if m.ServiceAdopted && st.Hardened == nil {
				kind = rpc.InvAdoptedService
			}
			add(rpc.InventoryItem{Kind: kind, Path: s.Definition, Label: m.ServiceLabel})
		}
	}
	if st.Adopted != nil {
		inv.Adopted = st.Adopted.PlanID
	}
	inv.Hardened = st.Hardened != nil
	return inv, nil
}

// dirSize is the total size of the regular files under dir (symlinks not
// followed).
func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}
