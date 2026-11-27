package main

// Migrating a helper installed before the rename to Twinnel (it was
// "duet": label com.l3ony2k.duet.helper, state /Library/Application
// Support/duet, socket /var/run/duet/helper.sock). `twinnel helper
// install` and `twinnel helper upgrade` do it when they find that install
// and no new one, in one run as root:
//
//  1. read the old state (as root) and refuse when mihomo itself runs from
//     files in the old state dir (hardened, or its plist or config names
//     that dir): moving them would need a mihomo restart;
//  2. note what the old helper holds (helper.info: subscriptions, layers,
//     history, takeover, secrets; from disk when it does not answer);
//  3. stop the old helper and wait until it is gone;
//  4. copy the state dir to the new path (modes and owners kept) and point
//     the paths and the label inside its JSON files at the new names;
//  5. install and start the new helper;
//  6. check that it answers with the same subscriptions, layers, history,
//     takeover and secrets;
//  7. only then remove the old helper's plist, binary and socket, rename
//     the old state dir to <dir>.migrated-<time> (kept until you delete it)
//     and drop the old keychain items the new helper copied.
//
// Any failure in 3 to 6 rolls back: the new helper is stopped and removed,
// the copy is deleted, and the old helper is started again from its old
// paths. mihomo and tailscaled are never stopped, started or reloaded.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// UpgradeMigrate: a helper from before the rename is installed and the
// new one is not; install/upgrade migrate it.
const UpgradeMigrate = "migrate"

// migrateSide is one install of the helper.
type migrateSide struct {
	Label, StateDir, Socket, Bin, Plist string
}

// legacyHelper is the install from before the rename (zero where there is
// none to migrate). Replaceable in tests.
var legacyHelper = legacyInstall()

// migrateNow is the clock of the .migrated-<time> suffix (tests).
var migrateNow = time.Now

// migrationNeeded: the default label is asked for, the old helper is
// installed and the new one is not.
func migrationNeeded(f *installFlags) bool {
	old := legacyHelper
	if old.Label == "" || f.label != store.DefaultHelperLabel {
		return false
	}
	if !fileExistsFn(old.Bin) && !fileExistsFn(old.Plist) {
		return false
	}
	bin, def := helperPaths(f.stateDir, f.label)
	return !fileExistsFn(bin) && !fileExistsFn(def)
}

// migration is one run.
type migration struct {
	old, cur  migrateSide
	helperBin string
	want      string // the bundled helper's version ("" any)
}

func newMigration(f *installFlags, want string) *migration {
	bin, def := helperPaths(f.stateDir, f.label)
	return &migration{
		old:       legacyHelper,
		cur:       migrateSide{Label: f.label, StateDir: f.stateDir, Socket: f.socket, Bin: bin, Plist: def},
		helperBin: f.helperBin,
		want:      want,
	}
}

// helperMigrate runs the migration as root (one administrator prompt).
func helperMigrate(f *installFlags, want, op string) error {
	m := newMigration(f, want)
	return runAsRoot(op, f, m.print, m.run, m.precheck)
}

// precheck runs before asking for the password and again as root.
func (m *migration) precheck() error {
	if fi, err := os.Stat(m.helperBin); err != nil || fi.IsDir() {
		return fmt.Errorf("helper binary %s not found (build it with `go build -o %s ./cmd/helper` or pass --helper-bin)", m.helperBin, m.helperBin)
	}
	if filepath.Clean(m.old.StateDir) == filepath.Clean(m.cur.StateDir) {
		return fmt.Errorf("the old and the new state dir are both %s", m.cur.StateDir)
	}
	if fileExistsFn(filepath.Join(m.cur.StateDir, "settings.json")) {
		return fmt.Errorf("%s already holds a helper's settings: both the old (%s) and a new helper's data exist; move one of them away first", m.cur.StateDir, legacy.Name)
	}
	return nil
}

