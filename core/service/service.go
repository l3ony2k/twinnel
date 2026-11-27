// Package service is the platform-neutral model of the system services Twinnel
// manages (mihomo, tailscaled, the helper itself): a Spec describes what to
// run, a Manager renders it to the platform's definition (launchd plist,
// later systemd unit / Windows service) and turns operations into an
// explicit Plan of steps. Plans can be printed (dry run) or applied through
// an Executor and a Files implementation, both replaceable in tests.
package service

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Spec describes one service.
type Spec struct {
	Name       string // "mihomo", "tailscaled", "helper"
	Label      string // launchd label / unit name
	Program    string
	Args       []string // not including Program
	WorkingDir string
	Env        map[string]string
	StdoutPath string
	StderrPath string
	RunAtLoad  bool
	KeepAlive  bool
	// ThrottleSeconds is the minimum time between restarts (launchd
	// ThrottleInterval). 0 leaves the platform default.
	ThrottleSeconds int
	UserName        string // empty: root
}

// Status is the runtime state of a service.
type Status struct {
	Label        string
	Installed    bool // definition file present
	Loaded       bool
	Running      bool
	PID          int
	LastExitCode *int
	Runs         int
	Program      string
	Definition   string
}

// Op is a service operation.
type Op string

const (
	OpInstall   Op = "install"
	OpUninstall Op = "uninstall"
	OpStart     Op = "start"
	OpStop      Op = "stop"
	OpRestart   Op = "restart"
	// OpDisable keeps a service from loading at boot without removing its
	// definition; OpEnable undoes it. Not exposed over RPC.
	OpDisable Op = "disable"
	OpEnable  Op = "enable"
)

// StepKind says what a Step does.
type StepKind string

const (
	StepMkdir  StepKind = "mkdir"
	StepCopy   StepKind = "copy"
	StepWrite  StepKind = "write"
	StepRemove StepKind = "remove"
	StepRun    StepKind = "run"
	// StepWait runs Argv every WaitEvery until its output contains Until
	// (whatever the exit status), for at most Timeout. Used to wait for an
	// asynchronous change (launchd finishing a bootout) before the next
	// step.
	StepWait StepKind = "wait"
)

// Step is one action of a Plan.
type Step struct {
	Kind    StepKind
	Path    string      // mkdir/copy(dst)/write/remove
	Src     string      // copy
	Mode    fs.FileMode // mkdir/copy/write
	Content []byte      // write
	Argv    []string    // run
	// IgnoreError marks steps whose failure is expected in some states
	// (e.g. unloading a service that is not loaded).
	IgnoreError bool

	// Retry (run) is how many more times a failing command is run when
	// its error or output contains one of RetryOn, waiting RetryBackoff,
	// then twice as long each time.
	Retry        int
	RetryOn      []string
	RetryBackoff time.Duration

	// Until and Timeout (wait): see StepWait. OrUntil are more strings any
	// of which also ends the wait (e.g. "service does not exist").
	Until   string
	OrUntil []string
	Timeout time.Duration
}

// WaitEvery is how often a StepWait runs its command.
const WaitEvery = 200 * time.Millisecond

