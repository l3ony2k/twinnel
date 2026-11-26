//go:build darwin

// Package launchd implements service.Manager for macOS LaunchDaemons:
// it renders plists, plans launchctl calls and parses
// `launchctl print system/<label>`.
//
// Nothing in this package runs launchctl by itself except Status (a read);
// mutations happen only when a caller applies a Plan with a real Executor.
package launchd

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/l3ony2k/duet/core/service"
)

// DefaultDir is where system LaunchDaemons live.
const DefaultDir = "/Library/LaunchDaemons"

// Launchd is a service.Manager for the system domain.
type Launchd struct {
	Dir       string           // default /Library/LaunchDaemons
	Launchctl string           // default /bin/launchctl
	Exec      service.Executor // used by Status only
	// Stat is os.Stat (replaceable for tests).
	Stat func(string) (fs.FileInfo, error)
}

// New returns a Launchd with the real executor.
func New() *Launchd {
	return &Launchd{Dir: DefaultDir, Launchctl: "/bin/launchctl", Exec: service.ExecExecutor{}}
}

var _ service.Manager = (*Launchd)(nil)

var labelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func checkLabel(l string) error {
	if !labelRE.MatchString(l) {
		return fmt.Errorf("invalid launchd label %q", l)
	}
	return nil
}

func (l *Launchd) dir() string {
	if l.Dir != "" {
		return l.Dir
	}
	return DefaultDir
}

func (l *Launchd) launchctl() string {
	if l.Launchctl != "" {
		return l.Launchctl
	}
	return "/bin/launchctl"
}

// DefinitionPath implements service.Manager.
func (l *Launchd) DefinitionPath(label string) string {
	return filepath.Join(l.dir(), label+".plist")
}