func (m *migration) print(w io.Writer) {
	fmt.Fprintf(w, "# The helper installed before the rename (%s, %s) is migrated to %s:\n", legacy.Name, m.old.Label, m.cur.Label)
	n := 0
	step := func(format string, a ...any) {
		n++
		fmt.Fprintf(w, "%d. "+format+"\n", append([]any{n}, a...)...)
	}
	step("read %s/settings.json; stop if mihomo runs from files in that dir (it would need a restart)", m.old.StateDir)
	step("note what the old helper holds (subscriptions, layers, history, takeover, secrets)")
	if stop, err := planHelperStop(m.old.Label); err == nil {
		for _, s := range stop {
			step("%s", s)
		}
	}
	step("copy %s to %s (modes and owners kept); point its paths and label at the new names", m.old.StateDir, m.cur.StateDir)
	if steps, err := planHelperInstall(m.helperBin, m.cur.StateDir, m.cur.Socket, m.cur.Label); err == nil {
		for _, s := range steps {
			step("%s", s)
		}
	}
	step("wait until the new helper answers on %s with the same subscriptions, layers, history, takeover and secrets", m.cur.Socket)
	step("remove %s, %s and %s; rename %s to %s.migrated-<time>; delete the old keychain items the new helper copied", m.old.Plist, m.old.Bin, filepath.Dir(m.old.Socket), m.old.StateDir, m.old.StateDir)
	fmt.Fprintln(w, "# If a step from 3 on fails, the new helper is removed and the old one starts again. mihomo and tailscaled are not touched.")
}

// migrateBaseline is what must survive the move.
type migrateBaseline struct {
	Subscriptions, Layers, History int
	MihomoLabel                    string
	Adopted                        bool
	Secrets                        []string
}

func baselineOf(i *rpc.HelperInfo) migrateBaseline {
	b := migrateBaseline{Subscriptions: i.Subscriptions, Layers: i.Layers, History: i.History,
		MihomoLabel: i.Mihomo.Label, Adopted: i.Mihomo.Adopted, Secrets: slices.Clone(i.SecretKeys)}
	slices.Sort(b.Secrets)
	return b
}

func (b migrateBaseline) String() string {
	return fmt.Sprintf("%d subscriptions, %d layers, %d history entries, mihomo service %s (adopted %v), secrets %v",
		b.Subscriptions, b.Layers, b.History, b.MihomoLabel, b.Adopted, b.Secrets)
}

// run is the root part.
func (m *migration) run(ctx context.Context) error {
	st, err := m.inspect()
	if err != nil {
		return err
	}
	base, err := m.baseline(ctx, st)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "# old helper: %s\n", base)

	// 3. stop the old helper.
	stop, err := planHelperStop(m.old.Label)
	if err != nil {
		return err
	}
	if err := applyPlan(ctx, stop); err != nil {
		return m.rollback(ctx, fmt.Errorf("stopping the old helper: %w", err), false)
	}
	if err := waitGone(ctx, m.old.Socket); err != nil {
		return m.rollback(ctx, err, false)
	}

	// 4.-6.
	if err := m.move(ctx, base); err != nil {
		return m.rollback(ctx, err, true)
	}

	// 7. the old install goes.
	m.cleanup(ctx)
	fmt.Fprintf(stdout, "# migrated: the helper runs as %s with its data in %s\n", m.cur.Label, m.cur.StateDir)
	return nil
}

// inspect reads the old settings and refuses a migration that would move
// files mihomo runs from.
func (m *migration) inspect() (*store.Settings, error) {
	b, err := os.ReadFile(filepath.Join(m.old.StateDir, "settings.json"))
	st := &store.Settings{}
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("reading the old helper's settings: %w", err)
	default:
		if err := json.Unmarshal(b, st); err != nil {
			return nil, fmt.Errorf("the old helper's settings.json is not readable (%v); nothing was changed", err)
		}
	}
	st.Defaults(m.old.StateDir)
	refuse := func(why string) error {
		return fmt.Errorf("not migrating the helper from before the rename: %s, and moving %s would need a mihomo restart. Nothing was changed; the old helper keeps running (see docs/runbook-adopt-macos.md, \"Migrating from duet\")", why, m.old.StateDir)
	}
	if h := st.Hardened; h != nil {
		return nil, refuse("mihomo runs from the old helper's own LaunchDaemon (" + st.Mihomo.ServiceLabel + ", `" + legacy.CLI + " helper harden`) with its files in the old state dir")
	}
	if p := serviceDefinition(st.Mihomo.ServiceLabel); p != "" {
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(m.old.StateDir)) {
			return nil, refuse("mihomo's service " + p + " names the old state dir")
		}
	}
	if b, err := os.ReadFile(st.Mihomo.ConfigFile); err == nil && bytes.Contains(b, []byte(m.old.StateDir)) {
		return nil, refuse("mihomo's config " + st.Mihomo.ConfigFile + " names the old state dir")
	}
	return st, nil
}

