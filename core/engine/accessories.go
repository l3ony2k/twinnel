package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"gopkg.in/yaml.v3"
)

var accessoryMu sync.Mutex

// accessorySettings are the parts of the running config that locate the
// accessories.
type accessorySettings struct {
	geox                   map[string]string
	ui, uiName, uiURL      string
	hasConfig, hasUIConfig bool
}

func readAccessorySettings(cfgFile string) accessorySettings {
	var a accessorySettings
	b, err := readConfined(cfgFile)
	if err != nil {
		return a
	}
	root, err := config.Parse(b)
	if err != nil {
		return a
	}
	a.hasConfig = true
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		switch {
		case k == "geox-url" && v.Kind == yaml.MappingNode:
			a.geox = map[string]string{}
			for j := 0; j+1 < len(v.Content); j += 2 {
				a.geox[strings.ToLower(v.Content[j].Value)] = v.Content[j+1].Value
			}
		case v.Kind != yaml.ScalarNode:
		case k == "external-ui":
			a.ui, a.hasUIConfig = v.Value, true
		case k == "external-ui-name":
			a.uiName = v.Value
		case k == "external-ui-url":
			a.uiURL = v.Value
		}
	}
	return a
}

func (e *Engine) fetcher(ctx context.Context, st *store.Settings) *kernel.Fetcher {
	f := &kernel.Fetcher{API: e.GitHubAPI}
	f.HTTP, _ = e.downloadClient(ctx, st)
	return f
}

// inside reports whether p is dir or below it.
func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ---- geo databases ----

func (e *Engine) geoItems(st *store.Settings) []kernel.GeoItem {
	return kernel.GeoItems(st.Mihomo.ConfigDir, readAccessorySettings(st.Mihomo.ConfigFile).geox)
}

func (e *Engine) geoCheck(ctx context.Context, st *store.Settings) ([]rpc.AccessoryFile, []kernel.Remote) {
	f := e.fetcher(ctx, st)
	items := e.geoItems(st)
	files := make([]rpc.AccessoryFile, len(items))
	remotes := make([]kernel.Remote, len(items))
	for i, it := range items {
		af := rpc.AccessoryFile{Name: it.Name, File: it.File, URL: redactURL(it.URL)}
		if sum, n, err := kernel.FileSHA256(it.File); err == nil {
			af.Installed, af.SHA256, af.Size = true, sum, n
		}
		r, err := f.Resolve(ctx, it.URL)
		remotes[i] = r
		if err != nil {
			af.Error = err.Error()
		}
		af.Remote, af.RemoteSize, af.Verifiable = r.SHA256, r.Size, r.SHA256 != ""
		af.UpdateAvailable = err == nil && (!af.Installed || (af.Verifiable && af.Remote != af.SHA256))
		files[i] = af
	}
	return files, remotes
}

// GeoCheck compares the local geo databases with the published ones
// (read-only: GitHub API requests only).
func (e *Engine) GeoCheck(ctx context.Context) (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	files, _ := e.geoCheck(ctx, st)
	return rpc.AccessoryCheck{Dir: st.Mihomo.ConfigDir, Files: files}, nil
}

// GeoUpdate downloads every database that is missing or outdated (all of
// them with force), verifies them, swaps them in as a set and, when a
// config and a mihomo binary exist, re-validates the running config with
// the new files (mihomo -t); if that fails, the old files are put back.
// mihomo picks the new files up at its next reload or restart.
func (e *Engine) GeoUpdate(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
	accessoryMu.Lock()
	defer accessoryMu.Unlock()
	out := rpc.AccessoryUpdateResult{Updated: []string{}}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(st.Mihomo.ConfigDir, 0o755); err != nil {
		return out, err
	}
	dir, err := openConfDir(st.Mihomo.ConfigDir, st.Mihomo.ConfigDirUID)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	defer dir.Close()
	files, remotes := e.geoCheck(ctx, st)
	items := e.geoItems(st)
	var swaps []*kernel.Swap
	var names []string
	fail := func(err error) (rpc.AccessoryUpdateResult, error) {
		kernel.Discard(swaps)
		out.Files, out.Error = files, err.Error()
		e.log().Warn("geo update failed", "err", err)
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	for i, it := range items {
		if files[i].Error != "" {
			return fail(fmt.Errorf("%s: %s", it.Name, files[i].Error))
		}
		if !p.Force && !files[i].UpdateAvailable {
			out.Skipped = append(out.Skipped, it.Name)
			continue
		}
		sw, _, err := e.fetcher(ctx, st).PrepareGeo(ctx, dir.Root, it, remotes[i])
		if err != nil {
			return fail(fmt.Errorf("%s: %w", it.Name, err))
		}
		swaps = append(swaps, sw)
		names = append(names, it.Name)
	}
	if len(swaps) == 0 {
		out.Files = files
		return out, nil
	}
	if err := kernel.Commit(swaps); err != nil {
		out.RolledBack = true
		return fail(err)
	}
	if err := e.revalidate(ctx, st); err != nil {
		kernel.Rollback(swaps)
		out.RolledBack = true
		return fail(fmt.Errorf("mihomo -t failed with the new geo files, old files restored: %w", err))
	}
	out.Updated = names
	out.ReloadNeeded = true
	out.Files, _ = e.geoCheck(ctx, st)
	e.publish(rpc.EventAccessoryChanged, map[string]any{"geo": names})
	e.log().Info("geo databases updated", "files", names)
	return out, nil
}

// revalidate runs mihomo -t on the config on disk with the config dir as
// geo dir. Without a config or a binary there is nothing to check.
func (e *Engine) revalidate(ctx context.Context, st *store.Settings) error {
	cfg, err := readConfined(st.Mihomo.ConfigFile)
	if err != nil {
		return nil
	}
	bin, err := e.mihomoBinary(st)
	if err != nil {
		return nil
	}
	v, err := e.validate(ctx, bin, cfg, e.validateOpts(st)...)
	if err != nil {
		return err
	}
	if !v.OK {
		return fmt.Errorf("%v", v.Errors)
	}
	return nil
}

// ---- external UI ----

// uiTarget is the UI dir and the bundle URL from the running config:
// <config dir>/<external-ui>[/<external-ui-name>], which must stay inside
// the config dir.
func (e *Engine) uiTarget(st *store.Settings) (dir, src string, err error) {
	a := readAccessorySettings(st.Mihomo.ConfigFile)
	if !a.hasConfig {
		return "", "", errors.New("no config file yet: apply a config first")
	}
	if !a.hasUIConfig || strings.TrimSpace(a.ui) == "" {
		return "", "", errors.New("the config has no external-ui; add e.g. `external-ui: ui` in a layer")
	}
	dir = a.ui
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(st.Mihomo.ConfigDir, dir)
	}
	if a.uiName != "" {
		dir = filepath.Join(dir, a.uiName)
	}
	dir = filepath.Clean(dir)
	if !inside(st.Mihomo.ConfigDir, dir) || dir == filepath.Clean(st.Mihomo.ConfigDir) {
		return "", "", fmt.Errorf("external-ui %q is outside the config dir %s", dir, st.Mihomo.ConfigDir)
	}
	src = a.uiURL
	if src == "" {
		src = kernel.DefaultUIURL
	}
	return dir, src, nil
}

