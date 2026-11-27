package health

import (
	"context"
	"fmt"
	"github.com/l3ony2k/twinnel/core/foreign"
	"runtime/debug"
	"strings"
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
	// Set by Accept: the status before it was accepted, and the check
	// whose accepted cause broke this one.
	Was      Status `json:"was,omitempty"`
	CausedBy string `json:"causedBy,omitempty"`
	// Controller is the mihomo controller the check's evidence came from
	// (when it read the running mihomo), worded in the report's language:
	// "127.0.0.1:9090（Twinnel）", "127.0.0.1:9090（Clash Party）".
	Controller string `json:"controller,omitempty"`
	// At is when the result was measured, set when that was before the
	// report's run: an on-demand egress test carried into later reports.
	At time.Time `json:"at,omitzero"`
}

// Report is the result of a run.
type Report struct {
	Platform string        `json:"platform"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"durationNs"`
	Overall  Status        `json:"overall"`
	Checks   []CheckReport `json:"checks"`
	// Lang is the language the texts are in (WithLang); "" in reports
	// from helpers older than it, which wrote DefaultLang.
	Lang string `json:"lang,omitempty"`
	// Controller is who listened on the controller address during the
	// run (nil when the Env does not know).
	Controller *foreign.Owner `json:"controller,omitempty"`
	// Source is where the checks ran (Source*), set by the client that
	// shows the report; "" when it does not say.
	Source string `json:"source,omitempty"`
}

// Where a report's checks ran (Report.Source). The helper's run is the
// one the app shows; a local run lacks what only the helper knows (who
// holds the controller when the helper is down, the competitors'
// autostarts, the DNS it guards), so its verdicts can differ.
const (
	SourceHelper        = "helper"          // the helper ran them
	SourceLocal         = "local"           // this process, as asked (--local)
	SourceLocalNoHelper = "local-no-helper" // this process: the helper did not answer
)

// Only returns a copy of r with just the checks named in ids (all of
// them when ids is empty) and Overall worked out again.
func (r *Report) Only(ids []string) *Report {
	if r == nil || len(ids) == 0 {
		return r
	}
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	out := *r
	out.Checks, out.Overall = nil, StatusOK
	for _, c := range r.Checks {
		if keep[c.ID] {
			out.Checks = append(out.Checks, c)
			out.Overall = Worse(out.Overall, c.Status)
		}
	}
	return &out
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
	L := tx(ctx)
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
	rep := &Report{Platform: platform, Started: start, Overall: StatusOK, Checks: make([]CheckReport, len(checks)), Lang: LangOf(ctx)}
	owner, ownerKnown := ownerOf(ctx, env)
	if ownerKnown {
		rep.Controller = &owner
	}
	isForeign := ownerKnown && owner.IsForeign()

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, c := range checks {
		rep.Checks[i] = CheckReport{ID: c.ID(), Title: checkTitle(L, c)}
		if !supports(c, platform) {
			rep.Checks[i].Result = unsupported(L, platform)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := now()
			tenv := &trackEnv{Env: env, foreign: isForeign}
			res := runOne(ctx, c, tenv, timeout)
			if tenv.used() {
				rep.Checks[i].Controller = controllerLabel(L, owner, ownerKnown)
				if isForeign && c.ID() != (ControllerOwnerCheck{}).ID() {
					res = notJudged(L, owner, res)
				}
			}
			res.Evidence = dropBlank(res.Evidence)
			rep.Checks[i].Result = res
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

// checkTitle returns c's title in L's language: built-in checks word it
// per language, others have only Title.
func checkTitle(L *texts, c Check) string {
	if lc, ok := c.(interface{ title(*texts) string }); ok {
		return lc.title(L)
	}
	return c.Title()
}

// runOne runs a check, converting a panic or a timeout into a fail result.
func runOne(ctx context.Context, c Check, env Env, timeout time.Duration) Result {
	L := tx(ctx)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- Result{Status: StatusFail, Summary: L.msgCheckPanicked,
					Evidence: []string{fmt.Sprint(p), string(debug.Stack())}, Reasons: []string{ReasonCheckPanicked}}
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
		return Result{Status: StatusWarn, Summary: fmt.Sprintf(L.msgCheckTimeout, timeout), Reasons: []string{ReasonCheckTimeout}}
	}
}

// dropBlank leaves out evidence lines with nothing in them: every report
// (the app, the CLI, the copied text) would print them as a bare "· ".
func dropBlank(ev []string) []string {
	var out []string
	for _, e := range ev {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}
