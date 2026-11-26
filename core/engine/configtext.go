package engine

import (
	"context"
	"errors"
	"io/fs"

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
		raw, err := readConfined(st.Mihomo.ConfigFile)
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
