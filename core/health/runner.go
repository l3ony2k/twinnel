package health

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// Runner runs checks with a per-check timeout and bounded concurrency.
type Runner struct {
	Registry    *Registry
	Timeout     time.Duration // per check; default 10s
	Concurrency int           // default 4
	// Only, if non-empty, limits the run to these check IDs.
	Only []string
	// Now is used for timestamps; default time.Now.
	Now func() time.Time
}

// CheckReport is the result of one check in a report.
type CheckReport struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Duration time.Duration `json:"durationNs"`
	Result
}

// Report is the result of a run.
type Report struct {
	Platform string        `json:"platform"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"durationNs"`
	Overall  Status        `json:"overall"`
	Checks   []CheckReport `json:"checks"`
}

// Counts returns how many checks ended in each status.
func (r *Report) Counts() map[Status]int {
	m := map[Status]int{}
	for _, c := range r.Checks {
		m[c.Status]++
	}
	return m
}

// Run runs all applicable checks against env and returns a report in
// registration order. env is wrapped with Memo.
func (r *Runner) Run(ctx context.Context, env Env) *Report {
	now := r.Now
	if now == nil {
		now = time.Now
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	conc := r.Concurrency
	if conc <= 0 {
		conc = 4
	}
	only := map[string]bool{}
	for _, id := range r.Only {
		only[id] = true
	}

	start := now()
	platform := env.Platform()
	env = Memo(ctx, env)

	var checks []Check
	for _, c := range r.Registry.Checks() {
		if len(only) > 0 && !only[c.ID()] {
			continue
		}
		checks = append(checks, c)
	}
	rep := &Report{Platform: platform, Started: start, Overall: StatusOK, Checks: make([]CheckReport, len(checks))}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, c := range checks {
		rep.Checks[i] = CheckReport{ID: c.ID(), Title: c.Title()}
		if !supports(c, platform) {
			rep.Checks[i].Result = skip(msgNotOnPlatform, platform)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := now()
			rep.Checks[i].Result = runOne(ctx, c, env, timeout)
			rep.Checks[i].Duration = now().Sub(t0)
		}()
	}
	wg.Wait()
	for _, c := range rep.Checks {
		rep.Overall = Worse(rep.Overall, c.Status)
	}
	rep.Duration = now().Sub(start)
	return rep
}

// runOne runs a check, converting a panic or a timeout into a fail result.
func runOne(ctx context.Context, c Check, env Env, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- Result{Status: StatusFail, Summary: msgCheckPanicked,
					Evidence: []string{fmt.Sprint(p), string(debug.Stack())}}
			}
		}()
		done <- c.Run(ctx, env)
	}()
	select {
	case r := <-done:
		if r.Status == "" {
			r.Status = StatusSkip
		}
		return r
	case <-ctx.Done():
		return Result{Status: StatusWarn, Summary: fmt.Sprintf(msgCheckTimeout, timeout)}
	}
}
