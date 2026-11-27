// Package svchost runs a program that is not a Windows service (mihomo.exe)
// as one: `twinnel-svchost.exe svchost --name <service> [--log FILE]
// [--keep-alive] [--throttle 10s] -- <program> <args>`. The host answers
// the Service Control Manager, keeps the child alive and ends it on Stop.
//
// The restart logic (Supervisor) and the argument parsing have no build tag
// and are tested on every OS; the SCM glue is in host_windows.go.
package svchost

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"
)

// Process is a started child.
type Process interface {
	Wait() error
	Kill() error
}

// Supervisor starts a child and, with KeepAlive, starts it again when it
// exits, at most once per Throttle. When ctx ends it kills the child and
// waits for it.
type Supervisor struct {
	Start     func(ctx context.Context) (Process, error)
	KeepAlive bool
	Throttle  time.Duration
	Log       func(msg string, args ...any)
	Sleep     func(ctx context.Context, d time.Duration) error
	Now       func() time.Time
}

func (s *Supervisor) log(msg string, args ...any) {
	if s.Log != nil {
		s.Log(msg, args...)
	}
}

func (s *Supervisor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Supervisor) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ErrExited is returned when the child exits without KeepAlive.
var ErrExited = errors.New("program exited")

// Run blocks until ctx ends (nil) or, without KeepAlive, the child exits
// (ErrExited wrapping its error) or cannot be started.
func (s *Supervisor) Run(ctx context.Context) error {
	for runs := 0; ; runs++ {
		if ctx.Err() != nil {
			return nil
		}
		started := s.now()
		p, err := s.Start(ctx)
		if err != nil {
			s.log("start failed", "err", err)
			if !s.KeepAlive {
				return fmt.Errorf("start: %w", err)
			}
		} else {
			s.log("started", "run", runs+1)
			done := make(chan error, 1)
			go func() { done <- p.Wait() }()
			select {
			case <-ctx.Done():
				s.log("stopping")
				p.Kill()
				<-done
				return nil
			case err = <-done:
			}
			s.log("exited", "err", err)
			if !s.KeepAlive {
				return fmt.Errorf("%w: %v", ErrExited, err)
			}
		}
		if wait := s.Throttle - s.now().Sub(started); wait > 0 {
			if s.sleep(ctx, wait) != nil {
				return nil
			}
		}
	}
}

// Options are the host's command line.
type Options struct {
	Name      string
	Log       string
	KeepAlive bool
	Throttle  time.Duration
	Argv      []string // program and its arguments
}

// Parse reads the arguments after "svchost".
func Parse(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet("svchost", flag.ContinueOnError)
	fs.StringVar(&o.Name, "name", "", "service `NAME`")
	fs.StringVar(&o.Log, "log", "", "append the program's output to `FILE`")
	fs.BoolVar(&o.KeepAlive, "keep-alive", false, "start the program again when it exits")
	fs.DurationVar(&o.Throttle, "throttle", 10*time.Second, "minimum time between starts")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.Argv = fs.Args()
	switch {
	case o.Name == "":
		return o, errors.New("--name is required")
	case len(o.Argv) == 0:
		return o, errors.New("no program: svchost --name NAME [flags] -- PROGRAM [ARGS]")
	case o.Throttle < time.Second:
		o.Throttle = time.Second
	}
	return o, nil
}
