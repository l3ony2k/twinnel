package scm

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/quick"

	"github.com/l3ony2k/twinnel/core/service"
)

const (
	hostBin  = `C:\Program Files\Twinnel\twinnel-svchost.exe`
	stateDir = `C:\ProgramData\Twinnel`
)

func newTestManager() *Manager {
	return &Manager{HostSrc: `C:\Program Files\Twinnel\twinnel-helper.exe`, HostBin: hostBin}
}

func TestEscapeArgRoundTrip(t *testing.T) {
	cases := [][]string{
		{`C:\Program Files\Twinnel\twinnel-helper.exe`, "--state-dir", `C:\ProgramData\Twinnel`},
		{"a", "", `"quoted"`, `trailing\`, `with space\`, `back\\"slash`},
	}
	for _, argv := range cases {
		cl, err := CommandLine(argv)
		if err != nil {
			t.Fatal(err)
		}
		if got := SplitCommandLine(cl); !slices.Equal(got, argv) {
			t.Errorf("%q → %s → %q", argv, cl, got)
		}
	}
	f := func(a, b string) bool {
		for _, s := range []string{a, b} {
			if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
				return true // refused, checked below
			}
		}
		cl, err := CommandLine([]string{"prog", a, b})
		return err == nil && slices.Equal(SplitCommandLine(cl), []string{"prog", a, b})
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
	if _, err := CommandLine([]string{"x", "a\nb"}); err == nil {
		t.Error("newline accepted")
	}
	if _, err := CommandLine([]string{"x", "a\x00b"}); err == nil {
		t.Error("NUL accepted")
	}
}

func argvs(steps []service.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.String())
	}
	return out
}

func TestHelperInstallPlan(t *testing.T) {
	m := newTestManager()
	spec := service.HelperSpec("TwinnelHelper", `C:\Program Files\Twinnel\twinnel-helper.exe`, stateDir, `\\.\pipe\twinnel-helper`, "")
	steps, err := m.Plan(service.OpInstall, spec)
	if err != nil {
		t.Fatal(err)
	}
	// Stop and wait for STOPPED (or "does not exist") before anything
	// else: the old exe stays locked while it runs.
	if steps[0].Kind != service.StepRun || !slices.Equal(steps[0].Argv, []string{SC, "stop", "TwinnelHelper"}) || !steps[0].IgnoreError {
		t.Fatalf("first step %v", steps[0])
	}
	if w := steps[1]; w.Kind != service.StepWait || w.Until != StateStopped || !slices.Contains(w.OrUntil, ErrNoService) {
		t.Fatalf("second step %v", w)
	}
	// The helper is a service itself: no host copy.
	for _, s := range steps {
		if s.Kind == service.StepCopy {
			t.Errorf("native service copies %s", s.Path)
		}
	}
	var create, cfg Command
	var sawStart, sawFlag bool
	for _, s := range steps {
		if s.Kind != service.StepRun {
			continue
		}
		c, err := ParseCommand(s.Argv)
		if err != nil {
			t.Fatalf("%v: %v", s.Argv, err)
		}
		switch c.Verb {
		case "create":
			create = c
			if !s.IgnoreError {
				t.Error("create must tolerate an existing service")
			}
		case "config":
			cfg = c
		case "start":
			sawStart = true
		case "failureflag":
			sawFlag = true
		case "failure":
			acts, err := ParseActions(c.Opts["actions"])
			if err != nil || len(acts) != 3 || !acts[0].Restart || acts[0].Delay.Seconds() != 5 {
				t.Errorf("failure actions %v %v", acts, err)
			}
		}
	}
	if !sawStart || !sawFlag {
		t.Errorf("start %v, failureflag %v", sawStart, sawFlag)
	}
	if create.Opts["binpath"] != cfg.Opts["binpath"] || create.Opts["start"] != "auto" || cfg.Opts["obj"] != "LocalSystem" {
		t.Errorf("create %v config %v", create.Opts, cfg.Opts)
	}
	want := []string{`C:\Program Files\Twinnel\twinnel-helper.exe`, "--state-dir", stateDir, "--socket", `\\.\pipe\twinnel-helper`}
	if got := SplitCommandLine(cfg.Opts["binpath"]); !slices.Equal(got, want) {
		t.Errorf("binPath %q, want %q", got, want)
	}
}

