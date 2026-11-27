//go:build windows

package scm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/l3ony2k/twinnel/core/service"
)

// Executor carries out sc.exe steps in process with the SCM API and passes
// every other command to Fallback (default service.ExecExecutor). Its
// output for query is sc.exe's, in English, so wait steps can match it.
type Executor struct {
	Fallback service.Executor
}

var _ service.Executor = Executor{}

// Run implements service.Executor.
func (e Executor) Run(ctx context.Context, argv []string) ([]byte, error) {
	if !IsSC(argv) {
		fb := e.Fallback
		if fb == nil {
			fb = service.ExecExecutor{}
		}
		return fb.Run(ctx, argv)
	}
	c, err := ParseCommand(argv)
	if err != nil {
		return nil, err
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to the Service Control Manager: %w", err)
	}
	defer m.Disconnect()
	if c.Verb == "create" {
		return nil, create(m, c)
	}
	s, err := m.OpenService(c.Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			if c.Verb == "delete" {
				return nil, nil // already gone
			}
			return []byte(NoServiceText()), err
		}
		return nil, err
	}
	defer s.Close()
	switch c.Verb {
	case "config":
		return nil, config(s, c)
	case "description":
		cfg, err := s.Config()
		if err != nil {
			return nil, err
		}
		cfg.Description = strings.Join(c.Args, " ")
		return nil, s.UpdateConfig(cfg)
	case "failure":
		acts, err := ParseActions(c.Opts["actions"])
		if err != nil {
			return nil, err
		}
		reset := uint32(86400)
		if v := c.Opts["reset"]; v != "" {
			var n uint32
			if _, err := fmt.Sscan(v, &n); err != nil {
				return nil, fmt.Errorf("reset= %q: %w", v, err)
			}
			reset = n
		}
		ra := make([]mgr.RecoveryAction, len(acts))
		for i, a := range acts {
			ra[i] = mgr.RecoveryAction{Type: mgr.NoAction, Delay: a.Delay}
			if a.Restart {
				ra[i].Type = mgr.ServiceRestart
			}
		}
		return nil, s.SetRecoveryActions(ra, reset)
	case "failureflag":
		return nil, s.SetRecoveryActionsOnNonCrashFailures(len(c.Args) > 0 && c.Args[0] == "1")
	case "start":
		err := s.Start()
		if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return nil, nil
		}
		return nil, err
	case "stop":
		st, err := s.Control(svc.Stop)
		if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return []byte(QueryText(c.Name, uint32(svc.Stopped), 0)), nil
		}
		return []byte(QueryText(c.Name, uint32(st.State), st.ProcessId)), err
	case "query":
		st, err := s.Query()
		if err != nil {
			return nil, err
		}
		return []byte(QueryText(c.Name, uint32(st.State), st.ProcessId)), nil
	case "delete":
		err := s.Delete()
		if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			return nil, fmt.Errorf("%s is already marked for deletion; close the Services window (or anything else holding it open) and it goes: %w", c.Name, err)
		}
		return nil, err
	}
	return nil, fmt.Errorf("unsupported service command %q", c.Verb)
}

func create(m *mgr.Mgr, c Command) error {
	bp := c.Opts["binpath"]
	if bp == "" {
		return errors.New("create: binPath= is required")
	}
	start, err := StartTypeCode(c.Opts["start"])
	if err != nil {
		return err
	}
	name, _ := windows.UTF16PtrFromString(c.Name)
	disp, _ := windows.UTF16PtrFromString(c.Opts["displayname"])
	bin, err := windows.UTF16PtrFromString(bp)
	if err != nil {
		return err
	}
	var obj *uint16
	if o := c.Opts["obj"]; o != "" && !strings.EqualFold(o, "LocalSystem") {
		if obj, err = windows.UTF16PtrFromString(o); err != nil {
			return err
		}
	}
	h, err := windows.CreateService(m.Handle, name, disp, windows.SERVICE_ALL_ACCESS,
		windows.SERVICE_WIN32_OWN_PROCESS, start, windows.SERVICE_ERROR_NORMAL, bin, nil, nil, nil, obj, nil)
	if err != nil {
		return err
	}
	return windows.CloseServiceHandle(h)
}

func config(s *mgr.Service, c Command) error {
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if v, ok := c.Opts["binpath"]; ok {
		cfg.BinaryPathName = v
	}
	if v, ok := c.Opts["start"]; ok {
		if cfg.StartType, err = StartTypeCode(v); err != nil {
			return err
		}
	}
	if v, ok := c.Opts["obj"]; ok {
		cfg.ServiceStartName = v
	}
	if v, ok := c.Opts["displayname"]; ok {
		cfg.DisplayName = v
	}
	cfg.Password = ""
	return s.UpdateConfig(cfg)
}

// queryService reads a service's state and command line (read-only).
func queryService(_ context.Context, name string) (service.Status, error) {
	st := service.Status{Label: name}
	m, err := connectReadOnly()
	if err != nil {
		return st, err
	}
	defer m.Disconnect()
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenService(m.Handle, n, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	s := &mgr.Service{Name: name, Handle: h}
	defer s.Close()
	st.Installed, st.Loaded = true, true
	if q, err := s.Query(); err == nil {
		st.Running = q.State == svc.Running
		st.PID = int(q.ProcessId)
		if q.State == svc.Stopped && q.Win32ExitCode != 0 {
			code := int(q.Win32ExitCode)
			if q.Win32ExitCode == uint32(windows.ERROR_SERVICE_SPECIFIC_ERROR) {
				code = int(q.ServiceSpecificExitCode)
			}
			st.LastExitCode = &code
		}
	}
	if cfg, err := s.Config(); err == nil {
		st.Program = cfg.BinaryPathName
		if args := SplitCommandLine(cfg.BinaryPathName); len(args) > 0 {
			st.Program = args[0]
		}
	}
	return st, nil
}

// connectReadOnly opens the SCM with only SC_MANAGER_CONNECT, which a
// normal user has (mgr.Connect asks for all access).
func connectReadOnly() (*mgr.Mgr, error) {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, err
	}
	return &mgr.Mgr{Handle: h}, nil
}
