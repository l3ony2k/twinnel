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
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/binswap"
	"github.com/l3ony2k/twinnel/core/consoletext"
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
	// StepRemoveAll deletes a directory tree (Windows purge; Unix plans
	// keep `rm -rf`). Needs a Files with RemoveAll.
	StepRemoveAll StepKind = "removeall"
	// StepRename moves Src to Path (replacing it). On Windows it moves a
	// running program's file aside, which the system allows while it
	// refuses to overwrite or delete it. Needs a Files with Rename.
	StepRename StepKind = "rename"
)

// Step is one action of a Plan.
type Step struct {
	Kind    StepKind
	Path    string      // mkdir/copy(dst)/write/remove/rename(dst)
	Src     string      // copy/rename
	Mode    fs.FileMode // mkdir/copy/write
	Content []byte      // write
	Argv    []string    // run
	// IgnoreError marks steps whose failure is expected in some states
	// (e.g. unloading a service that is not loaded).
	IgnoreError bool
	// Always runs the step even after an earlier step failed (a
	// "finally": starting again a service the plan stopped).
	Always bool

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

// Apply runs a plan in order. At the first failing step that is not
// IgnoreError it stops, except for the Always steps after it, which still
// run; the first failure is returned.
func Apply(ctx context.Context, steps []Step, ex Executor, files Files) error {
	var failed error
	for _, s := range steps {
		if failed != nil && !s.Always {
			continue
		}
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
		case StepRemoveAll:
			ra, ok := files.(interface{ RemoveAll(string) error })
			if !ok {
				err = fmt.Errorf("cannot remove a directory tree with %T", files)
			} else {
				err = ra.RemoveAll(s.Path)
			}
		case StepRename:
			rn, ok := files.(interface{ Rename(src, dst string) error })
			if !ok {
				err = fmt.Errorf("cannot rename with %T", files)
			} else {
				err = rn.Rename(s.Src, s.Path)
			}
		case StepRun:
			err = runRetrying(ctx, s, ex)
		case StepWait:
			err = waitUntil(ctx, s, ex)
		default:
			err = fmt.Errorf("unknown step kind %q", s.Kind)
		}
		if err != nil && !s.IgnoreError && failed == nil {
			failed = fmt.Errorf("%s: %w", s, err)
		}
	}
	return failed
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
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	// Windows' own tools (sc, schtasks, reg) print in the OEM code page;
	// what is kept of their output (errors, history) must be UTF-8.
	return consoletext.Decode(out), err
}

// OSFiles implements Files on the real filesystem.
type OSFiles struct{}

func (OSFiles) MkdirAll(p string, m fs.FileMode) error { return os.MkdirAll(p, m) }

// RemoveAll deletes a directory tree.
func (OSFiles) RemoveAll(p string) error { return os.RemoveAll(p) }

// Rename moves src to dst, replacing dst.
func (OSFiles) Rename(src, dst string) error { return os.Rename(src, dst) }
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

// Copy replaces dst with a copy of src even while dst runs (Windows: the
// service host while mihomo runs, a helper not yet unloaded): the running
// file is moved aside and deleted on a later copy (core/binswap).
func (OSFiles) Copy(src, dst string, m fs.FileMode) error {
	return binswap.Replace(src, dst, m)
}
