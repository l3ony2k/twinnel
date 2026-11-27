package setup

// The export zip (停止使用 → 导出配置, `twinnel setup export`) and its
// import on another machine.
//
//	config.yaml               the live config, without the controller secret
//	overrides/<id>.yaml       every stored override (managed ones excluded)
//	twinnel-export.json       the manifest: override names, stages, order,
//	                          on/off; subscriptions (with their URLs when
//	                          the export included them)
//
// Zips from before the manifest (config.yaml and overrides/ only) import
// as user overrides in file order, all enabled.
//
// Everything in the zip is a secret: config.yaml holds the nodes'
// credentials, the manifest may hold subscription URLs. Nothing here logs
// any of it.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// ManifestName is the manifest's file name in the zip.
const ManifestName = "twinnel-export.json"

// Manifest is twinnel-export.json.
type Manifest struct {
	Version       int              `json:"version"`
	Layers        []ManifestLayer  `json:"layers"`
	Subscriptions []rpc.SubExport  `json:"subscriptions,omitempty"`
	Settings      *ManifestSetting `json:"settings,omitempty"`
}

// ManifestLayer is one override's metadata.
type ManifestLayer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Stage   string `json:"stage"`
	Format  string `json:"format"`
	Enabled bool   `json:"enabled"`
}

// ManifestSetting carries the settings a new machine should start with.
type ManifestSetting struct {
	LeakGuardGroups  []string `json:"leak_guard_groups,omitempty"`
	TailscaleCoexist bool     `json:"tailscale_coexist"`
}

// Caller is the part of a helper connection the export and setup use.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// ExportOptions choose what goes into the zip.
type ExportOptions struct {
	// SubscriptionURLs adds the subscriptions' URLs (subscription.export).
	SubscriptionURLs bool
}

// WriteExport writes the export zip to w.
func WriteExport(ctx context.Context, hc Caller, w io.Writer, o ExportOptions) error {
	// The helper's own list, not whatever a client last loaded: the zip
	// holds every override the inventory counts.
	var layers []rpc.Layer
	if err := hc.Call(ctx, rpc.MethodLayerList, nil, &layers); err != nil {
		return err
	}
	var live rpc.ConfigText
	if err := hc.Call(ctx, rpc.MethodConfigGet, rpc.ConfigGetParams{Source: rpc.ConfigLive}, &live); err != nil {
		return err
	}
	m := Manifest{Version: 1, Layers: []ManifestLayer{}}
	var st rpc.Settings
	if err := hc.Call(ctx, rpc.MethodSettingsGet, nil, &st); err == nil {
		m.Settings = &ManifestSetting{LeakGuardGroups: st.LeakGuardGroups, TailscaleCoexist: st.TailscaleCoexist}
	}
	if o.SubscriptionURLs {
		if err := hc.Call(ctx, rpc.MethodSubExport, nil, &m.Subscriptions); err != nil {
			return fmt.Errorf("subscription URLs: %w", err)
		}
	} else {
		var subs []rpc.Subscription
		if err := hc.Call(ctx, rpc.MethodSubList, nil, &subs); err == nil {
			for _, s := range subs {
				m.Subscriptions = append(m.Subscriptions, rpc.SubExport{ID: s.ID, Name: s.Name, Active: s.Active, IntervalMinutes: s.IntervalMinutes})
			}
		}
	}
	zw := zip.NewWriter(w)
	put := func(name string, content []byte) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(content)
		return err
	}
	err := put("config.yaml", []byte(live.Content))
	for _, l := range layers {
		if err != nil || l.Managed {
			continue
		}
		var lc rpc.LayerContent
		if err = hc.Call(ctx, rpc.MethodLayerGet, rpc.IDParams{ID: l.ID}, &lc); err == nil {
			err = put("overrides/"+l.ID+".yaml", []byte(lc.Content))
			m.Layers = append(m.Layers, ManifestLayer{ID: l.ID, Name: l.Name, Stage: l.Stage, Format: l.Format, Enabled: l.Enabled})
		}
	}
	if err == nil {
		var b []byte
		b, err = json.MarshalIndent(m, "", "  ")
		if err == nil {
			err = put(ManifestName, append(b, '\n'))
		}
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	return err
}

// Import is an export zip, read.
type Import struct {
	Layers []ImportLayer
	// Subscriptions from the manifest; URL empty when the export had none.
	Subscriptions []rpc.SubExport
	// Config is the exported live config: the offline fallback when no
	// subscription URL came with the zip (its proxies, as a manual
	// subscription).
	Config   []byte
	Settings *ManifestSetting
	// Manifest: the zip had twinnel-export.json.
	Manifest bool
}

// ImportLayer is one override to restore.
type ImportLayer struct {
	ManifestLayer
	Content string
}

// HasURL reports whether any subscription came with its URL.
func (im *Import) HasURL() bool {
	return slices.ContainsFunc(im.Subscriptions, func(s rpc.SubExport) bool { return s.URL != "" })
}

// HasBase reports whether an override is base settings (stage base).
func (im *Import) HasBase() bool {
	return slices.ContainsFunc(im.Layers, func(l ImportLayer) bool { return l.Stage == store.StageBase && l.Enabled })
}

const maxImportFile = 16 << 20

// ReadExport reads an export zip.
func ReadExport(r io.ReaderAt, size int64) (*Import, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("not a Twinnel export: %w", err)
	}
	im := &Import{}
	files := map[string][]byte{}
	var order []string
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			continue
		}
		if name != "config.yaml" && name != ManifestName && !(path.Dir(name) == "overrides" && strings.HasSuffix(name, ".yaml")) {
			continue
		}
		if f.UncompressedSize64 > maxImportFile {
			return nil, fmt.Errorf("%s is too large", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxImportFile+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		files[name] = b
		order = append(order, name)
	}
	im.Config = files["config.yaml"]
	if b, ok := files[ManifestName]; ok {
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", ManifestName, err)
		}
		if m.Version != 1 {
			return nil, fmt.Errorf("%s: version %d is newer than this Twinnel", ManifestName, m.Version)
		}
		im.Manifest, im.Subscriptions, im.Settings = true, m.Subscriptions, m.Settings
		for _, l := range m.Layers {
			c, ok := files["overrides/"+l.ID+".yaml"]
			if !ok || !store.ValidID(l.ID) {
				continue
			}
			im.Layers = append(im.Layers, ImportLayer{ManifestLayer: l, Content: string(c)})
		}
	} else {
		for _, name := range order {
			if path.Dir(name) != "overrides" {
				continue
			}
			id := strings.TrimSuffix(path.Base(name), ".yaml")
			if !store.ValidID(id) {
				continue
			}
			im.Layers = append(im.Layers, ImportLayer{
				ManifestLayer: ManifestLayer{ID: id, Name: id, Stage: store.StageUser, Format: store.FormatMerge, Enabled: true},
				Content:       string(files[name]),
			})
		}
	}
	if len(im.Layers) == 0 && len(im.Config) == 0 && !im.HasURL() {
		return nil, errors.New("not a Twinnel export: no config.yaml, overrides or subscriptions")
	}
	return im, nil
}
