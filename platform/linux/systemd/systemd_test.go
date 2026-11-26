package systemd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/service"
)

type fakeExec struct {
	calls [][]string
	out   []byte
	err   error
}

func (f *fakeExec) Run(ctx context.Context, argv []string) ([]byte, error) {
	f.calls = append(f.calls, argv)
	return f.out, f.err
}

func TestRenderUnit(t *testing.T) {
	s := &Systemd{Dir: t.TempDir()}
	spec := service.MihomoSpec("duet-mihomo", "/opt/duet/kernels/mihomo/current/mihomo", "/var/lib/duet/mihomo dir", "/var/lib/duet/mihomo dir/config.yaml", "/var/log/duet")
	spec.Env = map[string]string{"SAFE_PATHS": "/a 100%$HOME"}
	b, err := s.Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"[Unit]\nDescription=duet: mihomo\n",
		`ExecStart=/opt/duet/kernels/mihomo/current/mihomo -d "/var/lib/duet/mihomo dir" -f "/var/lib/duet/mihomo dir/config.yaml"` + "\n",
		`Environment="SAFE_PATHS=/a 100%%$$HOME"` + "\n",
		"Restart=always\nRestartSec=10\n",
		"StandardOutput=append:/var/log/duet/mihomo.out.log\n",
		"StandardError=append:/var/log/duet/mihomo.err.log\n",
		"[Install]\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "User=") {
		t.Error("root needs no User=")
	}
	// The rendered file parses back to the same command line.
	u := ParseUnitFile(b)
	argv := SplitExec(u.Get("Service", "ExecStart"))
	if !reflect.DeepEqual(argv, append([]string{spec.Program}, spec.Args...)) {
		t.Errorf("round trip: %q", argv)
	}
	if env := SplitExec(u.Get("Service", "Environment")); len(env) != 1 || env[0] != "SAFE_PATHS=/a 100%$HOME" {
		t.Errorf("env round trip: %q", env)
	}

	if _, err := s.Render(service.Spec{Label: "x", Program: "relative"}); err == nil {
		t.Error("relative program accepted")
	}
	if _, err := s.Render(service.Spec{Label: "x.service", Program: "/bin/x"}); err == nil {
		t.Error("label with .service accepted")
	}
	if _, err := s.Render(service.Spec{Label: "x", Program: "/bin/x", Args: []string{"a\nExecStartPre=/bin/evil"}}); err == nil {
		t.Error("newline in argument accepted")
	}
	b, _ = s.Render(service.Spec{Label: "x", Program: "/bin/x", UserName: "nobody"})
	if !strings.Contains(string(b), "User=nobody\n") || !strings.Contains(string(b), "Restart=no\n") || strings.Contains(string(b), "[Install]") {
		t.Errorf("plain spec:\n%s", b)
	}
}

func TestPlan(t *testing.T) {
	s := &Systemd{Dir: "/etc/systemd/system", Systemctl: "/usr/bin/systemctl"}
	spec := service.HelperSpec("duet-helper", "/usr/local/libexec/duet-helper", "/var/lib/duet", "/run/duet/helper.sock", "/var/log/duet")
	steps, err := s.Plan(service.OpInstall, spec)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, st := range steps {
		lines = append(lines, st.String())
	}
	want := []string{
		"install -d -m 0755 /var/log/duet",
		"write /etc/systemd/system/duet-helper.service (mode 0644, " + strconv.Itoa(len(steps[1].Content)) + " bytes)",
		"/usr/bin/systemctl daemon-reload",
		"/usr/bin/systemctl enable duet-helper.service",
		"/usr/bin/systemctl restart duet-helper.service",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("install plan:\n%s", strings.Join(lines, "\n"))
	}
	for op, want := range map[service.Op]string{
		service.OpUninstall: "/usr/bin/systemctl disable --now duet-helper.service  # errors ignored|rm -f /etc/systemd/system/duet-helper.service|/usr/bin/systemctl daemon-reload",
		service.OpStart:     "/usr/bin/systemctl start duet-helper.service",
		service.OpStop:      "/usr/bin/systemctl stop duet-helper.service",
		service.OpRestart:   "/usr/bin/systemctl restart duet-helper.service",
	} {
		steps, err := s.Plan(op, service.Spec{Label: "duet-helper"})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, st := range steps {
			got = append(got, st.String())
		}
		if strings.Join(got, "|") != want {
			t.Errorf("%s: %q", op, got)
		}
	}
	if _, err := s.Plan("explode", spec); err == nil {
		t.Error("unknown op accepted")
	}
}