// baseline asks the running old helper (helper.info), else reads its data
// from disk.
func (m *migration) baseline(ctx context.Context, st *store.Settings) (migrateBaseline, error) {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var info rpc.HelperInfo
	if err := helperRPC(sctx, m.old.Socket, rpc.MethodHelperInfo, nil, &info); err == nil {
		return baselineOf(&info), nil
	}
	hist, err := (&store.Store{Dir: m.old.StateDir}).History()
	if err != nil {
		return migrateBaseline{}, fmt.Errorf("reading the old helper's history: %w", err)
	}
	i := &rpc.HelperInfo{Subscriptions: len(st.Subscriptions), Layers: len(st.Layers), History: len(hist),
		Mihomo: rpc.HelperMihomoService{Label: st.Mihomo.ServiceLabel, Adopted: st.Mihomo.ServiceAdopted}}
	b := baselineOf(i)
	b.Secrets = nil // unknown without the old helper: not compared
	return b, nil
}

// waitGone waits until nothing answers on socket.
func waitGone(ctx context.Context, socket string) error {
	deadline := time.Now().Add(verifyTimeout)
	for {
		sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var st rpc.Status
		err := helperRPC(sctx, socket, rpc.MethodStatus, nil, &st)
		cancel()
		if err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the old helper still answers on %s after it was stopped", socket)
		}
		time.Sleep(pollEvery)
	}
}

// move copies the state, installs the new helper and checks it.
func (m *migration) move(ctx context.Context, base migrateBaseline) error {
	fmt.Fprintf(stdout, "# copying %s to %s\n", m.old.StateDir, m.cur.StateDir)
	if err := copyTree(m.old.StateDir, m.cur.StateDir); err != nil {
		return fmt.Errorf("copying the state dir: %w", err)
	}
	if err := rewriteStateNames(m.cur.StateDir, map[string]string{
		m.old.StateDir: m.cur.StateDir,
		m.old.Label:    m.cur.Label,
	}); err != nil {
		return fmt.Errorf("renaming paths in the copied state: %w", err)
	}
	steps, err := planHelperInstall(m.helperBin, m.cur.StateDir, m.cur.Socket, m.cur.Label)
	if err != nil {
		return err
	}
	if err := applyPlan(ctx, steps); err != nil {
		return fmt.Errorf("installing the new helper: %w", err)
	}
	if err := verifyHelper(ctx, m.cur.Socket, m.want, 0); err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var info rpc.HelperInfo
	if err := helperRPC(sctx, m.cur.Socket, rpc.MethodHelperInfo, nil, &info); err != nil {
		return fmt.Errorf("asking the new helper what it holds: %w", err)
	}
	got := baselineOf(&info)
	if base.Secrets == nil {
		got.Secrets = nil
	}
	if filepath.Clean(info.StateDir) != filepath.Clean(m.cur.StateDir) || !reflectEqual(got, base) {
		return fmt.Errorf("the new helper does not hold the same data: it has %s in %s, the old one had %s", got, info.StateDir, base)
	}
	fmt.Fprintf(stdout, "# new helper: %s\n", got)
	return nil
}

func reflectEqual(a, b migrateBaseline) bool {
	return a.Subscriptions == b.Subscriptions && a.Layers == b.Layers && a.History == b.History &&
		a.MihomoLabel == b.MihomoLabel && a.Adopted == b.Adopted && slices.Equal(a.Secrets, b.Secrets)
}

