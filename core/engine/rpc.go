package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

// Register wires every allowlisted method to the engine.
func (e *Engine) Register(s *rpc.Server) {
	rpc.Typed(s, rpc.MethodStatus, func(ctx context.Context, _ rpc.Empty) (rpc.Status, error) { return e.Status(ctx) })
	rpc.Typed(s, rpc.MethodHelperInfo, func(ctx context.Context, _ rpc.Empty) (rpc.HelperInfo, error) { return e.HelperInfo(ctx) })
	rpc.Typed(s, rpc.MethodHelperInventory, func(ctx context.Context, _ rpc.Empty) (rpc.Inventory, error) { return e.HelperInventory(ctx) })
	rpc.Typed(s, rpc.MethodHelperRestart, func(ctx context.Context, _ rpc.Empty) (rpc.HelperRestartResult, error) {
		return e.RestartHelper()
	})

	rpc.Typed(s, rpc.MethodComposeStatus, func(ctx context.Context, _ rpc.Empty) (rpc.ComposeStatus, error) { return e.ComposeStatus(ctx) })
	rpc.Typed(s, rpc.MethodComposePreview, func(ctx context.Context, _ rpc.Empty) (rpc.ComposeResult, error) { return e.Preview(ctx) })
	rpc.Typed(s, rpc.MethodComposeApply, func(ctx context.Context, p rpc.ApplyParams) (*rpc.ApplyResult, error) {
		// No reason: Apply records the code "apply" (a client words it).
		return withResult(e.Apply(ctx, p))
	})

	rpc.Typed(s, rpc.MethodHistoryList, func(ctx context.Context, _ rpc.Empty) ([]rpc.HistoryEntry, error) { return e.History() })
	rpc.Typed(s, rpc.MethodHistoryRollback, func(ctx context.Context, p rpc.IDParams) (*rpc.ApplyResult, error) {
		return withResult(e.Rollback(ctx, p.ID))
	})

	rpc.Typed(s, rpc.MethodLayerList, func(ctx context.Context, _ rpc.Empty) ([]rpc.Layer, error) { return e.Layers() })
	rpc.Typed(s, rpc.MethodLayerGet, func(ctx context.Context, p rpc.IDParams) (rpc.LayerContent, error) { return e.Layer(ctx, p.ID) })
	rpc.Typed(s, rpc.MethodLayerPut, func(ctx context.Context, p rpc.LayerPutParams) (rpc.Layer, error) { return e.PutLayer(p) })
	rpc.Typed(s, rpc.MethodLayerDelete, func(ctx context.Context, p rpc.IDParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.DeleteLayer(p.ID)
	})
	rpc.Typed(s, rpc.MethodLayerSetEnabled, func(ctx context.Context, p rpc.LayerEnableParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.SetLayerEnabled(p.ID, p.Enabled)
	})
	rpc.Typed(s, rpc.MethodLayerReorder, func(ctx context.Context, p rpc.ReorderParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.ReorderLayers(p.IDs)
	})
	rpc.Typed(s, rpc.MethodLayerRestore, func(ctx context.Context, p rpc.LayerRestoreParams) (rpc.LayerRestoreResult, error) {
		return e.RestoreLayers(ctx, p)
	})

	rpc.Typed(s, rpc.MethodSubList, func(ctx context.Context, _ rpc.Empty) ([]rpc.Subscription, error) { return e.Subscriptions() })
	rpc.Typed(s, rpc.MethodSubPut, func(ctx context.Context, p rpc.SubPutParams) (rpc.Subscription, error) {
		return e.PutSubscription(ctx, p)
	})
	rpc.Typed(s, rpc.MethodSubDelete, func(ctx context.Context, p rpc.IDParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.DeleteSubscription(p.ID)
	})
	rpc.Typed(s, rpc.MethodSubActivate, func(ctx context.Context, p rpc.IDParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.ActivateSubscription(p.ID)
	})
	rpc.Typed(s, rpc.MethodSubRefresh, func(ctx context.Context, p rpc.IDParams) (rpc.SubRefreshResult, error) {
		return e.RefreshSubscription(ctx, p.ID)
	})

	rpc.Typed(s, rpc.MethodSettingsGet, func(ctx context.Context, _ rpc.Empty) (rpc.Settings, error) { return e.Settings() })
	rpc.Typed(s, rpc.MethodSettingsUpdate, func(ctx context.Context, u rpc.SettingsUpdate) (rpc.Settings, error) {
		return e.UpdateSettings(u)
	})

	rpc.Typed(s, rpc.MethodTailnetRefresh, func(ctx context.Context, _ rpc.Empty) (rpc.TailnetRefreshResult, error) {
		return e.RefreshTailnet(ctx)
	})

	rpc.Typed(s, rpc.MethodKernelList, func(ctx context.Context, _ rpc.Empty) (rpc.KernelList, error) { return e.KernelList() })
	rpc.Typed(s, rpc.MethodKernelCheck, func(ctx context.Context, p rpc.KernelCheckParams) (rpc.KernelRelease, error) {
		return e.KernelCheck(ctx, p.Channel)
	})
	rpc.Typed(s, rpc.MethodKernelInstall, func(ctx context.Context, p rpc.KernelInstallParams) (rpc.KernelList, error) {
		return e.KernelInstall(ctx, p)
	})
	rpc.Typed(s, rpc.MethodKernelRollback, func(ctx context.Context, _ rpc.Empty) (rpc.KernelList, error) { return e.KernelRollback() })

	rpc.Typed(s, rpc.MethodTailscaleInfo, func(ctx context.Context, _ rpc.Empty) (rpc.TailscaleInfo, error) {
		return e.TailscaleInfo(ctx), nil
	})
	rpc.Typed(s, rpc.MethodServiceLog, func(_ context.Context, p rpc.ServiceLogParams) (rpc.ServiceLog, error) { return e.ServiceLog(p) })
	rpc.Typed(s, rpc.MethodServiceStatus, func(ctx context.Context, p rpc.ServiceParams) (rpc.ServiceStatus, error) {
		return e.ServiceStatus(ctx, p.Name)
	})

	for method, op := range map[string]service.Op{
		rpc.MethodServiceStart: service.OpStart, rpc.MethodServiceStop: service.OpStop,
		rpc.MethodServiceRestart: service.OpRestart, rpc.MethodServiceInstall: service.OpInstall,
		rpc.MethodServiceUninstall: service.OpUninstall,
	} {
		rpc.Typed(s, method, func(ctx context.Context, p rpc.ServiceParams) (rpc.ServiceOpResult, error) {
			return e.ServiceOp(ctx, p.Name, op)
		})
	}

	rpc.Typed(s, rpc.MethodGeoStatus, func(context.Context, rpc.Empty) (rpc.AccessoryCheck, error) { return e.GeoStatus() })
	rpc.Typed(s, rpc.MethodGeoCheck, func(ctx context.Context, p rpc.AccessoryCheckParams) (rpc.AccessoryCheck, error) {
		return e.GeoCheck(ctx, p)
	})
	rpc.Typed(s, rpc.MethodGeoUpdate, func(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
		return e.GeoUpdate(ctx, p)
	})
	rpc.Typed(s, rpc.MethodUIStatus, func(context.Context, rpc.Empty) (rpc.AccessoryCheck, error) { return e.UIStatus() })
	rpc.Typed(s, rpc.MethodUICheck, func(ctx context.Context, p rpc.AccessoryCheckParams) (rpc.AccessoryCheck, error) {
		return e.UICheck(ctx, p)
	})
	rpc.Typed(s, rpc.MethodUIUpdate, func(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
		return e.UIUpdate(ctx, p)
	})

	rpc.Typed(s, rpc.MethodConfigGet, func(ctx context.Context, p rpc.ConfigGetParams) (rpc.ConfigText, error) {
		return e.ConfigText(ctx, p)
	})
	rpc.Typed(s, rpc.MethodTakeoverPlan, func(ctx context.Context, p rpc.TakeoverPlanParams) (rpc.TakeoverPlan, error) {
		return e.TakeoverPlan(ctx, p)
	})
	rpc.Typed(s, rpc.MethodTakeoverPlans, func(ctx context.Context, p rpc.TakeoverPlansParams) ([]rpc.TakeoverPlan, error) {
		if p.PlanID != "" {
			pl, err := e.TakeoverGet(p.PlanID)
			if err != nil {
				return nil, err
			}
			return []rpc.TakeoverPlan{pl}, nil
		}
		return e.TakeoverPlans()
	})
	rpc.Typed(s, rpc.MethodTakeoverDiscard, func(ctx context.Context, p rpc.TakeoverDiscardParams) (rpc.Empty, error) {
		return rpc.Empty{}, e.TakeoverDiscard(p)
	})
	rpc.Typed(s, rpc.MethodTakeoverAdopt, func(ctx context.Context, p rpc.AdoptParams) (rpc.AdoptResult, error) {
		return e.Adopt(ctx, p)
	})
	rpc.Typed(s, rpc.MethodTakeoverUndo, func(ctx context.Context, p rpc.TakeoverUndoParams) (rpc.TakeoverUndoResult, error) {
		return e.TakeoverUndo(ctx, p)
	})
	rpc.Typed(s, rpc.MethodControllerSecure, func(ctx context.Context, _ rpc.Empty) (rpc.ControllerSecureResult, error) {
		return e.SecureController(ctx)
	})
	rpc.Typed(s, rpc.MethodControllerMove, func(ctx context.Context, p rpc.ControllerMoveParams) (rpc.ControllerMoveResult, error) {
		return e.MoveController(ctx, p)
	})
	rpc.Typed(s, rpc.MethodHardenPlan, func(ctx context.Context, p rpc.HardenPlanParams) (rpc.HardenPlan, error) {
		return e.HardenPlan(ctx, p)
	})
	rpc.Typed(s, rpc.MethodHardenPlans, func(ctx context.Context, p rpc.HardenPlansParams) ([]rpc.HardenPlan, error) {
		return e.HardenPlans(p.PlanID)
	})
	rpc.Typed(s, rpc.MethodHardenApply, func(ctx context.Context, p rpc.HardenApplyParams) (rpc.HardenResult, error) {
		return e.HardenApply(ctx, p)
	})
	rpc.Typed(s, rpc.MethodDNSStatus, func(ctx context.Context, _ rpc.Empty) (rpc.DNSStatus, error) { return e.DNSStatus(ctx) })
	rpc.Typed(s, rpc.MethodDNSPoint, func(ctx context.Context, _ rpc.Empty) (rpc.DNSStatus, error) { return e.DNSPoint(ctx) })
	rpc.Typed(s, rpc.MethodDNSRestore, func(ctx context.Context, _ rpc.Empty) (rpc.DNSStatus, error) { return e.DNSRestore(ctx) })
	rpc.Typed(s, rpc.MethodDNSBackup, func(ctx context.Context, _ rpc.Empty) (rpc.DNSStatus, error) { return e.DNSBackup(ctx) })
	rpc.Typed(s, rpc.MethodTailscaleInstall, func(ctx context.Context, p rpc.TailscaleInstallParams) (rpc.TailscaleInstallResult, error) {
		return e.TailscaleInstall(ctx, p)
	})
	rpc.Typed(s, rpc.MethodTailscaleLogin, func(ctx context.Context, p rpc.TailscaleLoginParams) (rpc.TailscaleLoginResult, error) {
		return e.TailscaleLogin(ctx, p)
	})
	rpc.Typed(s, rpc.MethodAutostartList, func(ctx context.Context, _ rpc.Empty) (rpc.AutostartList, error) { return e.AutostartList(ctx), nil })
	rpc.Typed(s, rpc.MethodAutostartChange, func(ctx context.Context, p rpc.AutostartChangeParams) (rpc.AutostartChangeResult, error) {
		return e.AutostartChange(ctx, p)
	})
	e.registerCredentials(s)
	e.registerAgent(s)
	e.registerImport(s)

	rpc.Typed(s, rpc.MethodHealthLast, func(ctx context.Context, p rpc.HealthLastParams) (rpc.HealthReport, error) {
		return e.withEgressIPs(e.HealthLast(p.Lang))(p.IPs)
	})
	rpc.Typed(s, rpc.MethodHealthRun, func(ctx context.Context, p rpc.HealthRunParams) (rpc.HealthReport, error) {
		return e.withEgressIPs(e.HealthRun(ctx, p.Reason, p.Lang))(p.IPs)
	})
	rpc.Typed(s, rpc.MethodHealthEgress, func(ctx context.Context, p rpc.HealthEgressParams) (rpc.HealthReport, error) {
		return e.withEgressIPs(e.HealthEgress(ctx, p.Lang))(p.IPs)
	})

	rpc.Typed(s, rpc.MethodEventsWait, func(ctx context.Context, p rpc.EventsWaitParams) (rpc.EventsWaitResult, error) {
		t := time.Duration(p.TimeoutMS) * time.Millisecond
		if t <= 0 {
			t = 25 * time.Second
		}
		t = min(t, 2*time.Minute)
		evs, next := e.Bus.Wait(ctx, p.After, t)
		if evs == nil {
			evs = []rpc.Event{}
		}
		return rpc.EventsWaitResult{Events: evs, Next: next}, nil
	})
}

// withResult keeps a failed apply's result with its error: the RPC error
// carries it as data, so a client can show what failed and where (the
// validator's lines, a restored config) and not only the message.
func withResult(r *rpc.ApplyResult, err error) (*rpc.ApplyResult, error) {
	if err == nil || r == nil {
		return r, err
	}
	b, merr := json.Marshal(r)
	if merr != nil {
		return nil, err
	}
	re := &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}
	var orig *rpc.Error
	if errors.As(err, &orig) {
		re.Code = orig.Code
	}
	re.Data = b
	return nil, re
}

// registerImport wires config.export and config.import.
func (e *Engine) registerImport(s *rpc.Server) {
	rpc.Typed(s, rpc.MethodConfigExport, func(ctx context.Context, p rpc.ConfigExportParams) (rpc.ConfigExport, error) {
		return e.Export(ctx, p)
	})
	rpc.Typed(s, rpc.MethodConfigImport, func(ctx context.Context, p rpc.ConfigImportParams) (rpc.ConfigImportResult, error) {
		return e.Import(ctx, p)
	})
}
