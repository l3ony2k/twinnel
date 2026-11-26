package engine

import (
	"context"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
)

// Register wires every allowlisted method to the engine.
func (e *Engine) Register(s *rpc.Server) {
	rpc.Typed(s, rpc.MethodStatus, func(ctx context.Context, _ rpc.Empty) (rpc.Status, error) { return e.Status(ctx) })

	rpc.Typed(s, rpc.MethodComposePreview, func(ctx context.Context, _ rpc.Empty) (rpc.ComposeResult, error) { return e.Preview(ctx) })
	rpc.Typed(s, rpc.MethodComposeApply, func(ctx context.Context, p rpc.ApplyParams) (*rpc.ApplyResult, error) {
		if p.Reason == "" {
			p.Reason = "manual"
		}
		return e.Apply(ctx, p)
	})

	rpc.Typed(s, rpc.MethodHistoryList, func(ctx context.Context, _ rpc.Empty) ([]rpc.HistoryEntry, error) { return e.History() })
	rpc.Typed(s, rpc.MethodHistoryRollback, func(ctx context.Context, p rpc.IDParams) (*rpc.ApplyResult, error) {
		return e.Rollback(ctx, p.ID)
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

	rpc.Typed(s, rpc.MethodGeoCheck, func(ctx context.Context, _ rpc.Empty) (rpc.AccessoryCheck, error) { return e.GeoCheck(ctx) })
	rpc.Typed(s, rpc.MethodGeoUpdate, func(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
		return e.GeoUpdate(ctx, p)
	})
	rpc.Typed(s, rpc.MethodUICheck, func(ctx context.Context, _ rpc.Empty) (rpc.AccessoryCheck, error) { return e.UICheck(ctx) })
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
	rpc.Typed(s, rpc.MethodControllerSecure, func(ctx context.Context, _ rpc.Empty) (rpc.ControllerSecureResult, error) {
		return e.SecureController(ctx)
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
	e.registerCredentials(s)

	rpc.Typed(s, rpc.MethodHealthLast, func(ctx context.Context, _ rpc.Empty) (rpc.HealthReport, error) { return e.HealthLast() })
	rpc.Typed(s, rpc.MethodHealthRun, func(ctx context.Context, p rpc.HealthRunParams) (rpc.HealthReport, error) {
		return e.HealthRun(ctx, p.Reason)
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
