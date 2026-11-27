// Package scm is the Windows service manager: it renders services for the
// Service Control Manager and turns operations into plans of sc.exe-style
// steps (printable, and what an administrator could type), which Executor
// carries out in process through golang.org/x/sys/windows/svc/mgr.
//
// Programs that are not Windows services themselves (mihomo.exe) run under
// the service host, a copy of twinnel-helper.exe started as
// `twinnel-svchost.exe svchost --name … -- <program> <args>`.
//
// The planning code has no build tag and is tested on every OS; only the
// executor and the status query need Windows.
package scm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// SC is the argv[0] of the plan's service steps.
const SC = "sc.exe"

// Strings the steps wait for: sc.exe's state names (not localised) and the
// error code of a service that does not exist.
const (
	StateStopped   = "STOPPED"
	ErrNoService   = "1060"
	registryPrefix = `HKLM\SYSTEM\CurrentControlSet\Services\`
)

// Manager implements service.Manager for Windows.
type Manager struct {
	// HostSrc is copied to HostBin when a hosted service is installed (the
	// running helper's own executable). Empty: no copy step.
	HostSrc string
	// HostBin is the service host. Default winpath.SvcHostBin().
	HostBin string
	// StopTimeout bounds the wait for STOPPED. Default 30 s.
	StopTimeout time.Duration
	// Query reads a service's state. Default: the SCM (Windows only).
	Query func(ctx context.Context, name string) (service.Status, error)
}

var _ service.Manager = (*Manager)(nil)

// New returns a Manager that copies hostSrc as the service host.
func New(hostSrc string) *Manager { return &Manager{HostSrc: hostSrc} }

func (m *Manager) hostBin() string {
	if m.HostBin != "" {
		return m.HostBin
	}
	return winpath.SvcHostBin()
}

func (m *Manager) stopTimeout() time.Duration {
	if m.StopTimeout > 0 {
		return m.StopTimeout
	}
	return 30 * time.Second
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

// ValidName reports whether s can name a Twinnel-managed service.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// native reports whether the spec's program is a Windows service itself
// (the helper), as opposed to one that runs under the service host.
func native(s service.Spec) bool { return s.Name == "helper" }

// DefinitionPath is the service's registry key (the SCM's database).
func (m *Manager) DefinitionPath(label string) string { return registryPrefix + label }

// BinPath is the service's command line.
func (m *Manager) BinPath(s service.Spec) (string, error) {
	if s.Program == "" {
		return "", errors.New("service has no program")
	}
	if native(s) {
		return CommandLine(append([]string{s.Program}, s.Args...))
	}
	argv := []string{m.hostBin(), "svchost", "--name", s.Label}
	if p := s.StdoutPath; p != "" {
		argv = append(argv, "--log", p)
	} else if p := s.StderrPath; p != "" {
		argv = append(argv, "--log", p)
	}
	if s.KeepAlive {
		argv = append(argv, "--keep-alive")
		if s.ThrottleSeconds > 0 {
			argv = append(argv, "--throttle", fmt.Sprintf("%ds", s.ThrottleSeconds))
		}
	}
	argv = append(argv, "--", s.Program)
	return CommandLine(append(argv, s.Args...))
}

func displayName(s service.Spec) string {
	switch s.Name {
	case "helper":
		return "Twinnel Helper"
	case "":
		return "Twinnel " + s.Label
	}
	return "Twinnel " + s.Name
}

func description(s service.Spec) string {
	switch s.Name {
	case "helper":
		return "Twinnel's privileged helper: writes mihomo configs, kernels and service definitions for the Twinnel app."
	case "mihomo":
		return "mihomo proxy kernel, run and kept alive by Twinnel."
	}
	return "Managed by Twinnel."
}

func account(s service.Spec) string {
	if s.UserName == "" {
		return "LocalSystem"
	}
	return s.UserName
}

func startType(s service.Spec) string {
	if s.RunAtLoad {
		return "auto"
	}
	return "demand"
}

// restartDelayMs is the SCM's restart delay after a crash of the service
// process (launchd's ThrottleInterval).
func restartDelayMs(s service.Spec) int {
	if s.ThrottleSeconds > 0 {
		return s.ThrottleSeconds * 1000
	}
	return 5000
}

// failureActions restarts the service three times, then gives up until
// the counter resets after a day.
func failureActions(s service.Spec) string {
	d := fmt.Sprint(restartDelayMs(s))
	return strings.Join([]string{"restart", d, "restart", d, "restart", d}, "/")
}

// Render describes the service as the SCM will hold it.
func (m *Manager) Render(s service.Spec) ([]byte, error) {
	if !ValidName(s.Label) {
		return nil, fmt.Errorf("invalid service name %q", s.Label)
	}
	bp, err := m.BinPath(s)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "service:      %s\n", s.Label)
	fmt.Fprintf(&b, "display name: %s\n", displayName(s))
	fmt.Fprintf(&b, "command line: %s\n", bp)
	fmt.Fprintf(&b, "start:        %s\n", startType(s))
	fmt.Fprintf(&b, "account:      %s\n", account(s))
	if s.KeepAlive {
		fmt.Fprintf(&b, "on failure:   %s (reset after 86400 s)\n", failureActions(s))
	}
	return []byte(b.String()), nil
}

func sc(args ...string) []string { return append([]string{SC}, args...) }

func run(ignore bool, args ...string) service.Step {
	return service.Step{Kind: service.StepRun, Argv: sc(args...), IgnoreError: ignore}
}

// startAgain starts a service the plan stopped, whatever failed in
// between: never leave mihomo (the network) or the helper down.
func startAgain(name string) service.Step {
	s := run(false, "start", name)
	s.Always = true
	return s
}

// stopSteps stops a service and waits until the SCM reports it stopped
// (or reports that it does not exist): Windows keeps a running .exe
// locked, so nothing may replace the binary before.
func (m *Manager) stopSteps(name string, ignoreStop bool) []service.Step {
	return []service.Step{
		run(ignoreStop, "stop", name),
		{Kind: service.StepWait, Argv: sc("query", name), Until: StateStopped, OrUntil: []string{ErrNoService}, Timeout: m.stopTimeout()},
	}
}

// Plan implements service.Manager.
func (m *Manager) Plan(op service.Op, s service.Spec) ([]service.Step, error) {
	name := s.Label
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid service name %q", name)
	}
	switch op {
	case service.OpInstall:
		if s.Name == "tailscaled" || name == winpath.TailscaleService {
			return nil, errors.New("the Tailscale service belongs to Tailscale's own installer; Twinnel only starts, stops and reads it")
		}
		bp, err := m.BinPath(s)
		if err != nil {
			return nil, err
		}
		steps := m.stopSteps(name, true)
		if !native(s) && m.HostSrc != "" {
			steps = append(steps,
				service.Step{Kind: service.StepMkdir, Path: winpath.Dir(m.hostBin()), Mode: 0o755},
				service.Step{Kind: service.StepCopy, Src: m.HostSrc, Path: m.hostBin(), Mode: 0o755},
			)
		}
		def := []string{"binPath=", bp, "start=", startType(s), "obj=", account(s), "DisplayName=", displayName(s)}
		steps = append(steps,
			run(true, append([]string{"create", name}, def...)...), // fails when it exists; config follows
			run(false, append([]string{"config", name}, def...)...),
			run(false, "description", name, description(s)),
		)
		if s.KeepAlive {
			steps = append(steps,
				run(false, "failure", name, "reset=", "86400", "actions=", failureActions(s)),
				run(false, "failureflag", name, "1"), // also when it exits with an error (helper.restart)
			)
		}
		if s.RunAtLoad {
			steps = append(steps, startAgain(name))
		}
		return steps, nil
	case service.OpUninstall:
		steps := append(m.stopSteps(name, true), run(false, "delete", name))
		if !native(s) {
			// The host copy goes with the last hosted service; a running
			// one keeps it (removing a running .exe fails, ignored).
			steps = append(steps, service.Step{Kind: service.StepRemove, Path: m.hostBin(), IgnoreError: true})
		}
		return steps, nil
	case service.OpStart:
		return []service.Step{run(false, "start", name)}, nil
	case service.OpStop:
		return m.stopSteps(name, false), nil
	case service.OpRestart:
		return append(m.stopSteps(name, true), startAgain(name)), nil
	case service.OpDisable:
		return []service.Step{run(false, "config", name, "start=", "disabled")}, nil
	case service.OpEnable:
		return []service.Step{run(false, "config", name, "start=", "auto")}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", op)
}

// Status reads the service's state from the SCM (read-only).
func (m *Manager) Status(ctx context.Context, label string) (service.Status, error) {
	q := m.Query
	if q == nil {
		q = queryService
	}
	st, err := q(ctx, label)
	if st.Label == "" {
		st.Label = label
	}
	if st.Installed && st.Definition == "" {
		st.Definition = m.DefinitionPath(label)
	}
	return st, err
}
