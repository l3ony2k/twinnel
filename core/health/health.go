// Package health defines the coexistence health checks of Twinnel and runs them.
//
// A Check reads facts through an Env (implemented per platform, faked in
// tests) and returns a Result. Checks never change the system.
package health

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Status is the outcome of a check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// severity orders statuses from best to worst; skip counts as best.
func (s Status) severity() int {
	switch s {
	case StatusOK:
		return 1
	case StatusWarn:
		return 2
	case StatusFail:
		return 3
	}
	return 0
}

// Worse returns the worse of two statuses.
func Worse(a, b Status) Status {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// Platform names, matching runtime.GOOS.
const (
	Darwin  = "darwin"
	Linux   = "linux"
	Windows = "windows"
)

// AllPlatforms is a convenience for checks that apply everywhere.
var AllPlatforms = []string{Darwin, Linux, Windows}

// FixHint tells the user how to fix a problem. AutoFix names an automatic
// fix the helper may offer later (empty if none).
type FixHint struct {
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
	AutoFix string `json:"autoFix,omitempty"`
	// Param is what the fix acts on, when it needs one: the proxy group
	// (leak guard, the egress test's node) or the controller's port.
	Param string `json:"param,omitempty"`
}

// Result is what a check reports.
type Result struct {
	Status   Status   `json:"status"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence,omitempty"`
	Fix      *FixHint `json:"fix,omitempty"`
	// Causes name what a warn or fail comes from (Cause* codes), so a
	// cause the user accepted can be told apart (Accept). Empty: unknown.
	Causes []string `json:"causes,omitempty"`
	// Reasons name why a warn or fail happened (Reason* codes), each with
	// a fix in the catalog (FixFor). Empty in reports from older helpers.
	Reasons []string `json:"reasons,omitempty"`
	// Egress is what the egress test (LeakEgress) saw, unmasked. It is
	// never marshalled with the result: reports, logs, the CLI and MCP
	// keep MaskIP'd evidence. The helper hands it to the app only when
	// asked (rpc ips), and the app shows it.
	Egress *EgressIPs `json:"-"`
	// Unsupported: the check cannot run on this platform (a skip that
	// says so). The app offers to turn off a periodic test that has it.
	Unsupported bool `json:"unsupported,omitempty"`
}

// EgressIPs are the public addresses the egress test saw: through mihomo
// (and the node that carried it) and directly. A zero Addr was not
// measured.
type EgressIPs struct {
	Proxied netip.Addr
	Direct  netip.Addr
	Node    string // the node mihomo used for the proxied request
}

// Check is one health check.
type Check interface {
	ID() string
	Title() string // user-facing, in DefaultLang (built-in checks word it per run)
	Platforms() []string
	Run(ctx context.Context, env Env) Result
}

// Registry holds checks in registration order.
type Registry struct {
	mu     sync.Mutex
	checks []Check
	ids    map[string]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{ids: map[string]bool{}} }

// Register adds a check. It panics on a duplicate ID.
func (r *Registry) Register(c Check) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ids[c.ID()] {
		panic("health: duplicate check " + c.ID())
	}
	r.ids[c.ID()] = true
	r.checks = append(r.checks, c)
}

// Checks returns the registered checks in order.
func (r *Registry) Checks() []Check {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Check(nil), r.checks...)
}

// Get returns a check by ID.
func (r *Registry) Get(id string) (Check, bool) {
	for _, c := range r.Checks() {
		if c.ID() == id {
			return c, true
		}
	}
	return nil, false
}

// CheckIDs returns an error naming the ids that are no registered check,
// with the list of valid ones; nil when all are known. A run limited to
// an unknown id would run nothing and report "pass".
func (r *Registry) CheckIDs(ids []string) error {
	var bad []string
	for _, id := range ids {
		if _, ok := r.Get(id); !ok {
			bad = append(bad, id)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("unknown check %s (checks: %s)", strings.Join(quoteAll(bad), ", "), strings.Join(r.IDs(), ", "))
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strconv.Quote(s)
	}
	return out
}

// IDs returns the sorted IDs of registered checks.
func (r *Registry) IDs() []string {
	var ids []string
	for _, c := range r.Checks() {
		ids = append(ids, c.ID())
	}
	sort.Strings(ids)
	return ids
}

func supports(c Check, platform string) bool {
	for _, p := range c.Platforms() {
		if p == platform {
			return true
		}
	}
	return false
}

func skip(format string, a ...any) Result {
	return Result{Status: StatusSkip, Summary: fmt.Sprintf(format, a...)}
}

// skipErr is a skip with a short reason; the error goes to the evidence.
func skipErr(summary string, err any) Result {
	return Result{Status: StatusSkip, Summary: summary, Evidence: []string{fmt.Sprint(err)}}
}

// TidyEvidence drops what would render as an empty evidence line: blank
// entries, and blank lines and trailing spaces inside an entry. The runner
// tidies every result, and every renderer (Report.Text, Markdown, the
// app's DedupeEvidence) tidies again, for reports from an older helper.
func TidyEvidence(lines []string) []string {
	var out []string
	for _, e := range lines {
		var keep []string
		for _, l := range strings.Split(e, "\n") {
			if l = strings.TrimRight(l, " \t\r"); strings.TrimSpace(l) != "" {
				keep = append(keep, l)
			}
		}
		if len(keep) > 0 {
			out = append(out, strings.Join(keep, "\n"))
		}
	}
	return out
}

// skipText is skip with a text that has no format verbs.
func skipText(s string) Result { return Result{Status: StatusSkip, Summary: s} }
