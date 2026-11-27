package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"gopkg.in/yaml.v3"
)

// The geo databases and the web dashboard (external UI) in mihomo's
// config dir.
//
// geo.status / ui.status read only the disk: what is installed, when and
// from where (Twinnel's record in accessories.json), and the last check's
// answer. geo.check / ui.check ask GitHub and keep that answer, so the
// 内核 page can always say "已是最新 · 检查于 HH:MM" or offer 更新
// without asking again. geo.update / ui.update download, verify, swap in
// and (Reload) have mihomo load the new files, reporting each stage as
// accessory.progress events.

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

// fetcher returns a Fetcher through the download route proxy (rpc.Proxy*
// or a proxy URL), how it goes, and a stop function.
func (e *Engine) fetcher(ctx context.Context, st *store.Settings, proxy string) (*kernel.Fetcher, string, func(), error) {
	cl, via, stop, err := e.downloadVia(ctx, st, proxy)
	if err != nil {
		return nil, "", stop, err
	}
	return &kernel.Fetcher{API: e.GitHubAPI, HTTP: cl}, via, stop, nil
}

// inside reports whether p is dir or below it.
func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ---- the record: accessories.json in the state dir ----

// accRecord is what Twinnel installed.
type accRecord struct {
	SHA256 string    `json:"sha256"`
	Tag    string    `json:"tag,omitempty"`
	URL    string    `json:"url"`
	At     time.Time `json:"at"`
}

// accRemote is one file's answer from the last check.
type accRemote struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Error  string `json:"error,omitempty"`
}

type accKind struct {
	CheckedAt time.Time            `json:"checked_at,omitzero"`
	Via       string               `json:"via,omitempty"`
	Remote    map[string]accRemote `json:"remote,omitempty"`
	Installed map[string]accRecord `json:"installed,omitempty"`
	LastAuto  time.Time            `json:"last_auto,omitzero"`
	AutoError string               `json:"auto_error,omitempty"`
}

type accState struct {
	Geo accKind `json:"geo"`
	UI  accKind `json:"ui"`
}

var accStateMu sync.Mutex

func (e *Engine) accStatePath() string { return e.Store.Path("accessories.json") }

func (e *Engine) readAccState() accState {
	accStateMu.Lock()
	defer accStateMu.Unlock()
	var s accState
	if b, err := os.ReadFile(e.accStatePath()); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

// editAccState changes the record (a failure to save is only logged:
// the record is a convenience, the files are the truth).
func (e *Engine) editAccState(fn func(s *accState)) {
	accStateMu.Lock()
	defer accStateMu.Unlock()
	var s accState
	if b, err := os.ReadFile(e.accStatePath()); err == nil {
		json.Unmarshal(b, &s)
	}
	fn(&s)
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := e.accStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		err = os.Rename(tmp, e.accStatePath())
		if err != nil {
			e.log().Warn("accessories record", "err", err)
		}
	}
}

// ---- local hashes, cached by size and time ----

type hashKey struct {
	path string
	size int64
	mod  time.Time
}

var (
	hashMu    sync.Mutex
	hashCache = map[hashKey]string{}
)

// localFile stats p and returns its sha256 (cached while its size and
// time stay the same).
func localFile(p string) (sum string, size int64, mod time.Time, ok bool) {
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", 0, time.Time{}, false
	}
	k := hashKey{p, fi.Size(), fi.ModTime()}
	hashMu.Lock()
	sum, hit := hashCache[k]
	hashMu.Unlock()
	if !hit {
		s, _, err := kernel.FileSHA256(p)
		if err != nil {
			return "", 0, time.Time{}, false
		}
		sum = s
		hashMu.Lock()
		if len(hashCache) > 64 {
			clear(hashCache)
		}
		hashCache[k] = sum
		hashMu.Unlock()
	}
	return sum, fi.Size(), fi.ModTime(), true
}

// ---- progress ----

// accProgress publishes accessory.progress, download bytes at most
// every 200ms.
type accProgress struct {
	e           *Engine
	kind        string
	mu          sync.Mutex
	name        string
	done, total int64
	last        time.Time
}

