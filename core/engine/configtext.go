package engine

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

// ConfigText returns a config as text for diffs, always without the
// top-level secret (the form history keeps): the live file, the config
// compose.apply would write now, or a history entry.
func (e *Engine) ConfigText(ctx context.Context, p rpc.ConfigGetParams) (rpc.ConfigText, error) {
	out := rpc.ConfigText{Source: p.Source, ID: p.ID}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	var b []byte
	switch p.Source {
	case rpc.ConfigLive:
		raw, err := os.ReadFile(st.Mihomo.ConfigFile)
		if errors.Is(err, fs.ErrNotExist) {
			return out, rpc.ErrNotFound("config file", st.Mihomo.ConfigFile)
		}
		if err != nil {
			return out, err
		}
		if b, _, err = withoutSecret(raw); err != nil {
			// Not YAML we can re-encode: drop the secret line only.
			b = stripSecretBytes(raw)
		}
	case rpc.ConfigPreview:
		res, _, err := e.compose(ctx, st)
		if err != nil {
			return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
		}
		b = res.Config
	case rpc.ConfigHistory:
		if p.ID == "" {
			return out, rpc.ErrInvalid("source history needs an id")
		}
		data, _, err := e.Store.ReadHistory(p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotExist) {
				return out, rpc.ErrNotFound("history entry", p.ID)
			}
			return out, rpc.ErrInvalid("%v", err)
		}
		b = data
	default:
		return out, rpc.ErrInvalid("unknown source %q (live, preview, history)", p.Source)
	}
	out.Content, out.Hash, out.Size = string(b), store.Hash(b), len(b)
	return out, nil
}

// Adopt will take over an existing installation from a plan the helper
// generated itself with core/takeover (plan files under
// <state-dir>/takeover/<plan_id>/, never a path from the client).
//
// TODO(takeover): load the plan, re-verify it against the live files
// (hashes recorded in the plan), import base/layers/subscription into the
// store, point settings at the adopted binary/config/labels, then
// compose.apply. Not implemented yet; the method exists so the app can be
// wired.
func (e *Engine) Adopt(ctx context.Context, p rpc.AdoptParams) (rpc.AdoptResult, error) {
	out := rpc.AdoptResult{PlanID: p.PlanID}
	if !store.ValidID(p.PlanID) {
		return out, rpc.ErrInvalid("invalid plan_id %q", p.PlanID)
	}
	return out, rpc.Errorf(rpc.CodeNotImplemented, "takeover.adopt is not implemented yet")
}
