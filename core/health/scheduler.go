package health

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// Clock is the time source of a Scheduler; tests use a fake one.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is the subset of *time.Timer the Scheduler needs.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration)
}

// RealClock is the wall clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) NewTimer(d time.Duration) Timer {
	return realTimer{time.NewTimer(d)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time   { return r.t.C }
func (r realTimer) Stop() bool            { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) { r.t.Reset(d) }

// Change is a status transition of one check between two runs. From is ""
// for the first run.
type Change struct {
	ID    string      `json:"id"`
	From  Status      `json:"from"`
	To    Status      `json:"to"`
	Check CheckReport `json:"check"`
}

// Event is emitted after every scheduled or requested run.
type Event struct {
	Reason  string    `json:"reason"` // "start", "interval", "manual", or trigger reasons joined by ", "
	Report  *Report   `json:"report"`
	Changes []Change  `json:"changes,omitempty"`
	At      time.Time `json:"at"`
}

// Scheduler runs the checks periodically, on network-change triggers and on
// demand, keeps the last report and emits status transitions. It is meant to
// be embedded in the helper:
//
//	s := &health.Scheduler{Runner: r, NewEnv: newEnv, Triggers: []<-chan string{nw.C}}
//	go s.Run(ctx)
//	events, cancel := s.Subscribe(8)
type Scheduler struct {
	Runner *Runner
	// NewEnv returns the Env for one run. A fresh Env per run matters when
	// the Env caches things (the darwin Env reads the config file once).
	// If nil, Env is used for every run.
	NewEnv func() Env
	Env    Env
	// Interval between periodic runs; default 5 minutes. A triggered or
	// manual run restarts the interval.
	Interval time.Duration
	// Debounce collects triggers arriving within this window into one run;
	// default 2 seconds. The window starts at the first trigger and is not
	// extended by later ones, so a flapping network still gets checked.
	Debounce time.Duration
	// Triggers are external change sources (e.g. a network watcher); each
	// received value is a reason string.
	Triggers []<-chan string
	// SkipInitial disables the run at start.
	SkipInitial bool
	// OnEvent, if set, is called synchronously after every run.
	OnEvent func(Event)
	Clock   Clock // default RealClock

	once    sync.Once
	trig    chan string
	runMu   sync.Mutex // serializes runs
	mu      sync.Mutex // guards the fields below
	last    *Report
	subs    map[int]chan Event
	nextSub int
	running bool
}

// ErrSchedulerRunning is returned by Run when it is already running.
var ErrSchedulerRunning = errors.New("health: scheduler already running")

func (s *Scheduler) init() {
	s.once.Do(func() {
		s.trig = make(chan string, 64)
		s.subs = map[int]chan Event{}
		if s.Clock == nil {
			s.Clock = RealClock{}
		}
		if s.Interval <= 0 {
			s.Interval = 5 * time.Minute
		}
		if s.Debounce <= 0 {
			s.Debounce = 2 * time.Second
		}
	})
}

// Last returns the most recent report, or nil before the first run.
func (s *Scheduler) Last() *Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Subscribe returns a channel receiving every Event. Sends never block: a
// subscriber that falls more than buf events behind misses events (Last
// still has the newest report). cancel closes the channel.
func (s *Scheduler) Subscribe(buf int) (<-chan Event, func()) {
	s.init()
	if buf < 1 {
		buf = 1
	}
	ch := make(chan Event, buf)
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	s.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			close(ch)
		})
	}
}

// Trigger asks for a run soon (after the debounce window). It never blocks.
func (s *Scheduler) Trigger(reason string) {
	s.init()
	select {
	case s.trig <- reason:
	default: // a run is already pending
	}
}

// RunNow runs the checks immediately in the caller's goroutine and returns
// the report. It waits for a run in progress to finish first.
func (s *Scheduler) RunNow(ctx context.Context, reason string) *Report {
	s.init()
	if reason == "" {
		reason = "manual"
	}
	return s.runOnce(ctx, reason)
}

// Run runs the schedule until ctx is done.
func (s *Scheduler) Run(ctx context.Context) error {
	s.init()
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrSchedulerRunning
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	for _, c := range s.Triggers {
		go func(c <-chan string) {
			for {
				select {
				case <-ctx.Done():
					return
				case r, ok := <-c:
					if !ok {
						return
					}
					s.Trigger(r)
				}
			}
		}(c)
	}

	first := s.Interval
	if !s.SkipInitial {
		first = 0
	}
	tick := s.Clock.NewTimer(first)
	defer tick.Stop()
	initial := !s.SkipInitial
	var deb Timer
	var debC <-chan time.Time
	var reasons []string
	afterRun := func() {
		tick.Stop()
		drain(tick.C())
		tick.Reset(s.Interval)
	}

	for {
		select {
		case <-ctx.Done():
			if deb != nil {
				deb.Stop()
			}
			return ctx.Err()
		case <-tick.C():
			reason := "interval"
			if initial {
				reason, initial = "start", false
			}
			s.runOnce(ctx, reason)
			tick.Reset(s.Interval)
		case r := <-s.trig:
			reasons = appendUniq(reasons, r)
			if debC == nil {
				if deb == nil {
					deb = s.Clock.NewTimer(s.Debounce)
				} else {
					deb.Reset(s.Debounce)
				}
				debC = deb.C()
			}
		case <-debC:
			debC = nil
			reason := strings.Join(reasons, ", ")
			reasons = nil
			initial = false
			s.runOnce(ctx, reason)
			afterRun()
		}
	}
}

func drain(c <-chan time.Time) {
	select {
	case <-c:
	default:
	}
}

func appendUniq(list []string, s string) []string {
	if s == "" {
		s = "trigger"
	}
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func (s *Scheduler) runOnce(ctx context.Context, reason string) *Report {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	env := s.Env
	if s.NewEnv != nil {
		env = s.NewEnv()
	}
	rep := s.Runner.Run(ctx, env)
	if ctx.Err() != nil {
		// Cancelled mid-run: the results are timeouts, not facts.
		return rep
	}

	s.mu.Lock()
	changes := Diff(s.last, rep)
	s.last = rep
	ev := Event{Reason: reason, Report: rep, Changes: changes, At: s.Clock.Now()}
	subs := make([]chan Event, 0, len(s.subs))
	ids := make([]int, 0, len(s.subs))
	for id := range s.subs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		subs = append(subs, s.subs[id])
	}
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
	s.mu.Unlock()

	if s.OnEvent != nil {
		s.OnEvent(ev)
	}
	return rep
}

// Diff returns the checks whose status differs between two reports, in the
// order of cur. Checks missing from prev have From "". Checks missing from
// cur are reported with To "".
func Diff(prev, cur *Report) []Change {
	old := map[string]Status{}
	if prev != nil {
		for _, c := range prev.Checks {
			old[c.ID] = c.Status
		}
	}
	var out []Change
	seen := map[string]bool{}
	if cur != nil {
		for _, c := range cur.Checks {
			seen[c.ID] = true
			if from, ok := old[c.ID]; !ok || from != c.Status {
				out = append(out, Change{ID: c.ID, From: from, To: c.Status, Check: c})
			}
		}
	}
	if prev != nil {
		for _, c := range prev.Checks {
			if !seen[c.ID] {
				out = append(out, Change{ID: c.ID, From: c.Status, Check: c})
			}
		}
	}
	return out
}
