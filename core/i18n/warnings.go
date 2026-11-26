package i18n

import (
	"regexp"
	"strings"
)

// The helper reports compose, apply and takeover warnings as English text
// (core/config layer transforms, core/engine). Warning translates the ones
// it knows into the UI language; others pass through unchanged. The
// patterns follow the engine's wording: TestWarningsFromEngine (cmd/app)
// runs the real transforms so a reworded warning shows up there.

type warnPattern struct {
	kind string
	re   *regexp.Regexp
	list map[int]bool // captures that are a Go %v list ("[a b]"), joined in the UI's way
}

var warnPatterns = []warnPattern{
	{kind: "group-missing", re: regexp.MustCompile(`^proxy group "(.+)" not found$`)},
	{kind: "group-empty", re: regexp.MustCompile(`^proxy group "(.+)" has no members left; set to REJECT$`)},
	{kind: "group-direct", re: regexp.MustCompile(`^proxy group "(.+)" can still reach DIRECT through \[(.*)\](?: \(nested groups are not changed.*\))?$`), list: map[int]bool{2: true}},
	{kind: "nested-shared", re: regexp.MustCompile(`^nested group "(.+)" of a guarded group goes DIRECT when it has no members at run time \(it is used by other traffic too`)},
	{kind: "nested-direct", re: regexp.MustCompile(`^nested group "(.+)" of a guarded group goes DIRECT when it has no members at run time \(leak_guard_nested`)},
	{kind: "fakeip-whitelist", re: regexp.MustCompile(`^dns\.fake-ip-filter-mode is whitelist: the Tailscale fake-ip-filter entries were left out`)},
	{kind: "fakeip-unknown", re: regexp.MustCompile(`^dns\.fake-ip-filter-mode "(.*)" is unknown; the Tailscale fake-ip-filter entries were left out$`)},
	{kind: "new-secret", re: regexp.MustCompile(`^generated a new controller secret`)},
	{kind: "reload-disrupts", re: regexp.MustCompile(`^this reload interrupts the network briefly: (.*)$`)},
	{kind: "restart-needed", re: regexp.MustCompile(`^a hot reload does not apply (.*); the controller keeps its address and secret until mihomo restarts$`)},
	{kind: "mihomo-stopped", re: regexp.MustCompile(`^mihomo is not running \((.*)\); the config was written and will be used at the next start$`)},
	{kind: "exposed", re: regexp.MustCompile(`^(\S+): (\S+) is reachable from the network \(protected by the secret only\)$`)},
	{kind: "adopt-refuses", re: regexp.MustCompile(`^adopt will refuse this setup: (.*)$`)},
	{kind: "adopt-profiles", re: regexp.MustCompile(`^(\d+) subscription profiles found; adopt does not import a subscription URL`)},
	{kind: "adopt-disrupts", re: regexp.MustCompile(`^adopt's reload interrupts the network briefly: (.*)$`)},
	{kind: "adopt-differs", re: regexp.MustCompile(`^the applied config differs from the plan's composed\.yaml`)},
	{kind: "binary-writable", re: regexp.MustCompile(`^the mihomo binary (.+) can be replaced without root \((.*)\), and the service runs it as root`)},
	{kind: "plain-http", re: regexp.MustCompile(`^the subscription URL uses plain http: `)},
	{kind: "secret-clients", re: regexp.MustCompile(`^clients of the controller \(the web dashboard, the app, scripts\) need the new secret; .*?(sudo security find-generic-password .*?)\)?$`)},
}

// WarningKind names the kind of a known warning ("reload-disrupts", ...),
// or "" for one duet does not know. The UI uses it to skip what another
// row already says.
func WarningKind(w string) string {
	if _, rest, ok := strings.Cut(w, ": "); ok {
		for _, p := range warnPatterns {
			if p.re.MatchString(rest) {
				return p.kind
			}
		}
	}
	for _, p := range warnPatterns {
		if p.re.MatchString(w) {
			return p.kind
		}
	}
	return ""
}

// Warning returns w in the UI language when it is a warning duet knows.
// Compose puts the layer's ID first ("leak-guard: ..."); a known text names
// its source itself, so the ID is dropped.
func (s *Strings) Warning(w string) string {
	if out, ok := s.warning(w); ok {
		return out
	}
	if layer, rest, ok := strings.Cut(w, ": "); ok && !strings.ContainsAny(layer, " \t") {
		if out, ok := s.warning(rest); ok {
			return out
		}
	}
	return w
}

func (s *Strings) warning(w string) (string, bool) {
	for _, p := range warnPatterns {
		m := p.re.FindStringSubmatch(w)
		if m == nil {
			continue
		}
		tmpl, ok := s.WarningTexts[p.kind]
		if !ok {
			return "", false
		}
		args := make([]any, 0, len(m)-1)
		for i, v := range m[1:] {
			if p.list[i+1] {
				v = strings.Join(strings.Fields(v), s.ListSep)
			}
			args = append(args, v)
		}
		return F(tmpl, args...), true
	}
	return "", false
}
