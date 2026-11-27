package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/l3ony2k/twinnel/core/service"
)

// OpResult is the --json shape of the commands that run steps rather than
// return a helper result (helper install, restart, uninstall, upgrade):
// what ran, whether it worked, and the lines it would have printed.
type OpResult struct {
	Op     string   `json:"op"`
	OK     bool     `json:"ok"`
	DryRun bool     `json:"dry_run,omitempty"`
	Output []string `json:"output"`
	// Steps are the plan's steps as data (what the app shows), in the
	// running system's dialect.
	Steps []service.StepView `json:"steps,omitempty"`
	Error string             `json:"error,omitempty"`
}

// jsonSteps collects the steps of the plans shown while jsonRun runs
// (nil otherwise).
var jsonSteps *[]service.StepView

// recordPlan adds a plan's steps to the --json result.
func recordPlan(steps []service.Step) {
	if jsonSteps != nil {
		*jsonSteps = append(*jsonSteps, service.Current().Views(steps)...)
	}
}

// jsonRun runs fn; with on, its text output is collected and printed as
// one OpResult instead. The error is still returned (exit status 1).
func jsonRun(op string, on, dryRun bool, fn func() error) error {
	if !on {
		return fn()
	}
	real := stdout
	var buf bytes.Buffer
	var steps []service.StepView
	stdout, jsonSteps = &buf, &steps
	err := fn()
	stdout, jsonSteps = real, nil
	r := OpResult{Op: op, OK: err == nil, DryRun: dryRun, Output: []string{}, Steps: steps}
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if l != "" {
			r.Output = append(r.Output, l)
		}
	}
	if err != nil {
		r.Error = err.Error()
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Fprintln(real, string(b))
	return err
}

var _ io.Writer = (*bytes.Buffer)(nil)
