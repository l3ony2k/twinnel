package scm

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Command is one parsed sc.exe step.
type Command struct {
	Verb string // create, config, description, failure, failureflag, start, stop, query, delete
	Name string
	// Opts are the "key= value" pairs, keys lower-cased without "=".
	Opts map[string]string
	// Args are positional arguments after the name (description text,
	// failureflag value).
	Args []string
}

var optKeys = map[string]bool{"binpath": true, "start": true, "obj": true, "displayname": true, "reset": true, "actions": true}

// IsSC reports whether argv is a service step of a plan.
func IsSC(argv []string) bool {
	return len(argv) > 0 && strings.EqualFold(filepath.Base(strings.ReplaceAll(argv[0], `\`, "/")), SC)
}

// ParseCommand reads an sc.exe argv as the plans write it.
func ParseCommand(argv []string) (Command, error) {
	if !IsSC(argv) || len(argv) < 3 {
		return Command{}, fmt.Errorf("not a service command: %q", argv)
	}
	c := Command{Verb: strings.ToLower(argv[1]), Name: argv[2], Opts: map[string]string{}}
	if !ValidName(c.Name) {
		return Command{}, fmt.Errorf("invalid service name %q", c.Name)
	}
	rest := argv[3:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if k, ok := strings.CutSuffix(a, "="); ok && optKeys[strings.ToLower(k)] {
			if i+1 >= len(rest) {
				return Command{}, fmt.Errorf("%s needs a value", a)
			}
			c.Opts[strings.ToLower(k)] = rest[i+1]
			i++
			continue
		}
		c.Args = append(c.Args, a)
	}
	switch c.Verb {
	case "create", "config", "description", "failure", "failureflag", "start", "stop", "query", "delete":
	default:
		return Command{}, fmt.Errorf("unsupported service command %q", c.Verb)
	}
	return c, nil
}

// Action is one failure action.
type Action struct {
	Restart bool // else no action
	Delay   time.Duration
}

// ParseActions reads "restart/5000/restart/5000/…" (sc.exe failure
// actions=): pairs of action and delay in milliseconds.
func ParseActions(s string) ([]Action, error) {
	parts := strings.Split(s, "/")
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("actions %q: want action/delay pairs", s)
	}
	var out []Action
	for i := 0; i < len(parts); i += 2 {
		ms, err := strconv.Atoi(parts[i+1])
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("actions %q: bad delay %q", s, parts[i+1])
		}
		switch strings.ToLower(parts[i]) {
		case "restart":
			out = append(out, Action{Restart: true, Delay: time.Duration(ms) * time.Millisecond})
		case "", "none":
			out = append(out, Action{Delay: time.Duration(ms) * time.Millisecond})
		default:
			return nil, fmt.Errorf("actions %q: unsupported action %q", s, parts[i])
		}
	}
	return out, nil
}

// State names as sc.exe prints them.
var stateNames = map[uint32]string{1: "STOPPED", 2: "START_PENDING", 3: "STOP_PENDING", 4: "RUNNING", 5: "CONTINUE_PENDING", 6: "PAUSE_PENDING", 7: "PAUSED"}

// QueryText is the output of `sc.exe query` for a state, which the plans'
// wait steps match on.
func QueryText(name string, state uint32, pid uint32) string {
	n := stateNames[state]
	if n == "" {
		n = "UNKNOWN"
	}
	return fmt.Sprintf("SERVICE_NAME: %s\n        STATE              : %d  %s\n        PID                : %d\n", name, state, n, pid)
}

// NoServiceText is sc.exe's answer for a service that does not exist.
func NoServiceText() string {
	return "[SC] OpenService FAILED " + ErrNoService + ":\n\nThe specified service does not exist as an installed service.\n"
}

// StartTypeCode maps start= values to the SCM's start types.
func StartTypeCode(s string) (uint32, error) {
	switch strings.ToLower(s) {
	case "auto":
		return 2, nil // SERVICE_AUTO_START
	case "demand", "":
		return 3, nil // SERVICE_DEMAND_START
	case "disabled":
		return 4, nil // SERVICE_DISABLED
	}
	return 0, fmt.Errorf("unknown start type %q", s)
}
