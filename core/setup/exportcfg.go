package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/version"
)

// ExportConfig asks the helper for the export zip (config.export, core/
// bundle): manifest.json, config.yaml (the running config without the
// controller secret), the person's overrides and, unless
// p.NoSubscriptions, the subscriptions with their URLs. A helper from
// before config.export gets the zip built here from its lists, without
// subscriptions (their URLs never leave it). The app's 导出配置 and
// `twinnel export` both use it; `twinnel import` / 导入… read the result.
// The zip holds secrets: never log it.
func ExportConfig(ctx context.Context, hc Caller, p rpc.ConfigExportParams) (rpc.ConfigExport, error) {
	var x rpc.ConfigExport
	err := hc.Call(ctx, rpc.MethodConfigExport, p, &x)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == rpc.CodeMethodNotFound {
		x = rpc.ConfigExport{}
		x.Zip, x.Layers, err = exportFromLists(ctx, hc)
	}
	return x, err
}

// SaveExport writes zip to path, readable by its owner only (0600), with
// no half-written file left on an error.
func SaveExport(path string, zip []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, zip, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ExportFileName is the default name of an export made at t.
func ExportFileName(t time.Time) string {
	return "twinnel-config-" + t.Format("20060102") + ".zip"
}

// exportFromLists builds the zip from layer.list, layer.get and
// config.get, for a helper without config.export.
func exportFromLists(ctx context.Context, hc Caller) ([]byte, int, error) {
	// The helper's own list, not whatever a client last loaded: the zip
	// holds every override the inventory counts.
	var layers []rpc.Layer
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &layers); err != nil {
		return nil, 0, err
	}
	var live rpc.ConfigText
	if err := hc.Call(ctx, rpc.MethodConfigGet, rpc.ConfigGetParams{Source: rpc.ConfigLive}, &live); err != nil {
		return nil, 0, err
	}
	b := &bundle.Bundle{Config: live.Content}
	for _, l := range layers {
		if l.Managed {
			continue
		}
		var lc rpc.LayerContent
		if err := hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: l.ID}, &lc); err != nil {
			return nil, 0, err
		}
		b.Layers = append(b.Layers, bundle.Layer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format, Enabled: l.Enabled,
			NameCode: l.NameCode, NameArgs: l.NameArgs, Content: lc.Content})
	}
	var buf bytes.Buffer
	if err := bundle.Write(&buf, b, time.Now(), version.Version); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(b.Layers), nil
}