func (p *accProgress) stage(stage, name string) {
	p.mu.Lock()
	p.name = name
	d := rpc.AccessoryProgress{Kind: p.kind, Stage: stage, Name: name, Done: p.done, Total: p.total}
	p.mu.Unlock()
	p.e.publish(rpc.EventAccessoryProgress, d)
}

func (p *accProgress) bytes(n int64) {
	p.mu.Lock()
	p.done += n
	now := time.Now()
	if now.Sub(p.last) < 200*time.Millisecond {
		p.mu.Unlock()
		return
	}
	p.last = now
	d := rpc.AccessoryProgress{Kind: p.kind, Stage: rpc.AccStageDownload, Name: p.name, Done: p.done, Total: p.total}
	p.mu.Unlock()
	p.e.publish(rpc.EventAccessoryProgress, d)
}

// ---- geo databases ----

func (e *Engine) geoItems(st *store.Settings) []kernel.GeoItem {
	return kernel.GeoItems(st.Mihomo.ConfigDir, readAccessorySettings(st.Mihomo.ConfigFile).geox)
}

// geoFiles describes the databases from the disk, the record and the
// last check (no network). checked is false when the last check did not
// cover these URLs.
func (e *Engine) geoFiles(st *store.Settings, rec accKind) (files []rpc.AccessoryFile, checked bool) {
	items := e.geoItems(st)
	files = make([]rpc.AccessoryFile, len(items))
	checked = !rec.CheckedAt.IsZero()
	for i, it := range items {
		af := rpc.AccessoryFile{Name: it.Name, File: it.File, URL: redactURL(it.URL)}
		if sum, n, mod, ok := localFile(it.File); ok {
			af.Installed, af.SHA256, af.Size, af.ModTime = true, sum, n, mod.UTC()
			if r, ok := rec.Installed[it.Name]; ok && r.SHA256 == sum {
				af.Version, af.Source = r.Tag, redactURL(r.URL)
			}
		}
		r, ok := rec.Remote[it.Name]
		if !ok || r.URL != it.URL {
			checked = false
			af.UpdateAvailable = !af.Installed
			files[i] = af
			continue
		}
		af.Remote, af.RemoteSize, af.RemoteTag, af.Error = r.SHA256, r.Size, r.Tag, r.Error
		af.Verifiable = r.SHA256 != ""
		af.UpdateAvailable = r.Error == "" && (!af.Installed || (af.Verifiable && af.Remote != af.SHA256))
		files[i] = af
	}
	return files, checked
}

func (e *Engine) geoResult(st *store.Settings, rec accKind) rpc.AccessoryCheck {
	files, checked := e.geoFiles(st, rec)
	auto := geoAuto(st)
	return rpc.AccessoryCheck{Dir: st.Mihomo.ConfigDir, Files: files, CheckedAt: rec.CheckedAt, Checked: checked,
		Via: rec.Via, AutoUpdate: auto, LastAuto: rec.LastAuto, AutoError: rec.AutoError}
}

func geoAuto(st *store.Settings) string {
	if st.GeoAutoUpdate == rpc.GeoAutoWeekly {
		return rpc.GeoAutoWeekly
	}
	return rpc.GeoAutoOff
}

// geoResolve asks GitHub for every database and keeps the answer.
func (e *Engine) geoResolve(ctx context.Context, f *kernel.Fetcher, st *store.Settings, via string) []kernel.Remote {
	items := e.geoItems(st)
	remotes := make([]kernel.Remote, len(items))
	got := map[string]accRemote{}
	for i, it := range items {
		r, err := f.Resolve(ctx, it.URL)
		remotes[i] = r
		ar := accRemote{URL: it.URL, SHA256: r.SHA256, Tag: r.Tag, Size: r.Size}
		if err != nil {
			ar.Error = err.Error()
		}
		got[it.Name] = ar
	}
	e.editAccState(func(s *accState) {
		s.Geo.CheckedAt, s.Geo.Via, s.Geo.Remote = e.now().UTC(), via, got
	})
	return remotes
}

// GeoStatus describes the local geo databases and the last check's
// answer, without any network request.
func (e *Engine) GeoStatus() (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	return e.geoResult(st, e.readAccState().Geo), nil
}

