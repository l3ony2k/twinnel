package engine

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

// HelperInfo answers helper.info (read-only).
func (e *Engine) HelperInfo(ctx context.Context) (rpc.HelperInfo, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.HelperInfo{}, err
	}
	info := rpc.HelperInfo{
		Version:       HelperVersion,
		StateDir:      e.Store.Dir,
		LogFile:       e.Store.Path("logs", "helper.log"),
		Label:         st.HelperLabel,
		Mihomo:        rpc.HelperMihomoService{Label: st.Mihomo.ServiceLabel, Adopted: st.Mihomo.ServiceAdopted, Controller: st.Mihomo.Controller},
		Subscriptions: len(st.Subscriptions),
		Layers:        len(st.Layers),
	}
	if e.Services != nil {
		if s, err := e.Services.Status(ctx, st.Mihomo.ServiceLabel); err == nil {
			info.Mihomo.Installed = s.Installed
		}
	}
	if h := st.Hardened; h != nil {
		info.Hardened = &rpc.HelperHardened{PlanID: h.PlanID, OldLabel: h.OldLabel, OldPlist: h.OldPlist}
	}
	if hist, err := e.Store.History(); err == nil {
		info.History = len(hist)
	}
	if e.Secrets != nil {
		for _, k := range st.SecretKeys() {
			if _, err := e.Secrets.Get(k); err == nil {
				info.SecretKeys = append(info.SecretKeys, k)
			} else if !errors.Is(err, store.ErrNoSecret) {
				// Unknown (keychain error): list it, a purge tries it.
				info.SecretKeys = append(info.SecretKeys, k)
			}
		}
	}
	return info, nil
}

// RestartHelper answers helper.restart: Restart (set by cmd/helper) ends
// the process shortly after the answer went out, and the service manager
// starts it again.
func (e *Engine) RestartHelper() (rpc.HelperRestartResult, error) {
	if e.Restart == nil {
		return rpc.HelperRestartResult{}, rpc.Errorf(rpc.CodeNotImplemented, "this helper cannot restart itself")
	}
	e.Log.Info("restart requested")
	restart := e.Restart
	time.AfterFunc(300*time.Millisecond, restart)
	return rpc.HelperRestartResult{PID: os.Getpid()}, nil
}
