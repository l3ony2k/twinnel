package main

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/service"
)

// The install plan of the first-run view: `twinnel helper install --dry-run
// --json` gives the plan's steps as data (core/service.StepView: kind,
// paths, argv, and the command as the system's dialect prints it).
// parsePlan turns them into steps a person reads: what the step does in
// words, the command under it, and a file's content behind a disclosure.
// Nothing here reads the printed commands, so the plan reads the same way
// whatever system's words print it.

type planStep struct {
	what    string // in words; the command when the step is not one Twinnel knows
	cmd     string // the command as printed
	content string // a written file's content
}

// planResult is the part of the CLI's --json result (OpResult) the view
// reads.
type planResult struct {
	Steps []service.StepView `json:"steps"`
	Error string             `json:"error"`
}

// parsePlan reads the dry run's --json output; ok is false when it has no
// steps (the view then shows the raw text).
func parsePlan(out string) (steps []planStep, ok bool) {
	var r planResult
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &r) != nil {
		return nil, false
	}
	for _, v := range r.Steps {
		steps = append(steps, planStep{what: describeStep(v), cmd: v.Text, content: strings.TrimSuffix(v.Content, "\n")})
	}
	return steps, len(steps) > 0
}

// planError is the error a failed --json dry run reports ("" when out is
// not one).
func planError(out string) string {
	var r planResult
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &r) != nil {
		return ""
	}
	return r.Error
}

// describeStep says what a plan step does, in the UI language.
func describeStep(v service.StepView) string {
	T := i18n.T
	switch v.Kind {
	case service.StepMkdir:
		return i18n.F(T.PlanMkdir, v.Path)
	case service.StepCopy:
		return i18n.F(T.PlanCopy, v.Path)
	case service.StepWrite:
		return i18n.F(T.PlanWrite, v.Path)
	case service.StepRemove:
		return i18n.F(T.PlanRemove, v.Path)
	case service.StepWait:
		if len(v.Argv) > 0 {
			return T.PlanWaitStopped
		}
	case service.StepRun:
		if w := describeRun(v.Argv); w != "" {
			return w
		}
	}
	return v.Text
}

// describeRun says what a service command does ("" when it is not one
// Twinnel knows).
func describeRun(argv []string) string {
	T := i18n.T
	if len(argv) < 2 {
		return ""
	}
	tool := strings.ToLower(filepath.Base(strings.ReplaceAll(argv[0], `\`, "/")))
	sub := strings.ToLower(argv[1])
	switch tool {
	case "sc.exe":
		// Windows: the service steps, the sc.exe commands they stand
		// for (platform/windows/scm runs them through the SCM API).
		switch sub {
		case "stop":
			return T.PlanBootout
		case "create":
			return T.PlanSCCreate
		case "config":
			return T.PlanSCConfig
		case "failure", "failureflag":
			return T.PlanSCFailure
		case "description":
			return T.PlanSCDescription
		case "start":
			return T.PlanSCStart
		case "delete":
			return T.PlanSCDelete
		}
	case "launchctl":
		switch sub {
		case "kickstart":
			return T.PlanKickstart
		case "bootout":
			return T.PlanBootout
		case "bootstrap":
			return T.PlanBootstrap
		case "enable":
			return T.PlanEnable
		}
	case "systemctl":
		switch sub {
		case "daemon-reload":
			return T.PlanDaemonReload
		case "enable":
			return T.PlanEnable
		case "restart", "start":
			return T.PlanBootstrap
		case "disable":
			return T.PlanBootout
		}
	}
	return ""
}