// Render implements service.Manager.
func (l *Launchd) Render(s service.Spec) ([]byte, error) {
	if err := checkLabel(s.Label); err != nil {
		return nil, err
	}
	if s.Program == "" || !filepath.IsAbs(s.Program) {
		return nil, fmt.Errorf("program must be an absolute path: %q", s.Program)
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	key := func(k string) { fmt.Fprintf(&b, "\t<key>%s</key>\n", esc(k)) }
	str := func(indent, v string) { fmt.Fprintf(&b, "%s<string>%s</string>\n", indent, esc(v)) }
	boolean := func(v bool) {
		if v {
			b.WriteString("\t<true/>\n")
		} else {
			b.WriteString("\t<false/>\n")
		}
	}
	key("Label")
	str("\t", s.Label)
	key("ProgramArguments")
	b.WriteString("\t<array>\n")
	for _, a := range append([]string{s.Program}, s.Args...) {
		str("\t\t", a)
	}
	b.WriteString("\t</array>\n")
	if s.WorkingDir != "" {
		key("WorkingDirectory")
		str("\t", s.WorkingDir)
	}
	if len(s.Env) > 0 {
		key("EnvironmentVariables")
		b.WriteString("\t<dict>\n")
		ks := make([]string, 0, len(s.Env))
		for k := range s.Env {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Fprintf(&b, "\t\t<key>%s</key>\n", esc(k))
			str("\t\t", s.Env[k])
		}
		b.WriteString("\t</dict>\n")
	}
	key("RunAtLoad")
	boolean(s.RunAtLoad)
	key("KeepAlive")
	boolean(s.KeepAlive)
	if s.ThrottleSeconds > 0 {
		key("ThrottleInterval")
		fmt.Fprintf(&b, "\t<integer>%d</integer>\n", s.ThrottleSeconds)
	}
	if s.StdoutPath != "" {
		key("StandardOutPath")
		str("\t", s.StdoutPath)
	}
	if s.StderrPath != "" {
		key("StandardErrorPath")
		str("\t", s.StderrPath)
	}
	key("UserName")
	str("\t", orDefault(s.UserName, "root"))
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes(), nil
}

func esc(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Plan implements service.Manager.
func (l *Launchd) Plan(op service.Op, s service.Spec) ([]service.Step, error) {
	if err := checkLabel(s.Label); err != nil {
		return nil, err
	}
	lc := l.launchctl()
	target := "system/" + s.Label
	path := l.DefinitionPath(s.Label)
	run := func(ignore bool, args ...string) service.Step {
		return service.Step{Kind: service.StepRun, Argv: append([]string{lc}, args...), IgnoreError: ignore}
	}
	switch op {
	case service.OpInstall:
		plist, err := l.Render(s)
		if err != nil {
			return nil, err
		}
		var steps []service.Step
		for _, p := range []string{s.StdoutPath, s.StderrPath} {
			if p != "" {
				steps = append(steps, service.Step{Kind: service.StepMkdir, Path: filepath.Dir(p), Mode: 0o755})
			}
		}
		steps = dedupeMkdir(steps)
		return append(steps,
			run(true, "bootout", target), // replace a loaded older definition
			service.Step{Kind: service.StepWrite, Path: path, Mode: 0o644, Content: plist},
			run(false, "bootstrap", "system", path),
			run(false, "enable", target),
		), nil
	case service.OpUninstall:
		return []service.Step{
			run(true, "bootout", target),
			{Kind: service.StepRemove, Path: path},
		}, nil
	case service.OpStart:
		return []service.Step{
			run(true, "bootstrap", "system", path), // fails harmlessly if already loaded
			run(false, "kickstart", target),
		}, nil
	case service.OpStop:
		// With KeepAlive, killing the process only restarts it; unloading
		// stops it until the next start or boot.
		return []service.Step{run(false, "bootout", target)}, nil
	case service.OpRestart:
		return []service.Step{run(false, "kickstart", "-k", target)}, nil
	case service.OpDisable:
		// Persists in launchd's override database: the plist stays in
		// place but is not loaded at boot (`launchctl enable` undoes it).
		return []service.Step{run(false, "disable", target)}, nil
	case service.OpEnable:
		return []service.Step{run(false, "enable", target)}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", op)
}

func dedupeMkdir(steps []service.Step) []service.Step {
	seen := map[string]bool{}
	out := steps[:0]
	for _, s := range steps {
		if seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		out = append(out, s)
	}
	return out
}

// Status implements service.Manager by running `launchctl print
// system/<label>` (read-only).
func (l *Launchd) Status(ctx context.Context, label string) (service.Status, error) {
	if err := checkLabel(label); err != nil {
		return service.Status{}, err
	}
	st := service.Status{Label: label, Definition: l.DefinitionPath(label)}
	stat := l.Stat
	if stat == nil {
		stat = os.Stat
	}
	if _, err := stat(st.Definition); err == nil {
		st.Installed = true
	}
	ex := l.Exec
	if ex == nil {
		ex = service.ExecExecutor{}
	}
	out, err := ex.Run(ctx, []string{l.launchctl(), "print", "system/" + label})
	if err != nil {
		if bytes.Contains(out, []byte("Could not find service")) {
			return st, nil
		}
		return st, fmt.Errorf("launchctl print: %w: %s", err, strings.TrimSpace(string(out)))
	}
	p := ParsePrint(out)
	st.Loaded = true
	st.Running = p["state"] == "running"
	st.PID, _ = strconv.Atoi(p["pid"])
	if c, err := strconv.Atoi(p["last exit code"]); err == nil {
		st.LastExitCode = &c
	}
	st.Runs, _ = strconv.Atoi(p["runs"])
	st.Program = p["program"]
	if path := p["path"]; path != "" {
		st.Definition = path
		st.Installed = true
	}
	return st, nil
}

// ParsePrint returns the top-level `key = value` pairs of `launchctl print`
// output; nested blocks ({ ... }) are skipped.
func ParsePrint(out []byte) map[string]string {
	m := map[string]string{}
	depth := 0
	for _, line := range strings.Split(string(out), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if t == "}" {
			depth--
			continue
		}
		if strings.HasSuffix(t, "{") { // "system/x = {", "arguments = {", ...
			depth++
			continue
		}
		k, v, ok := strings.Cut(t, " = ")
		if ok && depth == 1 {
			if _, dup := m[k]; !dup {
				m[k] = v
			}
		}
	}
	return m
}