// Sleep waits d or until ctx ends. Replaceable in tests.
var Sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// String renders the step as the shell command it is equivalent to.
func (s Step) String() string {
	var out string
	switch s.Kind {
	case StepMkdir:
		out = fmt.Sprintf("install -d -m %04o %s", s.Mode.Perm(), shellQuote(s.Path))
	case StepCopy:
		out = fmt.Sprintf("install -m %04o %s %s", s.Mode.Perm(), shellQuote(s.Src), shellQuote(s.Path))
	case StepWrite:
		out = fmt.Sprintf("write %s (mode %04o, %d bytes)", shellQuote(s.Path), s.Mode.Perm(), len(s.Content))
	case StepRemove:
		out = "rm -f " + shellQuote(s.Path)
	case StepRun:
		q := make([]string, len(s.Argv))
		for i, a := range s.Argv {
			q[i] = shellQuote(a)
		}
		out = strings.Join(q, " ")
		if s.Retry > 0 {
			out += fmt.Sprintf("  # retried up to %d times while busy", s.Retry)
		}
	case StepWait:
		q := make([]string, len(s.Argv))
		for i, a := range s.Argv {
			q[i] = shellQuote(a)
		}
		until := fmt.Sprintf("%q", s.Until)
		for _, o := range s.OrUntil {
			until += fmt.Sprintf(" or %q", o)
		}
		out = fmt.Sprintf("wait until `%s` says %s (up to %s)", strings.Join(q, " "), until, s.Timeout)
	default:
		out = string(s.Kind)
	}
	if s.IgnoreError {
		out += "  # errors ignored"
	}
	return out
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Manager is implemented per platform.
type Manager interface {
	// Render returns the service definition (plist, unit file...).
	Render(Spec) ([]byte, error)
	// DefinitionPath is where the definition of label is installed.
	DefinitionPath(label string) string
	// Plan lists the steps of an operation. Only OpInstall needs a full
	// Spec; the others use Spec.Label.
	Plan(op Op, spec Spec) ([]Step, error)
	// Status reads the runtime state (read-only).
	Status(ctx context.Context, label string) (Status, error)
}

// Executor runs commands. The real one execs; tests use a fake.
type Executor interface {
	Run(ctx context.Context, argv []string) ([]byte, error)
}

// Files does the filesystem side of a plan.
type Files interface {
	MkdirAll(path string, mode fs.FileMode) error
	WriteFile(path string, data []byte, mode fs.FileMode) error
	Copy(src, dst string, mode fs.FileMode) error
	Remove(path string) error
}

// Apply runs a plan in order and stops at the first failing step that is
// not IgnoreError.
func Apply(ctx context.Context, steps []Step, ex Executor, files Files) error {
	for _, s := range steps {
		var err error
		switch s.Kind {
		case StepMkdir:
			err = files.MkdirAll(s.Path, s.Mode)
		case StepCopy:
			err = files.Copy(s.Src, s.Path, s.Mode)
		case StepWrite:
			err = files.WriteFile(s.Path, s.Content, s.Mode)
		case StepRemove:
			err = files.Remove(s.Path)
		case StepRun:
			err = runRetrying(ctx, s, ex)
		case StepWait:
			err = waitUntil(ctx, s, ex)
		default:
			err = fmt.Errorf("unknown step kind %q", s.Kind)
		}
		if err != nil && !s.IgnoreError {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

// runRetrying runs a StepRun, again on the errors it names.
func runRetrying(ctx context.Context, s Step, ex Executor) error {
	backoff := s.RetryBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	for attempt := 0; ; attempt++ {
		out, err := ex.Run(ctx, s.Argv)
		if err == nil {
			return nil
		}
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		if attempt >= s.Retry || !matchesAny(err.Error(), s.RetryOn) {
			if attempt > 0 {
				err = fmt.Errorf("%w (after %d attempts)", err, attempt+1)
			}
			return err
		}
		if serr := Sleep(ctx, backoff); serr != nil {
			return err
		}
		backoff *= 2
	}
}

func matchesAny(s string, subs []string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// waitUntil runs a StepWait.
func waitUntil(ctx context.Context, s Step, ex Executor) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var last string
	for waited := time.Duration(0); ; waited += WaitEvery {
		out, _ := ex.Run(ctx, s.Argv)
		if strings.Contains(string(out), s.Until) || matchesAny(string(out), s.OrUntil) {
			return nil
		}
		last = strings.TrimSpace(string(out))
		if i := strings.IndexByte(last, '\n'); i >= 0 {
			last = last[:i]
		}
		if waited >= timeout {
			return fmt.Errorf("still not %q after %s (last: %s)", s.Until, timeout, last)
		}
		if err := Sleep(ctx, WaitEvery); err != nil {
			return err
		}
	}
}

// FormatPlan renders a plan for a dry run: each step on a line, file
// contents of write steps included.
func FormatPlan(w io.Writer, steps []Step) {
	for i, s := range steps {
		fmt.Fprintf(w, "%d. %s\n", i+1, s)
		if s.Kind == StepWrite {
			fmt.Fprintf(w, "----- %s -----\n%s", s.Path, s.Content)
			if len(s.Content) > 0 && s.Content[len(s.Content)-1] != '\n' {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "----- end -----\n")
		}
	}
}

// ExecExecutor runs commands with os/exec.
type ExecExecutor struct{ Timeout time.Duration }

// Run implements Executor.
func (e ExecExecutor) Run(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	t := e.Timeout
	if t == 0 {
		t = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	return exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
}

// OSFiles implements Files on the real filesystem.
type OSFiles struct{}

func (OSFiles) MkdirAll(p string, m fs.FileMode) error { return os.MkdirAll(p, m) }
func (OSFiles) Remove(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (OSFiles) WriteFile(p string, b []byte, m fs.FileMode) error {
	tmp := p + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, m); err != nil {
		return err
	}
	if err := os.Chmod(tmp, m); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}
func (f OSFiles) Copy(src, dst string, m fs.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return f.WriteFile(dst, b, m)
}
