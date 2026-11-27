//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// migrateModel is a fake launchd with two helpers (old and new) and the
// sockets they answer on. The new helper reads its state dir for real, so
// what it reports is what the copy holds.
type migrateModel struct {
	*lifecycleFakes
	root           string
	old            migrateSide
	newDir, newSoc string
	bin            string

	oldRunning, newRunning bool
	failNewBootstrap       bool
	dropLayer              bool // the new helper "loses" a layer
	stuck                  bool // the old helper keeps answering after bootout
	dropped                []string
}

const testSecret = "controller-secret"

func newMigrateModel(t *testing.T, euid int) *migrateModel {
	l := lifecycle(t, euid)
	root := t.TempDir()
	m := &migrateModel{lifecycleFakes: l, root: root, oldRunning: true}
	m.old = migrateSide{
		Label:    "com.l3ony2k.duet.helper",
		StateDir: filepath.Join(root, "old", "duet"),
		Socket:   filepath.Join(root, "run-duet", "helper.sock"),
		Bin:      filepath.Join(root, "tools", "com.l3ony2k.duet.helper"),
		Plist:    filepath.Join(root, "daemons", "com.l3ony2k.duet.helper.plist"),
	}
	m.newDir = filepath.Join(root, "new", "twinnel")
	m.newSoc = filepath.Join(root, "run-twinnel", "helper.sock")
	for _, p := range []string{m.old.Bin, m.old.Plist} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("old"), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "new"), 0o755)
	m.bin = helperBinary(t)
	l.versions[m.bin] = "0.3.0"
	l.versions[m.old.Bin] = "0.2.0"
	newBin, newPlist := helperPaths(m.newDir, store.DefaultHelperLabel)
	l.exists[newBin], l.exists[newPlist] = false, false
	l.loaded = false // the new label is not loaded: the full install plan

	writeOldState(t, m.old.StateDir, root)

	oldLegacy, oldNow, oldDrop := legacyHelper, migrateNow, dropLegacySecrets
	legacyHelper = m.old
	migrateNow = func() time.Time { return time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC) }
	dropLegacySecrets = func(keys []string) []string { m.dropped = append(m.dropped, keys...); return nil }
	t.Cleanup(func() { legacyHelper, migrateNow, dropLegacySecrets = oldLegacy, oldNow, oldDrop })

	helperRPC = func(_ context.Context, socket, method string, _, result any) error {
		l.calls = append(l.calls, socket+" "+method)
		var v any
		switch {
		case socket == m.old.Socket && (m.oldRunning || m.stuck):
			switch method {
			case rpc.MethodStatus:
				v = rpc.Status{HelperVersion: "0.2.0", PID: 10}
			case rpc.MethodHelperInfo:
				v = rpc.HelperInfo{StateDir: m.old.StateDir, Subscriptions: 2, Layers: 3, History: 4,
					Mihomo: rpc.HelperMihomoService{Label: "com.example.mihomo", Adopted: true}, SecretKeys: []string{testSecret}}
			}
		case socket == m.newSoc && m.newRunning:
			switch method {
			case rpc.MethodStatus:
				v = rpc.Status{HelperVersion: "0.3.0", PID: 20}
			case rpc.MethodHelperInfo:
				v = m.newInfo(t)
			}
		}
		if v == nil {
			return errors.New("dial unix " + socket + ": connect: no such file or directory")
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, result)
	}
	applyPlan = func(_ context.Context, steps []service.Step) error {
		l.applied = append(l.applied, steps)
		for _, s := range steps {
			if s.Kind != service.StepRun || len(s.Argv) < 3 || s.Argv[0] != "/bin/launchctl" {
				continue
			}
			arg := s.Argv[len(s.Argv)-1]
			switch s.Argv[1] {
			case "bootout":
				if arg == "system/"+m.old.Label {
					m.oldRunning = false
				} else if arg == "system/"+store.DefaultHelperLabel {
					m.newRunning = false
				}
			case "bootstrap":
				if strings.HasSuffix(arg, "/"+m.old.Label+".plist") {
					m.oldRunning = true
				} else if strings.HasSuffix(arg, store.DefaultHelperLabel+".plist") {
					if m.failNewBootstrap {
						return errors.New("/bin/launchctl bootstrap system " + arg + ": exit status 5: Bootstrap failed: 5: Input/output error")
					}
					m.newRunning = true
				}
			}
		}
		return nil
	}
	return m
}

