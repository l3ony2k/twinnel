package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/msg"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// Reserved IDs of the managed layers.
const (
	CoexistLayerID   = "tailscale-coexist"
	LeakGuardLayerID = "leak-guard"
)

// buildLayers turns settings into config layers. Stored layers come first in
// their saved order; the managed coexist and leak-guard layers are added
// when enabled (Compose sorts by stage, so they run after user layers).
func (e *Engine) buildLayers(ctx context.Context, st *store.Settings) ([]config.Layer, error) {
	var layers []config.Layer
	for _, l := range st.Layers {
		b, err := e.Store.ReadLayer(l.ID)
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", l.ID, err)
		}
		if l.Format == store.FormatVerge {
			if b, err = config.ImportVergeMerge(b); err != nil {
				return nil, fmt.Errorf("layer %s: %w", l.ID, err)
			}
		}
		stage := config.StageUser
		if l.Stage == store.StageBase {
			stage = config.StageBase
		}
		cl := config.OverrideLayer(l.ID, l.Name, config.KindUser, stage, b)
		cl.Enabled = l.Enabled
		layers = append(layers, cl)
	}
	if st.Tailscale.Coexist {
		l, err := e.coexistLayer(ctx, st)
		if err != nil {
			return nil, err
		}
		layers = append(layers, l)
	}
	if len(st.LeakGuardGroups) > 0 {
		layers = append(layers, config.LeakGuardLayer(st.LeakGuardGroups))
	}
	return layers, nil
}

// coexistLayer generates the Tailscale layer from the cached tailnet info.
// With no cache yet it polls once; if Tailscale can't be read the layer is
// still generated with the default CIDRs and domains (no peer names).
func (e *Engine) coexistLayer(ctx context.Context, st *store.Settings) (config.Layer, error) {
	e.tailnetMu.Lock()
	info, ok := e.cachedTailnet()
	if !ok && e.Tailnet != nil {
		if raw, err := e.Tailnet.Status(ctx); err == nil {
			if i, err := config.TailnetInfoFromStatus(raw); err == nil {
				i.Interface = e.tailnetIface(ctx, "")
				info, ok = i, true
				e.saveTailnet(i)
			}
		}
	}
	e.tailnetMu.Unlock()
	info.Resolver = st.Tailscale.Resolver
	return config.TailscaleCoexistLayer(info)
}

// compose builds the composed config without the controller secret, and
// returns the secret a layer set, if any.
func (e *Engine) compose(ctx context.Context, st *store.Settings) (*config.Result, string, error) {
	sub, ok := st.ActiveSubscription()
	if !ok {
		return nil, "", errors.New("no active subscription")
	}
	raw, err := e.Store.ReadSubscription(sub.ID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("subscription %s has no content yet (refresh it first)", sub.ID)
		}
		return nil, "", err
	}
	layers, err := e.buildLayers(ctx, st)
	if err != nil {
		return nil, "", err
	}
	res, err := config.ComposeReport(raw, layers)
	if err != nil {
		return nil, "", err
	}
	cfg, secret, err := withoutSecret(res.Config)
	if err != nil {
		return nil, "", err
	}
	// After service.harden, paths into the old config dir (kept in layers
	// that adopt derived from the live config) point into the new one.
	if h := st.Hardened; h != nil && h.OldConfigDir != "" {
		if cfg, _, err = relocatePaths(cfg, h.OldConfigDir); err != nil {
			return nil, "", err
		}
	}
	res.Config = cfg
	return res, secret, nil
}

func (e *Engine) validateOpts(st *store.Settings) []config.ValidateOption {
	// The geo databases are linked from a copy in the state dir, not
	// copied on every check (tens of MB; on Windows each copy is scanned
	// again). A fresh install has no config dir yet: mihomo -t then
	// downloads them, once, into that copy.
	opts := []config.ValidateOption{config.WithGeoCache(e.Store.Path("validate", "geo"))}
	if st.Mihomo.ConfigDir == "" {
		return opts
	}
	opts = append(opts, config.WithSafePaths(st.Mihomo.ConfigDir))
	if fi, err := os.Stat(st.Mihomo.ConfigDir); err == nil && fi.IsDir() {
		opts = append(opts, config.WithGeoDir(st.Mihomo.ConfigDir))
	}
	return opts
}