// GeoCheck compares the local geo databases with the published ones
// (read-only: GitHub API requests only).
func (e *Engine) GeoCheck(ctx context.Context, p rpc.AccessoryCheckParams) (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	f, via, stop, err := e.fetcher(ctx, st, p.Proxy)
	defer stop()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	e.geoResolve(ctx, f, st, via)
	return e.geoResult(st, e.readAccState().Geo), nil
}

// GeoUpdate downloads every database that is missing or outdated (all of
// them with force), verifies them, swaps them in as a set and, when a
// config and a mihomo binary exist, re-validates the running config with
// the new files (mihomo -t); if that fails, the old files are put back.
// A database mihomo -t already downloaded into the validation cache (a
// fresh install) is copied from there when it matches the published
// digest, not downloaded again. With Reload mihomo loads the new files at
// once; else at its next reload or restart.
func (e *Engine) GeoUpdate(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
	accessoryMu.Lock()
	defer accessoryMu.Unlock()
	out := rpc.AccessoryUpdateResult{Updated: []string{}}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	if st.Mihomo.ConfigDir == "" {
		return out, rpc.Errorf(rpc.CodeFailed, "no mihomo config dir in the settings")
	}
	if err := os.MkdirAll(st.Mihomo.ConfigDir, 0o755); err != nil {
		return out, err
	}
	dir, err := openConfDir(st.Mihomo.ConfigDir, st.Mihomo.ConfigDirUID)
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	defer dir.Close()
	f, via, stop, err := e.fetcher(ctx, st, p.Proxy)
	defer stop()
	if err != nil {
		return out, err
	}
	out.Via = via
	prog := &accProgress{e: e, kind: rpc.AccessoryGeo}
	f.OnBytes = prog.bytes
	prog.stage(rpc.AccStageDownload, "")
	remotes := e.geoResolve(ctx, f, st, via)
	files, _ := e.geoFiles(st, e.readAccState().Geo)
	items := e.geoItems(st)
	var swaps []*kernel.Swap
	var names []string
	fail := func(err error) (rpc.AccessoryUpdateResult, error) {
		kernel.Discard(swaps)
		out.Files, out.Error = files, err.Error()
		e.log().Warn("geo update failed", "err", err)
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	var todo []int
	for i, it := range items {
		if files[i].Error != "" {
			return fail(fmt.Errorf("%s: %s", it.Name, files[i].Error))
		}
		if !p.Force && !files[i].UpdateAvailable {
			out.Skipped = append(out.Skipped, it.Name)
			continue
		}
		todo = append(todo, i)
		prog.total += remotes[i].Size
	}
	cache := e.Store.Path("validate", "geo")
	for _, i := range todo {
		it := items[i]
		if !files[i].Installed {
			if sw, err := kernel.PrepareGeoFrom(dir.Root, it, filepath.Join(cache, it.Rel), remotes[i].SHA256); err == nil {
				swaps, names = append(swaps, sw), append(names, it.Name)
				prog.bytes(remotes[i].Size)
				continue
			}
		}
		prog.stage(rpc.AccStageDownload, it.Name)
		sw, _, err := f.PrepareGeo(ctx, dir.Root, it, remotes[i])
		if err != nil {
			return fail(fmt.Errorf("%s: %w", it.Name, err))
		}
		swaps, names = append(swaps, sw), append(names, it.Name)
	}
	if len(swaps) == 0 {
		out.Files = files
		return out, nil
	}
	prog.stage(rpc.AccStageInstall, "")
	if err := kernel.Commit(swaps); err != nil {
		out.RolledBack = true
		return fail(err)
	}
	prog.stage(rpc.AccStageVerify, "")
	if err := e.revalidate(ctx, st); err != nil {
		kernel.Rollback(swaps)
		out.RolledBack = true
		return fail(fmt.Errorf("mihomo -t failed with the new geo files, old files restored: %w", err))
	}
	now := e.now().UTC()
	e.editAccState(func(s *accState) {
		if s.Geo.Installed == nil {
			s.Geo.Installed = map[string]accRecord{}
		}
		for _, i := range todo {
			if sum, _, _, ok := localFile(items[i].File); ok {
				s.Geo.Installed[items[i].Name] = accRecord{SHA256: sum, Tag: remotes[i].Tag, URL: items[i].URL, At: now}
			}
		}
	})
	out.Updated = names
	out.ReloadNeeded = true
	if p.Reload {
		prog.stage(rpc.AccStageReload, "")
		e.reloadForAccessories(ctx, st, &out)
	}
	out.Files, _ = e.geoFiles(st, e.readAccState().Geo)
	e.publish(rpc.EventAccessoryChanged, map[string]any{"geo": names})
	e.log().Info("geo databases updated", "files", names, "via", via, "reloaded", out.Reloaded)
	return out, nil
}

// reloadForAccessories has mihomo load the config on disk again, so it
// reads the new geo files. Not running: nothing to do (it reads them when
// it starts). A config Twinnel does not manage yet is left alone.
func (e *Engine) reloadForAccessories(ctx context.Context, st *store.Settings, out *rpc.AccessoryUpdateResult) {
	if st.Adopted == nil && !e.managesConfig(st) {
		out.ReloadError = "mihomo runs a config Twinnel does not manage yet; restart mihomo to use the new files"
		return
	}
	if _, err := os.Stat(st.Mihomo.ConfigFile); err != nil {
		out.ReloadNeeded = false // no config yet: mihomo reads them when it starts
		return
	}
	ctrl := e.currentController(st)
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err := ctrl.Version(vctx)
	cancel()
	if err != nil {
		out.ReloadNeeded = false // not running: it reads them when it starts
		return
	}
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := reloadWith(rctx, ctrl, st.Mihomo.ConfigFile, false); err != nil {
		out.ReloadError = err.Error()
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := ctrl.Version(vctx)
		cancel()
		if err == nil {
			out.Reloaded, out.ReloadNeeded = true, false
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			out.ReloadError = "mihomo did not answer after the reload: " + err.Error()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
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

// geoAutoEvery is how often the weekly schedule updates.
const geoAutoEvery = 7 * 24 * time.Hour

// geoAutoLoop updates the geo databases weekly when geo_auto_update is
// on: checked hourly, run when the last update (by the schedule or by
// hand) is a week old; after a failure it waits a day.
func (e *Engine) geoAutoLoop(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		e.geoAutoDue(ctx)
	}
}

func (e *Engine) geoAutoDue(ctx context.Context) {
	st, err := e.Store.Settings()
	if err != nil || geoAuto(st) != rpc.GeoAutoWeekly || st.Mihomo.ConfigDir == "" {
		return
	}
	s := e.readAccState().Geo
	last := s.LastAuto
	for _, r := range s.Installed {
		if r.At.After(last) {
			last = r.At
		}
	}
	wait := geoAutoEvery
	if s.AutoError != "" {
		wait = 24 * time.Hour
	}
	if !last.IsZero() && e.now().Sub(last) < wait {
		return
	}
	_, err = e.GeoUpdate(ctx, rpc.AccessoryUpdateParams{Reload: true})
	e.editAccState(func(s *accState) {
		s.Geo.LastAuto, s.Geo.AutoError = e.now().UTC(), ""
		if err != nil {
			s.Geo.AutoError = err.Error()
		}
	})
	if err != nil {
		e.log().Warn("scheduled geo update", "err", err)
	}
}

// ---- external UI (the web dashboard) ----

// uiTarget is the UI dir and the bundle URL: <config dir>/<external-ui>
// [/<external-ui-name>] from the running config, which must stay inside
// the config dir. Without external-ui it is <config dir>/ui, which
// Twinnel serves once the person installs it (st.Dashboard; configured
// false until then).
func (e *Engine) uiTarget(st *store.Settings) (dir, src string, configured bool, err error) {
	a := readAccessorySettings(st.Mihomo.ConfigFile)
	src = a.uiURL
	if src == "" {
		src = kernel.DefaultUIURL
	}
	if st.Mihomo.ConfigDir == "" {
		return "", src, false, errors.New("no mihomo config dir in the settings")
	}
	if !a.hasUIConfig || strings.TrimSpace(a.ui) == "" {
		return filepath.Join(st.Mihomo.ConfigDir, DashboardUIDir), src, false, nil
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
		return "", src, true, fmt.Errorf("external-ui %q is outside the config dir %s", dir, st.Mihomo.ConfigDir)
	}
	return dir, src, true, nil
}

// uiLocal describes the installed UI from the disk, its stamp and the
// last check (no network).
func (e *Engine) uiLocal(st *store.Settings, rec accKind) (rpc.AccessoryCheck, error) {
	dir, src, configured, err := e.uiTarget(st)
	out := rpc.AccessoryCheck{Dir: st.Mihomo.ConfigDir, CheckedAt: rec.CheckedAt, Via: rec.Via, Configured: configured, Path: "/ui/"}
	af := rpc.AccessoryFile{Name: "ui", File: dir, URL: redactURL(src)}
	if err != nil {
		af.Error = err.Error()
		out.Files = []rpc.AccessoryFile{af}
		return out, err
	}
	if fi, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !fi.IsDir() {
		af.Installed, af.ModTime = true, fi.ModTime().UTC()
	}
	var stamp kernel.UIStampData
	stamped := false
	if r, err := os.OpenRoot(st.Mihomo.ConfigDir); err == nil {
		if rel, err := filepath.Rel(st.Mihomo.ConfigDir, dir); err == nil {
			stamp, stamped = kernel.ReadUIStamp(r, rel)
		}
		r.Close()
	}
	if stamped && af.Installed {
		af.SHA256, af.Version, af.Source = stamp.Tag, stamp.Tag, redactURL(stamp.URL)
		if af.SHA256 == "" {
			af.SHA256 = stamp.SHA256
		}
	}
	r, ok := rec.Remote["ui"]
	if !ok || r.URL != src {
		af.UpdateAvailable = !af.Installed
		out.Files = []rpc.AccessoryFile{af}
		return out, nil
	}
	out.Checked = true
	af.Remote, af.RemoteTag, af.RemoteSize, af.Verifiable, af.Error = r.Tag, r.Tag, r.Size, r.SHA256 != "", r.Error
	if af.Remote == "" {
		af.Remote = r.SHA256
	}
	// Up to date only when the stamp says this exact bundle (same URL and
	// digest) is installed; a UI installed by hand counts as outdated.
	current := stamped && af.Installed && stamp.URL == src && r.SHA256 != "" && stamp.SHA256 == r.SHA256
	af.UpdateAvailable = r.Error == "" && !current
	out.Files = []rpc.AccessoryFile{af}
	return out, nil
}

func (e *Engine) uiResolve(ctx context.Context, f *kernel.Fetcher, st *store.Settings, via string) (kernel.Remote, error) {
	_, src, _, err := e.uiTarget(st)
	if err != nil {
		return kernel.Remote{}, err
	}
	r, err := f.Resolve(ctx, src)
	ar := accRemote{URL: src, SHA256: r.SHA256, Tag: r.Tag, Size: r.Size}
	if err != nil {
		ar.Error = err.Error()
	}
	e.editAccState(func(s *accState) {
		s.UI.CheckedAt, s.UI.Via, s.UI.Remote = e.now().UTC(), via, map[string]accRemote{"ui": ar}
	})
	return r, err
}

// UIStatus describes the installed dashboard and the last check's answer,
// without any network request.
func (e *Engine) UIStatus() (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	out, _ := e.uiLocal(st, e.readAccState().UI)
	return out, nil
}

// UICheck compares the installed external UI with the published bundle.
func (e *Engine) UICheck(ctx context.Context, p rpc.AccessoryCheckParams) (rpc.AccessoryCheck, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	f, via, stop, err := e.fetcher(ctx, st, p.Proxy)
	defer stop()
	if err != nil {
		return rpc.AccessoryCheck{}, err
	}
	e.uiResolve(ctx, f, st, via)
	out, _ := e.uiLocal(st, e.readAccState().UI)
	return out, nil
}

// UIUpdate downloads and verifies the UI bundle, extracts it next to the
// UI dir and swaps the dirs (the old one stays as <dir>.twinnel-bak). The
// controller serves the new files right away. When the config has no
// external-ui, Twinnel adds `external-ui: ui` (settings dashboard) and
// applies the config, so mihomo serves it.
func (e *Engine) UIUpdate(ctx context.Context, p rpc.AccessoryUpdateParams) (rpc.AccessoryUpdateResult, error) {
	accessoryMu.Lock()
	defer accessoryMu.Unlock()
	out := rpc.AccessoryUpdateResult{Updated: []string{}}
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	failed := func(err error) (rpc.AccessoryUpdateResult, error) {
		out.Error = err.Error()
		e.log().Warn("ui update failed", "err", err)
		return out, rpc.Errorf(rpc.CodeFailed, "%v", err)
	}
	_, _, configured, err := e.uiTarget(st)
	if err != nil {
		return failed(err)
	}
	if !configured {
		// Twinnel adds external-ui when it composes: only a config Twinnel
		// composes can get it.
		if _, ok := st.ActiveSubscription(); !ok {
			return failed(errors.New("the config has no external-ui and Twinnel does not compose it yet (no active subscription); add `external-ui: ui` to the config"))
		}
	}
	f, via, stop, err := e.fetcher(ctx, st, p.Proxy)
	defer stop()
	if err != nil {
		return out, err
	}
	out.Via = via
	prog := &accProgress{e: e, kind: rpc.AccessoryUI}
	f.OnBytes = prog.bytes
	prog.stage(rpc.AccStageDownload, "ui")
	r, rerr := e.uiResolve(ctx, f, st, via)
	cur, _ := e.uiLocal(st, e.readAccState().UI)
	out.Files = cur.Files
	if rerr != nil {
		return failed(rerr)
	}
	af := cur.Files[0]
	if !p.Force && !af.UpdateAvailable && configured {
		out.Skipped = []string{"ui"}
		return out, nil
	}
	if p.Force || af.UpdateAvailable {
		prog.total = r.Size
		if err := os.MkdirAll(st.Mihomo.ConfigDir, 0o755); err != nil {
			return failed(err)
		}
		cd, err := openConfDir(st.Mihomo.ConfigDir, st.Mihomo.ConfigDirUID)
		if err != nil {
			return failed(err)
		}
		defer cd.Close()
		rel, err := cd.Rel(af.File)
		if err != nil {
			return failed(err)
		}
		sw, _, err := f.PrepareUI(ctx, cd.Root, rel, r)
		if err == nil {
			prog.stage(rpc.AccStageInstall, "ui")
			if err = kernel.Commit([]*kernel.Swap{sw}); err != nil {
				out.RolledBack = true
			}
		}
		if err != nil {
			return failed(err)
		}
		out.Updated = []string{"ui"}
	}
	if !configured {
		prog.stage(rpc.AccStageReload, "ui")
		if _, err := e.Store.UpdateSettings(func(s *store.Settings) error { s.Dashboard = true; return nil }); err != nil {
			return failed(err)
		}
		res, err := e.Apply(ctx, rpc.ApplyParams{Reason: "install the dashboard", ReasonCode: store.ReasonDashboard})
		if err == nil && res != nil && res.Error != "" {
			err = errors.New(res.Error)
		}
		if err != nil {
			e.Store.UpdateSettings(func(s *store.Settings) error { s.Dashboard = false; return nil })
			out.ReloadError = err.Error()
			out.ReloadNeeded = true
		} else {
			out.Configured, out.Reloaded = true, true
		}
	}
	st, _ = e.Store.Settings()
	now, _ := e.uiLocal(st, e.readAccState().UI)
	out.Files = now.Files
	e.publish(rpc.EventAccessoryChanged, map[string]any{"ui": now.Files[0].Remote})
	e.log().Info("external ui updated", "dir", now.Files[0].File, "version", now.Files[0].Remote, "via", via)
	if out.ReloadError != "" {
		out.Error = out.ReloadError
		return out, rpc.Errorf(rpc.CodeFailed, "the dashboard was downloaded, but the config with external-ui could not be applied: %s", out.ReloadError)
	}
	return out, nil
}

// redactURL hides a URL's query (it may carry a token).
func redactURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i] + "?…"
	}
	return u
}
