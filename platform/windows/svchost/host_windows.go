//go:build windows

package svchost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// maxLog is the size above which the log is moved to <log>.1 at start.
const maxLog = 10 << 20

// Main is the service host's entry point: `svchost --name … -- program`.
// It must be started by the SCM; it returns the process exit code.
func Main(args []string) int {
	o, err := Parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "svchost:", err)
		return 2
	}
	if ok, err := svc.IsWindowsService(); err != nil || !ok {
		fmt.Fprintln(os.Stderr, "svchost: must be started by the Service Control Manager (the service "+o.Name+")")
		return 2
	}
	out := io.Discard
	if o.Log != "" {
		if fi, err := os.Stat(o.Log); err == nil && fi.Size() > maxLog {
			os.Rename(o.Log, o.Log+".1")
		}
		f, err := os.OpenFile(o.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			defer f.Close()
			out = f
		}
	}
	h := &handler{o: o, out: &lockedWriter{w: out}}
	h.log = slog.New(slog.NewTextHandler(h.out, nil)).With("service", o.Name)
	if err := svc.Run(o.Name, h); err != nil {
		h.log.Error("service run", "err", err)
		return 1
	}
	return h.exit
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

type handler struct {
	o    Options
	out  io.Writer
	log  *slog.Logger
	exit int
}

// Execute implements svc.Handler.
func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	job, err := killOnCloseJob()
	if err != nil {
		h.log.Error("job object", "err", err)
		return true, 1
	}
	defer windows.CloseHandle(job) // kills anything left in it

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := &Supervisor{
		KeepAlive: h.o.KeepAlive, Throttle: h.o.Throttle,
		Log:   func(msg string, args ...any) { h.log.Info(msg, args...) },
		Start: func(context.Context) (Process, error) { return h.start(job) },
	}
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			// Without --keep-alive the program ended by itself: report a
			// failure so the SCM's recovery actions apply.
			h.log.Info("program ended", "err", err)
			h.exit = 1
			return true, 1
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}

type proc struct{ cmd *exec.Cmd }

func (p proc) Wait() error { return p.cmd.Wait() }
func (p proc) Kill() error { return p.cmd.Process.Kill() }

func (h *handler) start(job windows.Handle) (Process, error) {
	cmd := exec.Command(h.o.Argv[0], h.o.Argv[1:]...)
	cmd.Stdout, cmd.Stderr = h.out, h.out
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, ph)
		windows.CloseHandle(ph)
	}
	if err != nil {
		// Without the job the child could outlive a crashed host: refuse.
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("job object: %w", err)
	}
	return proc{cmd}, nil
}

// killOnCloseJob is a job whose processes die when its last handle
// closes, i.e. with the host, however it ends.
func killOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
