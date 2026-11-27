package setup

// The honest end of a setup: what is done, and for every step that is not
// (skipped, failed, waiting or never run), why and what to do about it. A
// client says "Setup complete" only when Remaining is empty: a step that
// was skipped because the platform lacks it, or because the user chose
// to, is still something the machine does not have.

import (
	"strconv"
	"strings"
)

// CodeUserSkipped marks a step the user chose to leave out (--no-dns,
// --no-tailscale, the wizard's 跳过). It is Skipped, but still listed by
// Remaining: the machine does not have it.
const CodeUserSkipped = "user-skipped"

// Why a step is left (Left.Why).
const (
	LeftPending     = "pending"     // never ran (an earlier step stopped the flow)
	LeftWaiting     = "waiting"     // needs the user (a subscription, the login page)
	LeftFailed      = "failed"      // ran and failed (Left.Error says why)
	LeftUnsupported = "unsupported" // this platform has no such step yet
	LeftUserSkipped = "user-skipped"
	LeftHealth      = "health" // the checks found problems (Left.Error names them)
)

// Left is a step that is not done.
type Left struct {
	Step  Step   `json:"step"`
	State State  `json:"state"`
	Why   string `json:"why"`             // Left* code
	Error string `json:"error,omitempty"` // the step's error, if any
	// Do is what to do next, in English, for a terminal or an agent
	// (clients word Why themselves).
	Do string `json:"do"`
}

// Remaining lists the steps that are not done, in order.
func (f *Flow) Remaining() []Left {
	var out []Left
	for _, s := range f.Steps() {
		if s.State == Done {
			continue
		}
		out = append(out, leftOf(s))
	}
	return out
}

// AllDone reports whether every step is done: nothing skipped, failed,
// waiting or pending. Only then is a setup complete.
func (f *Flow) AllDone() bool { return len(f.Remaining()) == 0 }

func leftOf(s StepState) Left {
	l := Left{Step: s.ID, State: s.State, Error: s.Error}
	switch {
	case s.State == Skipped && s.Code == CodeUserSkipped:
		l.Why = LeftUserSkipped
	case s.State == Skipped:
		l.Why = LeftUnsupported
	case s.State == Failed && s.Code == CodeHealthFail:
		l.Why = LeftHealth
	case s.State == Failed:
		l.Why = LeftFailed
	case s.State == Waiting:
		l.Why = LeftWaiting
	default:
		l.Why = LeftPending
	}
	l.Do = whatToDo(s.ID, l.Why, s.Code)
	return l
}

// whatToDo is the next step for a step left as why, for a terminal.
func whatToDo(id Step, why, code string) string {
	retry := "run `twinnel setup --step " + string(id) + "` to try again"
	switch why {
	case LeftUserSkipped:
		switch id {
		case StepDNS:
			return "the system DNS was left alone; mihomo's TUN still answers DNS. Run `twinnel setup --step dns` to point it at mihomo"
		case StepLogin:
			return "Tailscale is left out or stopped; run `twinnel setup --step tailscale`, start it (`twinnel service start tailscaled`), then `twinnel setup --step login`"
		case StepTailscale:
			return "Tailscale was left out; run `twinnel setup --step tailscale` to add it"
		}
		return "run `twinnel setup --step " + string(id) + "` to do it"
	case LeftUnsupported:
		switch id {
		case StepDNS:
			return "Twinnel cannot set the system DNS here yet: point the active network's DNS at 127.0.0.1 in the system settings, then run `twinnel doctor`"
		case StepTailscale:
			return "Twinnel cannot install Tailscale here yet: install it from tailscale.com, then run `twinnel setup` again"
		case StepLogin:
			return "log in with `tailscale login`, then run `twinnel setup` again"
		}
		return "this step is not available on this system yet"
	case LeftHealth:
		return "run `twinnel doctor` to see what failed and how to fix it"
	case LeftWaiting:
		switch id {
		case StepSubscription:
			return "give a subscription: `twinnel setup --subscription URL` (or $TWINNEL_SUBSCRIPTION_URL), or --import an export zip"
		case StepLogin:
			return "finish the Tailscale login in the browser, then run `twinnel setup` again"
		}
		return "run `twinnel setup` again"
	case LeftFailed:
		if id == StepTailscale && code == CodeDamaged {
			return "run `twinnel setup --step tailscale --repair-tailscale` to reinstall it"
		}
		return retry
	}
	return "run `twinnel setup` to continue"
}

// Summary is the last line(s) of a setup for a terminal: "Setup complete."
// only when every step is done; otherwise what is left and what to do.
func (f *Flow) Summary() string {
	left := f.Remaining()
	if len(left) == 0 {
		return "Setup complete."
	}
	var b strings.Builder
	n := len(left)
	word := "steps"
	if n == 1 {
		word = "step"
	}
	b.WriteString("Setup not finished: ")
	b.WriteString(strconv.Itoa(n))
	b.WriteString(" " + word + " left.\n")
	for _, l := range left {
		b.WriteString("  " + string(l.Step) + ": " + leftLabel(l))
		if l.Error != "" && l.Why != LeftUserSkipped {
			b.WriteString(" (" + l.Error + ")")
		}
		b.WriteString("\n    → " + l.Do + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func leftLabel(l Left) string {
	switch l.Why {
	case LeftUserSkipped:
		return "skipped by your choice"
	case LeftUnsupported:
		return "not available on this system"
	case LeftHealth:
		return "health checks failed"
	case LeftFailed:
		return "failed"
	case LeftWaiting:
		return "waiting for you"
	}
	return "not done yet"
}
