package setup

import (
	"context"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// ImportConfig is Env.ImportConfig on the helper's config.import: it stores
// the export's subscriptions and the person's overrides (ids already in use
// keep what is here), then downloads the active subscription when the zip
// brought its URL but not its content. Nothing is applied; the flow's
// config step does that. The zip and the URLs in it are never logged.
func ImportConfig(ctx context.Context, hc Caller, zip []byte) (ImportResult, error) {
	var r rpc.ConfigImportResult
	p := rpc.ConfigImportParams{Data: zip, Name: "export.zip", OnConflict: rpc.ImportSkip}
	if err := hc.Call(ctx, rpc.MethodConfigImport, p, &r); err != nil {
		return ImportResult{}, err
	}
	out := ImportResult{LeakGuardGroups: r.LeakGuardGroups}
	var subs []rpc.Subscription
	if err := hc.Call(ctx, rpc.MethodSubList, nil, &subs); err != nil {
		return out, err
	}
	for _, s := range subs {
		if !s.Active {
			continue
		}
		if s.Size == 0 && s.HasURL {
			var rr rpc.SubRefreshResult
			if err := hc.Call(ctx, rpc.MethodSubRefresh, rpc.IDParams{ID: s.ID}, &rr); err != nil {
				return out, downloadErr(err)
			}
		}
		out.Subscription = s.Size > 0 || s.HasURL
	}
	return out, nil
}