// newInfo is helper.info of a helper running on the copied state.
func (m *migrateModel) newInfo(t *testing.T) rpc.HelperInfo {
	s := &store.Store{Dir: m.newDir}
	st, err := s.Settings()
	if err != nil {
		t.Fatalf("new helper reads its settings: %v", err)
	}
	hist, _ := s.History()
	i := rpc.HelperInfo{StateDir: m.newDir, Label: st.HelperLabel, Subscriptions: len(st.Subscriptions), Layers: len(st.Layers),
		History: len(hist), Mihomo: rpc.HelperMihomoService{Label: st.Mihomo.ServiceLabel, Adopted: st.Mihomo.ServiceAdopted},
		SecretKeys: []string{testSecret}}
	if m.dropLayer {
		i.Layers--
	}
	return i
}

// writeOldState is a synthetic state dir of the old helper: an adopted
// mihomo whose config lives elsewhere, paths into the state dir in
// settings.json, history, a symlink, the usual modes.
func writeOldState(t *testing.T, dir, root string) {
	conf := filepath.Join(root, "mihomo-conf")
	os.MkdirAll(conf, 0o755)
	os.WriteFile(filepath.Join(conf, "config.yaml"), []byte("mixed-port: 7890\n"), 0o644)
	st := map[string]any{
		"version": 1,
		"mihomo": map[string]any{"config_dir": conf, "config_file": filepath.Join(conf, "config.yaml"),
			"controller": "127.0.0.1:19090", "service_label": "com.example.mihomo", "service_adopted": true},
		"subscriptions": []map[string]any{{"id": "main", "name": "Main", "active": true}, {"id": "spare", "name": "Spare"}},
		"layers":        []map[string]any{{"id": "base-1", "stage": "base", "enabled": true}, {"id": "user-1", "enabled": true}, {"id": "user-3", "enabled": true}},
		"helper_label":  "com.l3ony2k.duet.helper",
		"adopted":       map[string]any{"plan_id": "p1", "live_config": filepath.Join(conf, "config.yaml"), "backup": filepath.Join(dir, "takeover", "p1", "live-backup.yaml")},
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	for _, d := range []string{"subscriptions", "layers", "history", "takeover/p1", "kernels/mihomo", "logs"} {
		os.MkdirAll(filepath.Join(dir, d), 0o700)
	}
	os.Chmod(filepath.Join(dir, "logs"), 0o755)
	write := func(rel, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", string(b), 0o600)
	write("subscriptions/main.yaml", "proxies: []\n", 0o600)
	write("layers/user-3.yaml", "rules: []\n", 0o600)
	write("takeover/p1/live-backup.yaml", "mixed-port: 7890\n", 0o600)
	write("kernels/mihomo/v1", "binary", 0o755)
	write("logs/helper.log", "log\n", 0o644)
	var hist []map[string]any
	for i := range 4 {
		hist = append(hist, map[string]any{"id": "h" + string(rune('0'+i)), "time": time.Date(2026, 11, 20+i, 0, 0, 0, 0, time.UTC), "before_duet": i == 0})
	}
	hb, _ := json.Marshal(hist)
	write("history/index.json", string(hb), 0o600)
	os.Symlink("v1", filepath.Join(dir, "kernels/mihomo/current"))
}

func (m *migrateModel) upgradeArgs(extra ...string) []string {
	return append([]string{"--helper-bin", m.bin, "--state-dir", m.newDir, "--socket", m.newSoc}, extra...)
}

func (m *migrateModel) launchctl() []string {
	var out []string
	for _, s := range m.steps() {
		if strings.HasPrefix(s, "/bin/launchctl") {
			out = append(out, strings.SplitN(strings.TrimPrefix(s, "/bin/launchctl "), "  #", 2)[0])
		}
	}
	return out
}

func TestMigrateCheckReportsMigrate(t *testing.T) {
	m := newMigrateModel(t, 501)
	if err := runHelperUpgrade(m.upgradeArgs("--check", "--json")); err != nil {
		t.Fatal(err)
	}
	var c UpgradeCheck
	if err := json.Unmarshal(m.out.Bytes(), &c); err != nil {
		t.Fatalf("%v: %s", err, m.out.String())
	}
	if c.Action != UpgradeMigrate || c.Installed != "0.2.0" || c.Running != "0.2.0" || c.InstalledAt != m.old.Bin {
		t.Errorf("%+v", c)
	}
}

// The whole migration as root: stop the old helper, copy and rename,
// start the new one, verify, then remove the old install.
func TestMigrateAsRoot(t *testing.T) {
	m := newMigrateModel(t, 0)
	if err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes")); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	out := m.out.String()
	if !strings.Contains(out, "# migrated:") || !m.newRunning || m.oldRunning {
		t.Fatalf("not migrated (new %v old %v):\n%s", m.newRunning, m.oldRunning, out)
	}
	// launchd: the old helper stops before the new one starts; mihomo is
	// never touched.
	lc := strings.Join(m.launchctl(), "\n")
	stop, boot := strings.Index(lc, "bootout system/"+m.old.Label), strings.Index(lc, "bootstrap system /Library/LaunchDaemons/"+store.DefaultHelperLabel+".plist")
	if stop < 0 || boot < 0 || boot < stop {
		t.Errorf("launchctl order:\n%s", lc)
	}
	if strings.Contains(lc, "mihomo") {
		t.Errorf("mihomo touched:\n%s", lc)
	}
	// The copy: paths and the label point at the new names, the rest is
	// as it was, modes kept.
	b, err := os.ReadFile(filepath.Join(m.newDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, m.old.StateDir) || !strings.Contains(s, `"backup": "`+m.newDir+`/takeover/p1/live-backup.yaml"`) ||
		!strings.Contains(s, `"helper_label": "`+store.DefaultHelperLabel+`"`) || !strings.Contains(s, `"service_label": "com.example.mihomo"`) {
		t.Errorf("settings.json:\n%s", s)
	}
	for rel, mode := range map[string]os.FileMode{"subscriptions": 0o700, "subscriptions/main.yaml": 0o600, "logs": 0o755, "kernels/mihomo/v1": 0o755, "settings.json": 0o600} {
		fi, err := os.Stat(filepath.Join(m.newDir, rel))
		if err != nil || fi.Mode().Perm() != mode {
			t.Errorf("%s: %v %v, want %v", rel, fi.Mode(), err, mode)
		}
	}
	if l, err := os.Readlink(filepath.Join(m.newDir, "kernels/mihomo/current")); err != nil || l != "v1" {
		t.Errorf("symlink: %q %v", l, err)
	}
	if b, _ := os.ReadFile(filepath.Join(m.newDir, "history/index.json")); !strings.Contains(string(b), `"before_duet":true`) {
		t.Errorf("history index changed: %s", b)
	}
	// The old install: plist, binary and socket go; the state dir is kept
	// under another name; the old keychain items are dropped.
	if _, err := os.Stat(m.old.StateDir); !os.IsNotExist(err) {
		t.Errorf("old state dir still there: %v", err)
	}
	if _, err := os.Stat(m.old.StateDir + ".migrated-20261127-100000"); err != nil {
		t.Errorf("old state not kept: %v", err)
	}
	steps := strings.Join(m.steps(), "\n")
	for _, p := range []string{m.old.Plist, m.old.Bin, m.old.Socket} {
		if !strings.Contains(steps, "rm -f "+p) {
			t.Errorf("%s not removed:\n%s", p, steps)
		}
	}
	if strings.Join(m.dropped, ",") != testSecret {
		t.Errorf("legacy secrets dropped: %v", m.dropped)
	}
}

// `twinnel helper install` over the old install migrates as well.
func TestInstallMigrates(t *testing.T) {
	m := newMigrateModel(t, 0)
	if err := runHelperInstall(m.upgradeArgs("--as-root", "--yes")); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	if !strings.Contains(m.out.String(), "# migrated:") || !m.newRunning {
		t.Errorf("not migrated:\n%s", m.out.String())
	}
}

// As a normal user it shows the plan and elevates once, with the same
// command.
func TestMigrateElevates(t *testing.T) {
	m := newMigrateModel(t, 501)
	if err := runHelperUpgrade(m.upgradeArgs("--yes")); err != nil {
		t.Fatal(err)
	}
	if len(m.elevated) != 1 || len(m.applied) != 0 {
		t.Fatalf("elevated %d applied %d", len(m.elevated), len(m.applied))
	}
	if !strings.Contains(strings.Join(m.elevated[0], " "), "helper upgrade --as-root --yes") {
		t.Errorf("argv %q", m.elevated[0])
	}
	if out := m.out.String(); !strings.Contains(out, "is migrated to "+store.DefaultHelperLabel) || !strings.Contains(out, "rolls back") && !strings.Contains(out, "old one starts again") {
		t.Errorf("plan:\n%s", out)
	}
}

// The new helper does not start: everything is undone and the old helper
// runs again from its old paths.
func TestMigrateRollsBackWhenNewHelperFails(t *testing.T) {
	m := newMigrateModel(t, 0)
	m.failNewBootstrap = true
	err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes"))
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v\n%s", err, m.out.String())
	}
	m.assertRolledBack(t)
}

// The new helper starts but holds other data: rolled back too.
func TestMigrateRollsBackOnDataMismatch(t *testing.T) {
	m := newMigrateModel(t, 0)
	m.dropLayer = true
	err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes"))
	if err == nil || !strings.Contains(err.Error(), "does not hold the same data") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v\n%s", err, m.out.String())
	}
	m.assertRolledBack(t)
	if strings.Contains(strings.Join(m.launchctl(), "\n"), "bootout system/"+store.DefaultHelperLabel) == false {
		t.Errorf("the new helper was not stopped:\n%s", strings.Join(m.launchctl(), "\n"))
	}
}

// The old helper does not let go of its socket: nothing is copied, it is
// started again.
func TestMigrateRollsBackWhenOldHelperStays(t *testing.T) {
	m := newMigrateModel(t, 0)
	m.stuck = true
	err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes"))
	if err == nil || !strings.Contains(err.Error(), "still answers") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(m.newDir); !os.IsNotExist(err) {
		t.Errorf("copied anyway: %v", err)
	}
}

func (m *migrateModel) assertRolledBack(t *testing.T) {
	t.Helper()
	if !m.oldRunning || m.newRunning {
		t.Errorf("old running %v, new running %v", m.oldRunning, m.newRunning)
	}
	if _, err := os.Stat(m.newDir); !os.IsNotExist(err) {
		t.Errorf("the copy stayed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.old.StateDir, "settings.json")); err != nil {
		t.Errorf("old state touched: %v", err)
	}
	steps := strings.Join(m.steps(), "\n")
	if strings.Contains(steps, "rm -f "+m.old.Plist) || strings.Contains(steps, "rm -f "+m.old.Bin) {
		t.Errorf("old install removed during a rollback:\n%s", steps)
	}
	if lc := m.launchctl(); len(lc) == 0 || lc[len(lc)-1] != "bootstrap system /Library/LaunchDaemons/"+m.old.Label+".plist" {
		t.Errorf("old helper not started last: %v", lc)
	}
	if len(m.dropped) != 0 {
		t.Errorf("legacy secrets dropped during a rollback: %v", m.dropped)
	}
}

// Refusals happen before anything is stopped.
func TestMigrateRefuses(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(m *migrateModel)
		want  string
	}{
		{"hardened", func(m *migrateModel) {
			p := filepath.Join(m.old.StateDir, "settings.json")
			b, _ := os.ReadFile(p)
			var st map[string]any
			json.Unmarshal(b, &st)
			st["hardened"] = map[string]any{"plan_id": "h1", "old_label": "com.example.mihomo"}
			b, _ = json.Marshal(st)
			os.WriteFile(p, b, 0o600)
		}, "own LaunchDaemon"},
		{"config names the old dir", func(m *migrateModel) {
			os.WriteFile(filepath.Join(m.root, "mihomo-conf", "config.yaml"), []byte("external-ui: "+m.old.StateDir+"/ui\n"), 0o644)
		}, "names the old state dir"},
		{"new state exists", func(m *migrateModel) {
			os.MkdirAll(m.newDir, 0o755)
			os.WriteFile(filepath.Join(m.newDir, "settings.json"), []byte("{}"), 0o600)
		}, "already holds a helper's settings"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newMigrateModel(t, 0)
			c.setup(m)
			err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes"))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
			if len(m.applied) != 0 || !m.oldRunning {
				t.Errorf("changed something: %v", m.steps())
			}
		})
	}
}

