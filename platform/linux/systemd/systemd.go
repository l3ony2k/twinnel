// Package systemd implements service.Manager for systemd system units: it
// renders unit files, plans systemctl calls and parses `systemctl show`.
// It also parses existing unit files (read-only) for discovery.
//
// Nothing here runs systemctl by itself except Status (a read); mutations
// happen only when a caller applies a Plan with a real Executor. The package
// has no build tag so its tests run on every platform.
package systemd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/service"
)

// DefaultDir is where administrator-installed system units live.
const DefaultDir = "/etc/systemd/system"

// Systemd is a service.Manager for the system instance of systemd.
type Systemd struct {
	Dir       string           // default /etc/systemd/system
	Systemctl string           // default systemctl (looked up in PATH)
	Exec      service.Executor // used by Status only
	// Stat is os.Stat (replaceable for tests).
	Stat func(string) (fs.FileInfo, error)
}

// New returns a Systemd with the real executor.
func New() *Systemd {
	return &Systemd{Dir: DefaultDir, Systemctl: "systemctl", Exec: service.ExecExecutor{}}
}

var _ service.Manager = (*Systemd)(nil)

// Unit names: the label is the unit name without ".service".
var labelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,200}$`)

func checkLabel(l string) error {
	if !labelRE.MatchString(l) || strings.HasSuffix(l, ".service") {
		return fmt.Errorf("invalid systemd unit label %q (name without .service)", l)
	}
	return nil
}

func unit(label string) string { return label + ".service" }

func (s *Systemd) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return DefaultDir
}

func (s *Systemd) systemctl() string {
	if s.Systemctl != "" {
		return s.Systemctl
	}
	return "systemctl"
}

// DefinitionPath implements service.Manager.
func (s *Systemd) DefinitionPath(label string) string {
	return filepath.Join(s.dir(), unit(label))
}

// Render implements service.Manager.
//
// Mapping from Spec: KeepAlive → Restart=always, ThrottleSeconds →
// RestartSec, RunAtLoad → an [Install] section (the unit is enabled at
// install), Stdout/StderrPath → StandardOutput=append:... (systemd ≥ 240),
// UserName → User= (root when empty, so no line).
func (s *Systemd) Render(sp service.Spec) ([]byte, error) {
	if err := checkLabel(sp.Label); err != nil {
		return nil, err
	}
	if sp.Program == "" || !filepath.IsAbs(sp.Program) {
		return nil, fmt.Errorf("program must be an absolute path: %q", sp.Program)
	}
	for _, p := range []string{sp.WorkingDir, sp.StdoutPath, sp.StderrPath} {
		if strings.ContainsAny(p, "\n") {
			return nil, fmt.Errorf("path contains a newline: %q", p)
		}
	}
	var b bytes.Buffer
	name := sp.Name
	if name == "" {
		name = sp.Label
	}
	fmt.Fprintf(&b, "# Written by Twinnel. Local changes are overwritten; use a drop-in instead.\n")
	fmt.Fprintf(&b, "[Unit]\nDescription=twinnel: %s\n", escapeValue(name))
	b.WriteString("Wants=network-online.target\nAfter=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\n")
	args := append([]string{sp.Program}, sp.Args...)
	q := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, "\n") {
			return nil, fmt.Errorf("argument contains a newline: %q", a)
		}
		q[i] = quoteWord(a)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(q, " "))
	if sp.WorkingDir != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", escapeValue(sp.WorkingDir))
	}
	if len(sp.Env) > 0 {
		ks := make([]string, 0, len(sp.Env))
		for k := range sp.Env {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			if strings.ContainsAny(k+sp.Env[k], "\n") || strings.ContainsAny(k, "= ") {
				return nil, fmt.Errorf("invalid environment entry %q", k)
			}
			fmt.Fprintf(&b, "Environment=%s\n", quoteWord(k+"="+sp.Env[k]))
		}
	}
	if sp.KeepAlive {
		b.WriteString("Restart=always\n")
		if sp.ThrottleSeconds > 0 {
			fmt.Fprintf(&b, "RestartSec=%d\n", sp.ThrottleSeconds)
		}
	} else {
		b.WriteString("Restart=no\n")
	}
	if sp.StdoutPath != "" {
		fmt.Fprintf(&b, "StandardOutput=append:%s\n", escapeValue(sp.StdoutPath))
	}
	if sp.StderrPath != "" {
		fmt.Fprintf(&b, "StandardError=append:%s\n", escapeValue(sp.StderrPath))
	}
	if sp.UserName != "" && sp.UserName != "root" {
		fmt.Fprintf(&b, "User=%s\n", escapeValue(sp.UserName))
	}
	if sp.RunAtLoad {
		b.WriteString("\n[Install]\nWantedBy=multi-user.target\n")
	}
	return b.Bytes(), nil
}

// escapeValue escapes specifiers (%) for a plain unit setting.
func escapeValue(s string) string { return strings.ReplaceAll(s, "%", "%%") }

// quoteWord quotes one word of a command line or Environment= value:
// specifiers (%) and variable expansion ($) are escaped, and words with
// spaces, quotes or backslashes are double-quoted with C-style escapes.
func quoteWord(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// Plan implements service.Manager.
func (s *Systemd) Plan(op service.Op, sp service.Spec) ([]service.Step, error) {
	if err := checkLabel(sp.Label); err != nil {
		return nil, err
	}
	sc := s.systemctl()
	u := unit(sp.Label)
	path := s.DefinitionPath(sp.Label)
	run := func(ignore bool, args ...string) service.Step {
		return service.Step{Kind: service.StepRun, Argv: append([]string{sc}, args...), IgnoreError: ignore}
	}
	switch op {
	case service.OpInstall:
		content, err := s.Render(sp)
		if err != nil {
			return nil, err
		}
		var steps []service.Step
		seen := map[string]bool{}
		for _, p := range []string{sp.StdoutPath, sp.StderrPath} {
			if d := filepath.Dir(p); p != "" && !seen[d] {
				seen[d] = true
				steps = append(steps, service.Step{Kind: service.StepMkdir, Path: d, Mode: 0o755})
			}
		}
		steps = append(steps,
			service.Step{Kind: service.StepWrite, Path: path, Mode: 0o644, Content: content},
			run(false, "daemon-reload"),
		)
		if sp.RunAtLoad {
			// restart starts a stopped unit and replaces a running older one.
			steps = append(steps, run(false, "enable", u), run(false, "restart", u))
		}
		return steps, nil
	case service.OpUninstall:
		return []service.Step{
			run(true, "disable", "--now", u),
			{Kind: service.StepRemove, Path: path},
			run(false, "daemon-reload"),
		}, nil
	case service.OpStart:
		return []service.Step{run(false, "start", u)}, nil
	case service.OpStop:
		// Restart=always does not restart a unit stopped through systemctl.
		return []service.Step{run(false, "stop", u)}, nil
	case service.OpRestart:
		return []service.Step{run(false, "restart", u)}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", op)
}

// ShowProperties are the properties Status asks `systemctl show` for.
var ShowProperties = []string{
	"LoadState", "ActiveState", "SubState", "MainPID", "ExecMainStatus",
	"NRestarts", "FragmentPath", "ExecStart", "UnitFileState",
}

// Status implements service.Manager by running `systemctl show` (read-only).
func (s *Systemd) Status(ctx context.Context, label string) (service.Status, error) {
	if err := checkLabel(label); err != nil {
		return service.Status{}, err
	}
	st := service.Status{Label: label, Definition: s.DefinitionPath(label)}
	stat := s.Stat
	if stat == nil {
		stat = os.Stat
	}
	if _, err := stat(st.Definition); err == nil {
		st.Installed = true
	}
	ex := s.Exec
	if ex == nil {
		ex = service.ExecExecutor{}
	}
	out, err := ex.Run(ctx, []string{s.systemctl(), "show", unit(label), "--no-pager",
		"--property=" + strings.Join(ShowProperties, ",")})
	if err != nil {
		return st, fmt.Errorf("systemctl show: %w: %s", err, strings.TrimSpace(string(out)))
	}
	applyShow(&st, ParseShow(out))
	return st, nil
}

func applyShow(st *service.Status, p map[string]string) {
	if p["LoadState"] != "loaded" {
		return // "not-found": no such unit
	}
	st.Loaded = true
	st.Running = p["ActiveState"] == "active" && p["SubState"] == "running"
	st.PID, _ = strconv.Atoi(p["MainPID"])
	if !st.Running {
		st.PID = 0
		if c, err := strconv.Atoi(p["ExecMainStatus"]); err == nil {
			st.LastExitCode = &c
		}
	}
	// launchd counts runs; systemd counts automatic restarts.
	st.Runs, _ = strconv.Atoi(p["NRestarts"])
	st.Program = ExecPath(p["ExecStart"])
	if fp := p["FragmentPath"]; fp != "" {
		st.Definition = fp
		st.Installed = true
	}
}

// ParseShow parses `systemctl show` output (Key=Value lines).
func ParseShow(out []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			if _, dup := m[k]; !dup {
				m[k] = v
			}
		}
	}
	return m
}

// ExecPath extracts path= from systemctl's ExecStart property, e.g.
// "{ path=/usr/bin/x ; argv[]=/usr/bin/x -d /etc/x ; ... }".
func ExecPath(v string) string {
	i := strings.Index(v, "path=")
	if i < 0 {
		return ""
	}
	rest := v[i+len("path="):]
	if j := strings.Index(rest, " ;"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}