// rollback undoes a failed migration: the new helper and the copy go, the
// old helper starts again. copied: the state copy may exist.
func (m *migration) rollback(ctx context.Context, cause error, copied bool) error {
	fmt.Fprintf(stdout, "# migration failed: %v\n# rolling back: removing the new helper, starting the old one\n", cause)
	var errs []error
	if copied {
		if stop, err := planHelperStop(m.cur.Label); err == nil {
			_ = applyPlan(ctx, stop) // not installed: nothing to stop
		}
		rm := []service.Step{
			{Kind: service.StepRemove, Path: m.cur.Plist, IgnoreError: true},
			{Kind: service.StepRemove, Path: m.cur.Bin, IgnoreError: true},
			{Kind: service.StepRemove, Path: m.cur.Socket, IgnoreError: true},
		}
		if err := applyPlan(ctx, rm); err != nil {
			errs = append(errs, err)
		}
		if err := os.RemoveAll(m.cur.StateDir); err != nil {
			errs = append(errs, fmt.Errorf("removing the copy %s: %w", m.cur.StateDir, err))
		}
	}
	start, err := planHelperStart(m.old.Label)
	if err == nil {
		err = applyPlan(ctx, start)
	}
	if err == nil {
		err = verifyHelper(ctx, m.old.Socket, "", 0)
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("starting the old helper again: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w; the rollback failed too: %v. The old helper's files are untouched: `sudo launchctl bootstrap system %s` starts it", cause, errors.Join(errs...), m.old.Plist)
	}
	return fmt.Errorf("%w; rolled back: the old helper runs again, nothing else changed", cause)
}

// cleanup removes the old install after a verified migration. Failures
// are reported, not fatal: the new helper runs.
func (m *migration) cleanup(ctx context.Context) {
	steps := []service.Step{
		{Kind: service.StepRemove, Path: m.old.Plist, IgnoreError: true},
		{Kind: service.StepRemove, Path: m.old.Bin, IgnoreError: true},
		{Kind: service.StepRemove, Path: m.old.Socket, IgnoreError: true},
	}
	if filepath.Base(filepath.Dir(m.old.Socket)) == legacy.Name {
		steps = append(steps, service.Step{Kind: service.StepRemove, Path: filepath.Dir(m.old.Socket), IgnoreError: true})
	}
	if err := applyPlan(ctx, steps); err != nil {
		fmt.Fprintf(stdout, "# note: removing the old helper's files: %v\n", err)
	}
	kept := m.old.StateDir + ".migrated-" + migrateNow().Format("20060102-150405")
	if err := os.Rename(m.old.StateDir, kept); err != nil {
		fmt.Fprintf(stdout, "# note: the old state dir stays at %s: %v\n", m.old.StateDir, err)
	} else {
		fmt.Fprintf(stdout, "# the old state dir is kept as %s; delete it once Twinnel works: sudo rm -rf '%s'\n", kept, kept)
	}
	for _, n := range dropLegacySecrets(m.secretKeys(ctx)) {
		fmt.Fprintf(stdout, "# note: %s\n", n)
	}
}

// secretKeys are the new helper's secret keys (helper.info).
func (m *migration) secretKeys(ctx context.Context) []string {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var info rpc.HelperInfo
	if helperRPC(sctx, m.cur.Socket, rpc.MethodHelperInfo, nil, &info) != nil {
		return nil
	}
	return info.SecretKeys
}

// copyTree copies dir src to dst (which must not exist, or be an empty
// dir): directories, regular files and symlinks, with their modes, owners
// and modification times. Anything else (a socket) is skipped.
func copyTree(src, dst string) error {
	if entries, err := os.ReadDir(dst); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists and is not empty", dst)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			if err := os.Mkdir(to, 0o700); err != nil && !(rel == "." && os.IsExist(err)) {
				return err
			}
		case fi.Mode().IsRegular():
			if err := copyFile(p, to); err != nil {
				return err
			}
		case fi.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, to); err != nil {
				return err
			}
			return lchownLike(to, fi)
		default:
			return nil
		}
		if err := lchownLike(to, fi); err != nil {
			return err
		}
		if err := os.Chmod(to, fi.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(to, fi.ModTime(), fi.ModTime())
	})
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// rewriteStateNames points the copied state at the new names: in every
// JSON file (except under kernels/), a string that is an old name, or
// starts with an old path and "/", gets the new one. Only whole JSON
// strings change, so the formatting stays.
func rewriteStateNames(dir string, names map[string]string) error {
	var pairs []string
	for o, n := range names {
		pairs = append(pairs, `"`+o+`"`, `"`+n+`"`, `"`+o+`/`, `"`+n+`/`)
	}
	r := strings.NewReplacer(pairs...)
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p == filepath.Join(dir, "kernels") {
			return filepath.SkipDir
		}
		if d.IsDir() || !d.Type().IsRegular() || filepath.Ext(p) != ".json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		nb := r.Replace(string(b))
		if nb == string(b) {
			return nil
		}
		// WriteFile on the existing file keeps its mode and owner.
		return os.WriteFile(p, []byte(nb), 0o600)
	})
}