// validKey names one mihomo -t run: the bytes checked, the binary, and
// the geo databases it was given. Two runs with the same key give the same
// answer, so a passing one need not run again (预览 then 应用, or an
// apply after a status check found the same config).
func (e *Engine) validKey(st *store.Settings, bin string, final []byte) string {
	h := sha256.New()
	h.Write(final)
	fmt.Fprintf(h, "\x00bin=%s", bin)
	if fi, err := os.Stat(bin); err == nil {
		fmt.Fprintf(h, ":%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	fmt.Fprintf(h, "\x00dir=%s", st.Mihomo.ConfigDir)
	if st.Mihomo.ConfigDir != "" {
		es, _ := os.ReadDir(st.Mihomo.ConfigDir)
		for _, de := range es {
			if fi, err := de.Info(); err == nil && fi.Mode().IsRegular() {
				switch strings.ToLower(filepath.Ext(de.Name())) {
				case ".mmdb", ".metadb", ".dat":
					fmt.Fprintf(h, "\x00%s:%d:%d", de.Name(), fi.Size(), fi.ModTime().UnixNano())
				}
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validCacheSize is how many passing mihomo -t runs are remembered.
const validCacheSize = 4

// passedBefore reports whether a mihomo -t with key passed recently.
func (e *Engine) passedBefore(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Contains(e.validOK, key)
}

// rememberPass records a passing mihomo -t run.
func (e *Engine) rememberPass(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if slices.Contains(e.validOK, key) {
		return
	}
	e.validOK = append(e.validOK, key)
	if len(e.validOK) > validCacheSize {
		e.validOK = e.validOK[len(e.validOK)-validCacheSize:]
	}
}

// stage tells clients what an apply or rollback now waits for.
func (e *Engine) stage(name string) {
	e.publish(rpc.EventComposeStage, rpc.ComposeStage{Stage: name})
}

// blame names the overrides (or the subscription) that hold a name
// mihomo -t's errors quote, so the app can point at them (walkthrough 2,
// N5). Best effort: nothing when the sources can't be read.
func (e *Engine) blame(ctx context.Context, st *store.Settings, errs []string) []rpc.Culprit {
	sub, ok := st.ActiveSubscription()
	if !ok {
		return nil
	}
	raw, err := e.Store.ReadSubscription(sub.ID)
	if err != nil {
		return nil
	}
	layers, err := e.buildLayers(ctx, st)
	if err != nil {
		return nil
	}
	var out []rpc.Culprit
	for _, c := range config.Blame(errs, raw, layers) {
		out = append(out, rpc.Culprit{Name: c.Name, Layer: c.Layer, Title: c.Title})
	}
	return out
}

// ComposeStatus composes and compares with the file on disk, without
// mihomo -t: whether 应用 has work, cheap enough for every edit.
func (e *Engine) ComposeStatus(ctx context.Context) (rpc.ComposeStatus, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.ComposeStatus{}, err
	}
	res, layerSecret, err := e.compose(ctx, st)
	if err != nil {
		return rpc.ComposeStatus{}, err
	}
	current, _ := readConfined(st.Mihomo.ConfigFile)
	final, err := e.finalConfig(res.Config, current, layerSecret)
	if err != nil {
		return rpc.ComposeStatus{}, err
	}
	return rpc.ComposeStatus{Hash: store.Hash(res.Config), Changed: !bytes.Equal(final, current)}, nil
}

// Preview composes and validates without writing anything.
func (e *Engine) Preview(ctx context.Context) (rpc.ComposeResult, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.ComposeResult{}, err
	}
	res, layerSecret, err := e.compose(ctx, st)
	if err != nil {
		return rpc.ComposeResult{}, err
	}
	current, _ := readConfined(st.Mihomo.ConfigFile)
	cr, _, err := e.check(ctx, st, res.Config, res.Applied, res.Warnings, current, layerSecret)
	cr.Messages = msg.All(cr.Warnings)
	return cr, err
}

// check validates the final bytes (composed + secret) and fills a
// ComposeResult. The secret written is the stored one, else the one a layer
// set (layerSecret, already stripped from cfg), else the one in the file
// being replaced.
func (e *Engine) check(ctx context.Context, st *store.Settings, cfg []byte, applied, warnings []string, current []byte, layerSecret string) (rpc.ComposeResult, []byte, error) {
	cr := rpc.ComposeResult{Applied: applied, Warnings: warnings, Hash: store.Hash(cfg), Size: len(cfg)}
	if cr.Applied == nil {
		cr.Applied = []string{}
	}
	final, err := e.finalConfig(cfg, current, layerSecret)
	if err != nil {
		return cr, nil, err
	}
	cr.Changed = !bytes.Equal(final, current)
	bin, err := e.mihomoBinary(st)
	if err != nil {
		return cr, nil, err
	}
	key := e.validKey(st, bin, final)
	v := &config.Validation{OK: true}
	if !e.passedBefore(key) {
		if v, err = e.validate(ctx, bin, final, e.validateOpts(st)...); err != nil {
			return cr, nil, err
		}
		if v.OK {
			e.rememberPass(key)
		}
	}
	cr.Valid = v.OK
	cr.ValidateErrors = v.Errors
	if !v.OK && (len(applied) == 0 || !strings.HasPrefix(applied[0], "history:")) {
		cr.Culprits = e.blame(ctx, st, v.Errors)
	}
	warn, errs := exposureCheck(final, st)
	cr.Warnings = append(cr.Warnings, warn...)
	if len(errs) > 0 {
		cr.Valid = false
		cr.ValidateErrors = append(cr.ValidateErrors, errs...)
	}
	return cr, final, nil
}

// finalConfig is cfg with the controller secret written: the stored one,
// else the one a layer set (layerSecret, already stripped from cfg), else
// the one in the file being replaced.
func (e *Engine) finalConfig(cfg, current []byte, layerSecret string) ([]byte, error) {
	secret := e.controllerSecret()
	if secret == "" {
		secret = layerSecret
	}
	if secret == "" && len(current) > 0 {
		secret, _ = topLevel(current, "secret")
	}
	return withSecret(cfg, secret)
}

// Apply runs the whole pipeline: compose → mihomo -t → back up → write →
// reload → verify. On any failure the previous file stays (or is put back)
// and the previous config keeps running.
func (e *Engine) Apply(ctx context.Context, p rpc.ApplyParams) (*rpc.ApplyResult, error) {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	start := e.now()
	res := &rpc.ApplyResult{Reason: p.Reason, ReasonCode: p.ReasonCode, ReasonArgs: p.ReasonArgs, Time: start.UTC()}
	e.stage(rpc.StageValidate)

	st, err := e.Store.Settings()
	if err != nil {
		return e.fail(res, start, err)
	}
	comp, layerSecret, err := e.compose(ctx, st)
	if err != nil {
		return e.fail(res, start, err)
	}
	man, err := e.manifest(st, true)
	if err != nil {
		e.log().Warn("layer manifest", "err", err) // the entry is kept without one
		man = nil
	}
	meta := store.HistoryMeta{Reason: p.Reason, Code: p.ReasonCode, Args: p.ReasonArgs, Layers: man}
	if meta.Code == "" && meta.Reason == "" {
		meta.Code, meta.Reason = store.ReasonApply, "apply"
		res.ReasonCode = store.ReasonApply
	}
	out, err := e.install(ctx, st, comp.Config, comp.Applied, comp.Warnings, layerSecret, store.HistoryApplied, p, meta, res, start)
	if err == nil {
		e.setDrift(nil) // mihomo runs what the overrides compose
	}
	return out, err
}

// Rollback applies a history entry again through the same checks.
func (e *Engine) Rollback(ctx context.Context, id string) (*rpc.ApplyResult, error) {
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	start := e.now()
	res := &rpc.ApplyResult{Reason: "rollback to " + id, Time: start.UTC()}
	e.stage(rpc.StageValidate)
	data, target, err := e.Store.ReadHistory(id)
	if err != nil {
		if errors.Is(err, store.ErrNotExist) {
			return nil, rpc.ErrNotFound("history entry", id)
		}
		return nil, rpc.ErrInvalid("%v", err)
	}
	st, err := e.Store.Settings()
	if err != nil {
		return e.fail(res, start, err)
	}
	p := rpc.ApplyParams{Reason: res.Reason, ReasonCode: store.ReasonRollback, ReasonArgs: []string{id}, Force: true}
	res.ReasonCode, res.ReasonArgs = p.ReasonCode, p.ReasonArgs
	meta := store.HistoryMeta{Reason: p.Reason, Code: p.ReasonCode, Args: p.ReasonArgs, Layers: target.Layers}
	out, err := e.install(ctx, st, data, []string{"history:" + id}, nil, "", store.HistoryRollback, p, meta, res, start)
	if err != nil {
		return out, err
	}
	// Does mihomo now run what the overrides compose? Only when the entry
	// was composed from the very same overrides; otherwise the config page
	// says so, and automatic re-compose waits (it would undo the rollback).
	cur, merr := e.manifest(st, false)
	if merr == nil && target.Layers != nil && target.Layers.Equal(cur) {
		e.setDrift(nil)
	} else {
		e.setDrift(&store.Drift{Since: e.now().UTC().Truncate(time.Second), HistoryID: id, Manifest: target.Layers != nil})
	}
	return out, nil
}

func (e *Engine) install(ctx context.Context, st *store.Settings, cfg []byte, applied, warnings []string, layerSecret, kind string, p rpc.ApplyParams, meta store.HistoryMeta, res *rpc.ApplyResult, start time.Time) (*rpc.ApplyResult, error) {
	cfgPath := st.Mihomo.ConfigFile
	current, err := readConfined(cfgPath)
	hadCurrent := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return e.fail(res, start, err)
	}

	if generated, err := e.ensureSecret(st, layerSecret, current, hadCurrent); err != nil {
		return e.fail(res, start, err)
	} else if generated {
		warnings = append(warnings, "generated a new controller secret (kept in the helper's secret store)")
	}
	cr, final, err := e.check(ctx, st, cfg, applied, warnings, current, layerSecret)
	res.ComposeResult = cr
	if err != nil {
		return e.fail(res, start, err)
	}
	if !cr.Valid {
		return e.fail(res, start, fmt.Errorf("mihomo -t rejected the config: %v", cr.ValidateErrors))
	}
	if !cr.Changed && !p.Force {
		return e.done(res, start), nil
	}

	// What the reload will do to traffic. Without a previous file there is
	// nothing to compare: reload with force.
	force := true
	if hadCurrent {
		im := ReloadImpact(current, final)
		res.Impact = &im
		force = im.Force
		if im.Disruptive {
			res.Warnings = append(res.Warnings, "this reload interrupts the network briefly: "+strings.Join(im.Notes, "; "))
		} else if len(im.RestartNeeded) > 0 {
			res.Warnings = append(res.Warnings, "a hot reload does not apply "+strings.Join(im.RestartNeeded, ", ")+"; the controller keeps its address and secret until mihomo restarts")
		}
	}

	// Is mihomo running? If not, the config is written and used at the
	// next start; there is nothing to reload or verify.
	oldCtrl := e.currentController(st)
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, verr := oldCtrl.Version(vctx)
	cancel()
	running := verr == nil

	// Preflight: never write for, or reload, another program's mihomo on
	// the controller address (Clash Party, Clash Verge Rev…).
	if o := e.ControllerOwner(ctx); o.IsForeign() {
		res.Foreign = &o
		return e.fail(res, start, foreignError(o))
	}

	e.stage(rpc.StageWrite)
	// Back up what is on disk unless history already has it.
	if hadCurrent {
		stripped := stripSecretBytes(current)
		latest, ok, _ := e.Store.LatestHistory()
		if !ok || (latest.Hash != store.Hash(stripped) && !sameComposed(current, latest)) {
			bm := store.HistoryMeta{Reason: "file on disk before apply", Code: store.ReasonBackup,
				BeforeTwinnel: !ok || p.ReasonCode == store.ReasonAdopt}
			if _, err := e.Store.AddHistoryMeta(store.HistoryBackup, bm, stripped, e.now(), st.HistoryKeep); err != nil {
				return e.fail(res, start, fmt.Errorf("backup: %w", err))
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return e.fail(res, start, err)
	}
	cd, name, err := e.openConfigFileDir(st)
	if err != nil {
		return e.fail(res, start, err)
	}
	defer cd.Close()
	mode := fs.FileMode(0o600)
	oldInfo, _ := cd.Stat(name)
	if oldInfo != nil {
		mode = oldInfo.Mode().Perm()
	}
	if err := writeConfined(cd, name, final, mode, oldInfo); err != nil {
		return e.fail(res, start, fmt.Errorf("write %s: %w", cfgPath, err))
	}
	res.Written = true

	if running {
		e.stage(rpc.StageReload)
		if err := e.reloadAndVerify(ctx, oldCtrl, cfgPath, force); err != nil {
			// mihomo refused the path as outside its home: the mihomo that
			// answered is not the one that runs this config. Put the file
			// back without reloading that one again.
			if foreign.IsSafePathsError(err.Error()) {
				o := foreign.FromSafePaths(controllerAddr(st.Mihomo.Controller), err.Error(), e.now())
				e.setOwner(o)
				e.restore(ctx, nil, cd, name, final, current, hadCurrent, mode)
				res.Written, res.RolledBack, res.Foreign = false, true, &o
				return e.fail(res, start, foreignError(o))
			}
			e.restore(ctx, oldCtrl, cd, name, final, current, hadCurrent, mode)
			res.Written, res.RolledBack = false, true
			return e.fail(res, start, err)
		}
		res.Reloaded = true
		e.clearForeign()
	} else {
		res.Warnings = append(res.Warnings, "mihomo is not running ("+verr.Error()+"); the config was written and will be used at the next start")
	}

	h, err := e.Store.AddHistoryMeta(kind, meta, cfg, e.now(), st.HistoryKeep)
	if err != nil {
		e.log().Warn("history", "err", err)
	} else {
		res.HistoryID = h.ID
	}
	e.triggerHealth("config applied")
	return e.done(res, start), nil
}

// ensureSecret generates and stores a random controller secret when no
// secret exists anywhere (secret store, layers, the file being replaced)
// and the helper manages the config: there is no config file yet, or it
// lives in the helper's state dir. An adopted config without a secret is
// left alone, since its other clients would lose access.
func (e *Engine) ensureSecret(st *store.Settings, layerSecret string, current []byte, hadCurrent bool) (bool, error) {
	if e.Secrets == nil || layerSecret != "" || e.controllerSecret() != "" {
		return false, nil
	}
	if hadCurrent {
		if s, _ := topLevel(current, "secret"); s != "" {
			return false, nil
		}
		if !e.managesConfig(st) {
			return false, nil
		}
	}
	secret, err := newSecret()
	if err != nil {
		return false, err
	}
	if err := e.Secrets.Set(store.SecretController, secret); err != nil {
		return false, fmt.Errorf("store the new controller secret: %w", err)
	}
	e.log().Info("generated a controller secret")
	return true, nil
}

// newSecret returns 24 random bytes in hex.
func newSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// managesConfig: the config file is inside the helper's state dir (the
// layout of a fresh install), not an adopted file elsewhere.
func (e *Engine) managesConfig(st *store.Settings) bool {
	rel, err := filepath.Rel(e.Store.Dir, st.Mihomo.ConfigFile)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// sameComposed reports whether current is the latest history entry plus a
// secret (history entries are stored without it).
func sameComposed(current []byte, latest store.HistoryEntry) bool {
	stripped, _, err := withoutSecret(current)
	return err == nil && store.Hash(stripped) == latest.Hash
}

// reloadAndVerify asks mihomo to load path and waits until its controller
// answers again. A hot reload never moves the controller or changes its
// secret (mihomo reads those only at start), so the same controller is
// asked before and after.
func (e *Engine) reloadAndVerify(ctx context.Context, ctrl Controller, path string, force bool) error {
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := reloadWith(rctx, ctrl, path, force); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	e.stage(rpc.StageVerify)
	timeout := e.VerifyTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := ctrl.Version(vctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("mihomo did not answer after reload: %w", lastErr)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// restore puts the previous file back and asks mihomo to load it again
// (not when ctrl is nil).
func (e *Engine) restore(ctx context.Context, ctrl Controller, cd *confDir, name string, failed, old []byte, hadOld bool, mode fs.FileMode) {
	path := filepath.Join(cd.Path, name)
	if !hadOld {
		cd.Remove(name)
		return
	}
	oldInfo, _ := cd.Stat(name)
	if err := writeConfined(cd, name, old, mode, oldInfo); err != nil {
		e.log().Error("restore config failed", "path", path, "err", err)
		return
	}
	if ctrl == nil { // the controller is not Twinnel's: the file only
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := reloadWith(rctx, ctrl, path, ReloadImpact(failed, old).Force); err != nil {
		e.log().Error("reload of restored config failed", "err", err)
	}
}

func (e *Engine) done(res *rpc.ApplyResult, start time.Time) *rpc.ApplyResult {
	res.Duration = e.now().Sub(start).Round(time.Millisecond).String()
	res.Messages = msg.All(res.Warnings)
	e.mu.Lock()
	la := *res
	e.lastApply = &la
	e.mu.Unlock()
	e.publish(rpc.EventComposeApplied, res)
	e.log().Info("apply", "reason", res.Reason, "changed", res.Changed, "written", res.Written, "reloaded", res.Reloaded, "hash", short(res.Hash), "history", res.HistoryID)
	return res
}

func (e *Engine) fail(res *rpc.ApplyResult, start time.Time, err error) (*rpc.ApplyResult, error) {
	res.Error = err.Error()
	res.Duration = e.now().Sub(start).Round(time.Millisecond).String()
	res.Messages = msg.All(res.Warnings)
	if m := msg.Parse(res.Error); m.Code != "" {
		res.ErrorMsg = &m
	}
	e.mu.Lock()
	la := *res
	e.lastApply = &la
	e.mu.Unlock()
	e.publish(rpc.EventComposeFailed, res)
	e.log().Warn("apply failed", "reason", res.Reason, "err", err, "rolled_back", res.RolledBack)
	return res, err
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// History lists history entries.
func (e *Engine) History() ([]rpc.HistoryEntry, error) {
	idx, err := e.Store.History()
	if err != nil {
		return nil, err
	}
	out := make([]rpc.HistoryEntry, 0, len(idx))
	for _, h := range idx {
		out = append(out, rpc.HistoryEntry{ID: h.ID, Time: h.Time, Hash: h.Hash, Size: h.Size, Kind: h.Kind, Reason: h.Reason,
			ReasonCode: h.ReasonCode, ReasonArgs: h.ReasonArgs, BeforeTwinnel: h.BeforeTwinnel, Layers: toRPCManifest(h.Layers)})
	}
	return out, nil
}
