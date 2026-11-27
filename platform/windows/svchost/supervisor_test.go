package svchost

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

type fakeProc struct {
	exit   chan error
	killed bool
	mu     *sync.Mutex
}

func (p *fakeProc) Wait() error { return <-p.exit }
func (p *fakeProc) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.exit <- errors.New("killed")
	return nil
}

type clock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return ctx.Err()
}

// A child that keeps crashing at once is started again no more than once
// per throttle; Stop kills the running one.
func TestSupervisorKeepAliveThrottleAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &clock{t: time.Unix(1000, 0)}
	var mu sync.Mutex
	var procs []*fakeProc
	starts := 0
	s := &Supervisor{
		KeepAlive: true, Throttle: 10 * time.Second, Now: c.Now, Sleep: c.Sleep,
		Start: func(context.Context) (Process, error) {
			starts++
			p := &fakeProc{exit: make(chan error, 1), mu: &mu}
			procs = append(procs, p)
			if starts <= 2 {
				p.exit <- errors.New("exit status 1") // crashes at once
			} else {
				cancel() // the third keeps running until Stop
			}
			return p, nil
		},
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if starts != 3 {
		t.Fatalf("starts = %d", starts)
	}
	if !slices.Equal(c.sleeps, []time.Duration{10 * time.Second, 10 * time.Second}) {
		t.Errorf("sleeps %v", c.sleeps)
	}
	mu.Lock()
	defer mu.Unlock()
	if !procs[2].killed || procs[0].killed {
		t.Error("only the running child is killed on stop")
	}
}

func TestSupervisorWithoutKeepAlive(t *testing.T) {
	var mu sync.Mutex
	s := &Supervisor{Start: func(context.Context) (Process, error) {
		p := &fakeProc{exit: make(chan error, 1), mu: &mu}
		p.exit <- nil
		return p, nil
	}}
	if err := s.Run(context.Background()); !errors.Is(err, ErrExited) {
		t.Fatalf("err = %v", err)
	}
	s.Start = func(context.Context) (Process, error) { return nil, errors.New("no such file") }
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("start failure not reported")
	}
}

func TestParse(t *testing.T) {
	o, err := Parse([]string{"--name", "TwinnelMihomo", "--log", `C:\l.log`, "--keep-alive", "--throttle", "10s", "--", `C:\m.exe`, "-d", `C:\x`})
	if err != nil || o.Name != "TwinnelMihomo" || !o.KeepAlive || o.Throttle != 10*time.Second || !slices.Equal(o.Argv, []string{`C:\m.exe`, "-d", `C:\x`}) {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := Parse([]string{"--name", "X"}); err == nil {
		t.Error("no program accepted")
	}
	if o, _ := Parse([]string{"--name", "X", "--throttle", "0s", "--", "p"}); o.Throttle != time.Second {
		t.Errorf("throttle %v", o.Throttle)
	}
}