func TestMihomoRunsUnderTheHost(t *testing.T) {
	m := newTestManager()
	spec := service.MihomoSpec("TwinnelMihomo", stateDir+`\kernels\current\mihomo.exe`, stateDir+`\mihomo`, stateDir+`\mihomo\config.yaml`, stateDir+`\logs`)
	steps, err := m.Plan(service.OpInstall, spec)
	if err != nil {
		t.Fatal(err)
	}
	var copied bool
	var bp string
	for _, s := range steps {
		if s.Kind == service.StepCopy && s.Path == hostBin && s.Src == m.HostSrc {
			copied = true
		}
		if s.Kind == service.StepRun && s.Argv[1] == "config" {
			c, _ := ParseCommand(s.Argv)
			bp = c.Opts["binpath"]
		}
	}
	if !copied {
		t.Error("the host is not copied")
	}
	want := []string{hostBin, "svchost", "--name", "TwinnelMihomo", "--log", filepath.Join(stateDir+`\logs`, "mihomo.out.log"), // spec paths come from filepath.Join
		"--keep-alive", "--throttle", "10s", "--",
		stateDir + `\kernels\current\mihomo.exe`, "-d", stateDir + `\mihomo`, "-f", stateDir + `\mihomo\config.yaml`}
	if got := SplitCommandLine(bp); !slices.Equal(got, want) {
		t.Errorf("binPath\n got %q\nwant %q", got, want)
	}
	def, err := m.Render(spec)
	if err != nil || !strings.Contains(string(def), "svchost --name TwinnelMihomo") {
		t.Errorf("render: %s %v", def, err)
	}

	un, _ := m.Plan(service.OpUninstall, service.Spec{Label: "TwinnelMihomo"})
	if last := un[len(un)-1]; last.Kind != service.StepRemove || last.Path != hostBin || !last.IgnoreError {
		t.Errorf("uninstall ends with %v", last)
	}
	if !slices.Contains(argvs(un), "sc.exe delete TwinnelMihomo") {
		t.Errorf("uninstall %q", argvs(un))
	}
}

func TestPlansRefuse(t *testing.T) {
	m := newTestManager()
	if _, err := m.Plan(service.OpInstall, service.TailscaledSpec("Tailscale", `C:\x\tailscaled.exe`)); err == nil {
		t.Error("installing Tailscale's service was planned")
	}
	for _, bad := range []string{"", "a b", `x\y`, "x;y", strings.Repeat("a", 81)} {
		if _, err := m.Plan(service.OpStart, service.Spec{Label: bad}); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := ParseCommand([]string{SC, "sdset", "X", "D:"}); err == nil {
		t.Error("sdset accepted")
	}
	if _, err := ParseCommand([]string{"cmd.exe", "/c", "x"}); err == nil {
		t.Error("cmd.exe parsed as sc")
	}
}

func TestRestartStopStatus(t *testing.T) {
	m := newTestManager()
	r, _ := m.Plan(service.OpRestart, service.Spec{Label: "Tailscale"})
	if got := argvs(r); len(got) != 3 || got[2] != "sc.exe start Tailscale" {
		t.Errorf("restart %q", got)
	}
	s, _ := m.Plan(service.OpStop, service.Spec{Label: "TwinnelMihomo"})
	if s[0].IgnoreError {
		t.Error("an explicit stop must report errors")
	}
	m.Query = func(context.Context, string) (service.Status, error) {
		return service.Status{Installed: true, Running: true, PID: 42}, nil
	}
	st, err := m.Status(context.Background(), "TwinnelMihomo")
	if err != nil || st.Label != "TwinnelMihomo" || st.Definition != `HKLM\SYSTEM\CurrentControlSet\Services\TwinnelMihomo` {
		t.Errorf("%+v %v", st, err)
	}
}

func TestQueryTextMatchesWaits(t *testing.T) {
	if !strings.Contains(QueryText("X", 1, 0), StateStopped) || strings.Contains(QueryText("X", 4, 9), StateStopped) {
		t.Error("STOPPED")
	}
	if !strings.Contains(NoServiceText(), ErrNoService) {
		t.Error("1060")
	}
	// A wait step finishes on either.
	m := newTestManager()
	steps := m.stopSteps("X", true)
	calls := 0
	ex := fakeExec(func(argv []string) ([]byte, error) {
		calls++
		if argv[1] == "query" {
			return []byte(NoServiceText()), nil
		}
		return nil, nil
	})
	if err := service.Apply(context.Background(), steps, ex, nil); err != nil || calls != 2 {
		t.Errorf("err %v calls %d", err, calls)
	}
}

type fakeExec func([]string) ([]byte, error)

func (f fakeExec) Run(_ context.Context, argv []string) ([]byte, error) { return f(argv) }