func TestApplyInstallWithFakes(t *testing.T) {
	dir := t.TempDir()
	s := &Systemd{Dir: dir, Systemctl: "systemctl"}
	steps, err := s.Plan(service.OpInstall, service.TailscaledSpec("duet-tailscaled", "/usr/sbin/tailscaled"))
	if err != nil {
		t.Fatal(err)
	}
	ex := &fakeExec{}
	if err := service.Apply(context.Background(), steps, ex, service.OSFiles{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.DefinitionPath("duet-tailscaled"))
	if err != nil || !strings.Contains(string(b), "ExecStart=/usr/sbin/tailscaled\n") {
		t.Fatalf("%v\n%s", err, b)
	}
	if len(ex.calls) != 3 || ex.calls[2][1] != "restart" {
		t.Errorf("%q", ex.calls)
	}
}

func TestStatus(t *testing.T) {
	running, _ := os.ReadFile("testdata/show_running.txt")
	failed, _ := os.ReadFile("testdata/show_failed.txt")
	missing, _ := os.ReadFile("testdata/show_notfound.txt")
	noFile := func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist }

	ex := &fakeExec{out: running}
	s := &Systemd{Exec: ex, Stat: noFile}
	st, err := s.Status(context.Background(), "duet-mihomo")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Loaded || !st.Running || st.PID != 4242 || st.LastExitCode != nil || st.Runs != 2 ||
		st.Program != "/opt/duet/kernels/mihomo/current/mihomo" || st.Definition != "/etc/systemd/system/duet-mihomo.service" || !st.Installed {
		t.Errorf("running: %+v", st)
	}
	if got := strings.Join(ex.calls[0], " "); !strings.HasPrefix(got, "systemctl show duet-mihomo.service --no-pager --property=LoadState,") {
		t.Errorf("command: %s", got)
	}

	s.Exec = &fakeExec{out: failed}
	st, _ = s.Status(context.Background(), "duet-mihomo")
	if !st.Loaded || st.Running || st.PID != 0 || st.LastExitCode == nil || *st.LastExitCode != 1 {
		t.Errorf("failed: %+v", st)
	}

	s.Exec = &fakeExec{out: missing}
	st, _ = s.Status(context.Background(), "nope")
	if st.Loaded || st.Installed || st.Running {
		t.Errorf("missing: %+v", st)
	}

	s.Exec = &fakeExec{out: []byte("Failed to connect to bus"), err: errors.New("exit status 1")}
	if _, err := s.Status(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "bus") {
		t.Errorf("err: %v", err)
	}
	if _, err := s.Status(context.Background(), "../x"); err == nil {
		t.Error("bad label accepted")
	}
}

func TestParseUnitFile(t *testing.T) {
	b, err := os.ReadFile("testdata/tailscaled.service")
	if err != nil {
		t.Fatal(err)
	}
	u := ParseUnitFile(b)
	argv := SplitExec(u.Get("Service", "ExecStart"))
	want := []string{"/usr/sbin/tailscaled", "--state=/var/lib/tailscale/tailscaled.state", "--socket=/run/tailscale/tailscaled.sock", "--port=${PORT}", "$FLAGS"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("ExecStart: %q", argv)
	}
	if u.Get("Unit", "Description") != "Tailscale node agent" || len(u["Service"]["ExecStopPost"]) != 1 {
		t.Errorf("%v", u)
	}
	b, _ = os.ReadFile("testdata/mihomo.service")
	u = ParseUnitFile(b)
	if got := SplitExec(u.Get("Service", "ExecStart")); !reflect.DeepEqual(got, []string{"/usr/local/bin/mihomo", "-d", "/etc/mihomo", "-f", "/etc/mihomo/my config.yaml"}) {
		t.Errorf("continuation/quotes: %q", got)
	}
	if u.Get("Service", "User") != "mihomo" {
		t.Errorf("user: %v", u["Service"])
	}
	if got := u["Service"]["ExecStartPre"]; got != nil {
		t.Errorf("empty value must reset the list: %q", got)
	}
}

func TestSplitExec(t *testing.T) {
	for in, want := range map[string][]string{
		`-/bin/x a  'b c' "d \"e\""`: {"/bin/x", "a", "b c", `d "e"`},
		`@/bin/x x0 --arg=a\ b`:      {"/bin/x", "x0", "--arg=a b"},
		``:                           nil,
	} {
		if got := SplitExec(in); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%s: %q", in, got)
		}
	}
	if p := ExecPath("{ path=/usr/bin/x ; argv[]=/usr/bin/x -d /etc/x ; ignore_errors=no }"); p != "/usr/bin/x" {
		t.Error(p)
	}
}