func (e *Engine) uiCheck(ctx context.Context, st *store.Settings) (rpc.AccessoryFile, kernel.Remote, error) {
	dir, src, err := e.uiTarget(st)
	af := rpc.AccessoryFile{Name: "ui", File: dir, URL: redactURL(src)}
	if err != nil {
		return af, kernel.Remote{}, err
	}
	if fi, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !fi.IsDir() {
		af.Installed = true
	}
	var stamp kernel.UIStampData
	stamped := false
	if r, err := os.OpenRoot(st.Mihomo.ConfigDir); err == nil {
		if rel, err := filepath.Rel(st.Mihomo.ConfigDir, dir); err == nil {
			stamp, stamped = kernel.ReadUIStamp(r, rel)
		}
		r.Close()
	}
	if stamped {
		af.SHA256 = stamp.Tag
		if af.SHA256 == "" {
			af.SHA256 = stamp.SHA256
		}
	}
	r, rerr := e.fetcher(ctx, st).Resolve(ctx, src)
	if rerr != nil {
		af.Error = rerr.Error()
	}
	af.Remote, af.RemoteSize, af.Verifiable = r.Tag, r.Size, r.SHA256 != ""
	if af.Remote == "" {
		af.Remote = r.SHA256
	}
	// Up to date only when the stamp says this exact bundle (same URL and
	// digest) is installed; a UI installed by hand counts as outdated.
	current := stamped && af.Installed && stamp.URL == src && r.SHA256 != "" && stamp.SHA256 == r.SHA256
	af.UpdateAvailable = rerr == nil && !current
	return af, r, nil
}

// UICheck compares the installed external UI with the published bundle.
func (e *Engine) UICheck(ctx context.Context) (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	af, _, err := e.uiCheck(ctx, st)
	if err != nil {
		af.Error = err.Error()
	}
	return rpc.AccessoryCheck{Dir: st.Mihomo.ConfigDir, Files: []rpc.AccessoryFile{af}}, nil
}

// UIUpdate downloads and verifies the UI bundle, extracts it next to the
// UI dir and swaps the dirs (the old one stays as <dir>.twinnel-bak). The
// controller serves the new files right away.
func (e *Engine) UIUpdate(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
	accessoryMu.Lock()
	defer accessoryMu.Unlock()
	out := rpc.AccessoryUpdateResult{Updated: []string{}}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	af, r, err := e.uiCheck(ctx, st)
	out.Files = []rpc.AccessoryFile{af}
	if err == nil && af.Error != "" {
		err = errors.New(af.Error)
	}
	if err != nil {
		out.Error = err.Error()
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	if !p.Force && !af.UpdateAvailable {
		out.Skipped = []string{"ui"}
		return out, nil
	}
	cd, err := openConfDir(st.Mihomo.ConfigDir, st.Mihomo.ConfigDirUID)
	if err != nil {
		out.Error = err.Error()
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	defer cd.Close()
	rel, err := cd.Rel(af.File)
	if err != nil {
		out.Error = err.Error()
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	sw, _, err := e.fetcher(ctx, st).PrepareUI(ctx, cd.Root, rel, r)
	if err == nil {
		if err = kernel.Commit([]*kernel.Swap{sw}); err != nil {
			out.RolledBack = true
		}
	}
	if err != nil {
		out.Error = err.Error()
		e.log().Warn("ui update failed", "err", err)
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	out.Updated = []string{"ui"}
	af, _, _ = e.uiCheck(ctx, st)
	out.Files = []rpc.AccessoryFile{af}
	e.publish(rpc.EventAccessoryChanged, map[string]any{"ui": af.Remote})
	e.log().Info("external ui updated", "dir", af.File, "version", af.Remote)
	return out, nil
}

// redactURL hides a URL's query (it may carry a token).
func redactURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i] + "?…"
	}
	return u
}