// Only whole JSON strings that are an old name, or start with an old path
// and "/", change.
func TestRewriteStateNames(t *testing.T) {
	d := t.TempDir()
	in := `{"a": "/old/duet", "b": "/old/duet/x", "c": "/old/duet-other", "d": "see /old/duet/y", "l": "com.l3ony2k.duet.helper", "m": "com.l3ony2k.duet.mihomo"}`
	os.WriteFile(filepath.Join(d, "s.json"), []byte(in), 0o600)
	os.MkdirAll(filepath.Join(d, "kernels"), 0o700)
	os.WriteFile(filepath.Join(d, "kernels", "k.json"), []byte(in), 0o600)
	if err := rewriteStateNames(d, map[string]string{"/old/duet": "/new/twinnel", "com.l3ony2k.duet.helper": "com.l3ony2k.twinnel.helper"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "s.json"))
	want := `{"a": "/new/twinnel", "b": "/new/twinnel/x", "c": "/old/duet-other", "d": "see /old/duet/y", "l": "com.l3ony2k.twinnel.helper", "m": "com.l3ony2k.duet.mihomo"}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if k, _ := os.ReadFile(filepath.Join(d, "kernels", "k.json")); string(k) != in {
		t.Errorf("kernels rewritten: %s", k)
	}
}

func TestCopyTreeRefusesNonEmpty(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(dst, "x"), nil, 0o600)
	if err := copyTree(src, dst); err == nil {
		t.Error("copied into a non-empty dir")
	}
}

// Uninstalling while only the old helper is installed removes that one.
func TestUninstallLegacyOnly(t *testing.T) {
	m := newMigrateModel(t, 501)
	if err := runHelperUninstall([]string{"--dry-run"}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	out := m.out.String()
	for _, want := range []string{"bootout system/" + m.old.Label, "/Library/PrivilegedHelperTools/" + m.old.Label, m.old.Socket} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, store.DefaultHelperLabel) {
		t.Errorf("names the new helper:\n%s", out)
	}
}

// After a migration, --purge also deletes the kept old state dir and the
// old keychain items.
func TestPurgeRemovesMigrationLeftovers(t *testing.T) {
	m := newMigrateModel(t, 501)
	newBin, _ := helperPaths(m.newDir, store.DefaultHelperLabel)
	m.exists[newBin] = true // migrated: the new helper is installed
	kept := m.old.StateDir + ".migrated-20261127-100000"
	if err := os.Rename(m.old.StateDir, kept); err != nil {
		t.Fatal(err)
	}
	m.info = &rpc.HelperInfo{StateDir: m.newDir, SecretKeys: []string{testSecret}, Mihomo: rpc.HelperMihomoService{Label: "com.example.mihomo", Adopted: true}}
	helperRPC = func(_ context.Context, _ string, method string, _, result any) error {
		b, _ := json.Marshal(m.info)
		return json.Unmarshal(b, result)
	}
	if err := runHelperUninstall([]string{"--purge", "--dry-run", "--state-dir", m.newDir}); err != nil {
		t.Fatalf("%v\n%s", err, m.out.String())
	}
	out := m.out.String()
	for _, want := range []string{"/bin/rm -rf -- " + kept, "-a duet -s duet." + testSecret, "-a twinnel -s twinnel." + testSecret} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// A new state dir that holds something else (no settings.json) is never
// deleted by a rollback.
func TestMigrateKeepsForeignNewStateDir(t *testing.T) {
	m := newMigrateModel(t, 0)
	os.MkdirAll(m.newDir, 0o755)
	os.WriteFile(filepath.Join(m.newDir, "notes.txt"), []byte("mine"), 0o600)
	err := runHelperUpgrade(m.upgradeArgs("--as-root", "--yes"))
	if err == nil || !strings.Contains(err.Error(), "not empty") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(m.newDir, "notes.txt")); string(b) != "mine" {
		t.Error("foreign file deleted")
	}
	if !m.oldRunning {
		t.Error("old helper not running again")
	}
}
