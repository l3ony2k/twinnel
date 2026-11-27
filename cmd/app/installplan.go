package main

import (
	"regexp"
	"strings"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// The install plan of the first-run view: `twinnel helper install --dry-run`
// prints numbered steps (core/service.FormatPlan: "N. <command>", a written
// file's content between "----- <path> -----" and "----- end -----").
// parsePlan turns that into steps a person reads: what the step does in
// words, the command under it, and a file's content behind a disclosure.

type planStep struct {
	what    string // in words; the command when the step is not one Twinnel knows
	cmd     string // the command as printed
	content string // a written file's content
}

var (
	planLine   = regexp.MustCompile(`^(\d+)\. (.*)$`)
	planMkdir  = regexp.MustCompile(`^install -d -m \d+ (.+)$`)
	planCopy   = regexp.MustCompile(`^install -m \d+ ('[^']*'|\S+) ('[^']*'|\S+)$`)
	planWrite  = regexp.MustCompile(`^write ('[^']*'|\S+) \(mode \d+, \d+ bytes\)$`)
	planRemove = regexp.MustCompile(`^rm -f (.+)$`)
)

// parsePlan reads the dry-run output; ok is false when it has no steps
// (the view then shows the raw text).
func parsePlan(out string) (steps []planStep, ok bool) {
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "----- ") && strings.HasSuffix(l, " -----") && l != "----- end -----" && len(steps) > 0 {
			var body []string
			for i++; i < len(lines) && lines[i] != "----- end -----"; i++ {
				body = append(body, lines[i])
			}
			steps[len(steps)-1].content = strings.Join(body, "\n")
			continue
		}
		m := planLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cmd := m[2]
		steps = append(steps, planStep{what: describeStep(cmd), cmd: cmd})
	}
	return steps, len(steps) > 0
}

// describeStep says what a plan command does, in the UI language.
func describeStep(cmd string) string {
	T := i18n.T
	bare, _, _ := strings.Cut(cmd, "  # ")
	switch {
	case planMkdir.MatchString(bare):
		return i18n.F(T.PlanMkdir, unquote(planMkdir.FindStringSubmatch(bare)[1]))
	case planCopy.MatchString(bare):
		return i18n.F(T.PlanCopy, unquote(planCopy.FindStringSubmatch(bare)[2]))
	case planWrite.MatchString(bare):
		return i18n.F(T.PlanWrite, unquote(planWrite.FindStringSubmatch(bare)[1]))
	case planRemove.MatchString(bare):
		return i18n.F(T.PlanRemove, unquote(planRemove.FindStringSubmatch(bare)[1]))
	}
	if strings.HasPrefix(bare, "wait until `") && (strings.Contains(bare, "launchctl print") || strings.Contains(strings.ToLower(bare), "sc.exe query")) {
		return T.PlanWaitStopped
	}
	f := strings.Fields(bare)
	// Windows: the service steps, printed as the sc.exe commands they
	// stand for (platform/windows/scm runs them through the SCM API).
	if len(f) >= 2 && strings.HasSuffix(strings.ToLower(unquote(f[0])), "sc.exe") {
		switch strings.ToLower(f[1]) {
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
	}
	if len(f) >= 3 && strings.HasSuffix(f[0], "launchctl") {
		switch f[1] {
		case "kickstart":
			return T.PlanKickstart
		case "bootout":
			return T.PlanBootout
		case "bootstrap":
			return T.PlanBootstrap
		case "enable":
			return T.PlanEnable
		}
	}
	if len(f) >= 2 && strings.HasSuffix(f[0], "systemctl") {
		switch f[1] {
		case "daemon-reload":
			return T.PlanDaemonReload
		case "enable":
			return T.PlanBootstrap
		}
	}
	return bare
}

// unquote undoes core/service's shell quoting of one word.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}
