// Package msg turns the helper's English texts (warnings, apply errors,
// layer errors) into codes with arguments, so a client words them in its
// own language (core/i18n Strings.Msg). The helper codes what it sends
// (rpc.ComposeResult.Messages, rpc.ApplyResult.ErrorMsg); a client codes
// an older helper's texts and RPC error messages with the same table.
//
// The patterns follow the engine's wording: TestWarningsFromEngine
// (cmd/app) and this package's tests run the real producers, so a
// reworded text shows up there.
package msg

import (
	"regexp"
	"strings"

	"github.com/l3ony2k/twinnel/core/rpc"
)

type pattern struct {
	code string
	re   *regexp.Regexp
	list map[int]bool // captures (1-based) that are a Go %v list ("a b")
}

var patterns = []pattern{
	// Compose and apply warnings.
	{code: "group-missing", re: regexp.MustCompile(`^proxy group "(.+)" not found$`)},
	{code: "group-empty", re: regexp.MustCompile(`^proxy group "(.+)" has no members left; set to REJECT$`)},
	{code: "group-direct", re: regexp.MustCompile(`^proxy group "(.+)" can still reach DIRECT through \[(.*)\](?: \(nested groups are not changed.*\))?$`), list: map[int]bool{2: true}},
	{code: "nested-shared", re: regexp.MustCompile(`^nested group "(.+)" of a guarded group goes DIRECT when it has no members at run time \(it is used by other traffic too`)},
	{code: "nested-direct", re: regexp.MustCompile(`^nested group "(.+)" of a guarded group goes DIRECT when it has no members at run time \(leak_guard_nested`)},
	{code: "fakeip-whitelist", re: regexp.MustCompile(`^dns\.fake-ip-filter-mode is whitelist: the Tailscale fake-ip-filter entries were left out`)},
	{code: "fakeip-unknown", re: regexp.MustCompile(`^dns\.fake-ip-filter-mode "(.*)" is unknown; the Tailscale fake-ip-filter entries were left out$`)},
	{code: "new-secret", re: regexp.MustCompile(`^generated a new controller secret`)},
	{code: "reload-disrupts", re: regexp.MustCompile(`^this reload interrupts the network briefly: (.*)$`)},
	{code: "restart-needed", re: regexp.MustCompile(`^a hot reload does not apply (.*); the controller keeps its address and secret until mihomo restarts$`)},
	{code: "mihomo-stopped", re: regexp.MustCompile(`^mihomo is not running \((.*)\); the config was written and will be used at the next start$`)},
	{code: "exposed", re: regexp.MustCompile(`^(\S+): (\S+) is reachable from the network \(protected by the secret only\)$`)},
	{code: "adopt-refuses", re: regexp.MustCompile(`^adopt will refuse this setup: (.*)$`)},
	{code: "adopt-profiles", re: regexp.MustCompile(`^(\d+) subscription profiles found; adopt does not import a subscription URL`)},
	{code: "adopt-disrupts", re: regexp.MustCompile(`^adopt's reload interrupts the network briefly: (.*)$`)},
	{code: "adopt-differs", re: regexp.MustCompile(`^the applied config differs from the plan's composed\.yaml`)},
	{code: "binary-writable", re: regexp.MustCompile(`^the mihomo binary (.+) can be replaced without root \((.*)\), and the service runs it as root`)},
	{code: "plain-http", re: regexp.MustCompile(`^the subscription URL uses plain http: `)},
	{code: "secret-clients", re: regexp.MustCompile(`^clients of the controller \(the web dashboard, the app, scripts\) need the new secret; .*?(sudo security find-generic-password .*?)\)?$`)},

	// Apply errors (core/engine apply.go).
	{code: "no-active-subscription", re: regexp.MustCompile(`^no active subscription`)},
	{code: "subscription-empty", re: regexp.MustCompile(`^subscription (\S+) has no content yet`)},
	{code: "validate-failed", re: regexp.MustCompile(`^mihomo -t rejected the config`)},
	{code: "refuses-exposed", re: regexp.MustCompile(`^Twinnel refuses (\S+): (\S+) listens beyond this Mac and the controller has no secret`)},
	{code: "refuses-socket", re: regexp.MustCompile(`^Twinnel refuses (\S+): (.+) is outside the config dir (.+) \(mihomo, as root`)},
	{code: "backup-failed", re: regexp.MustCompile(`^backup: (.*)$`)},
	{code: "write-failed", re: regexp.MustCompile(`^write (\S+): (.*)$`)},
	{code: "controller-foreign", re: regexp.MustCompile(`^the controller belongs to another mihomo: (\S+) is held by another program; Twinnel did not reload it$`)},
	{code: "controller-foreign-app", re: regexp.MustCompile(`^the controller belongs to another mihomo: (\S+) is held by (.+); Twinnel did not reload it$`)},
	// An older helper's raw refusal (never shown as is: SAFE_PATHS means
	// another mihomo answered).
	{code: "reload-safe-paths", re: regexp.MustCompile(`^reload: .*path is not subpath of home directory or SAFE_PATHS`)},
	{code: "reload-failed", re: regexp.MustCompile(`^reload: (.*)$`)},
	{code: "verify-failed", re: regexp.MustCompile(`^mihomo did not answer after reload: (.*)$`)},

	// Helper errors without a code of their own (core/engine, core/takeover).
	// Not anchored: they come wrapped ("takeover plan: live config: …").
	{code: "live-config-missing", re: regexp.MustCompile(`live config(?: \S+)?: open (\S+): no such file or directory$`)},
	{code: "replan", re: regexp.MustCompile(`; re-plan$`)},
	{code: "sub-no-url", re: regexp.MustCompile(`^subscription "?([^"\s]+)"? has no URL$`)},
	{code: "mihomo-no-answer", re: regexp.MustCompile(`^the running mihomo does not answer`)},
	{code: "no-service-manager", re: regexp.MustCompile(`no service manager on this platform$`)},
	{code: "health-unavailable", re: regexp.MustCompile(`^health checks are not available in this helper$`)},
	{code: "takeover-unsupported", re: regexp.MustCompile(`^takeover is not supported by this helper`)},

	// Install errors of kernel.install and tailscale.install (core/kernel,
	// core/engine setup.go). Not anchored: they come wrapped.
	{code: "msi-busy", re: regexp.MustCompile(`another installation is in progress \(Windows Installer is busy\)`)},
	{code: "msi-canceled", re: regexp.MustCompile(`the Tailscale installation was canceled`)},
	{code: "msi-blocked", re: regexp.MustCompile(`a system policy blocks installing Tailscale`)},
	{code: "msi-other-version", re: regexp.MustCompile(`a different version of Tailscale is already installed`)},
	{code: "msi-failed", re: regexp.MustCompile(`the Tailscale installer failed \(msiexec exit code (-?\d+)\); its log is (.+)$`)},
	{code: "ts-service-down", re: regexp.MustCompile(`Tailscale was installed but its service did not start`)},
	{code: "ts-cli-no-run", re: regexp.MustCompile(`Tailscale was installed but (.+) does not run`)},
	{code: "ts-no-installer", re: regexp.MustCompile(`pkgs\.tailscale\.com has no Windows installer for (\S+)$`)},
	{code: "mihomo-no-run", re: regexp.MustCompile(`the downloaded mihomo does not run on this computer`)},
	{code: "mihomo-no-build", re: regexp.MustCompile(`release (\S+) has no asset for (\S+)$`)},
	{code: "ts-login-session", re: regexp.MustCompile(`Tailscale logs in from the signed-in user's session on this system`)},
	{code: "download-damaged", re: regexp.MustCompile(`checksum mismatch: got sha256:`)},

	// Layer errors (core/config merge, layer.put, compose). Args: layer,
	// line ("" when unknown), then the rest.
	{code: "layer-needs-list", re: regexp.MustCompile(`^layer (\S+): (?:line (\d+): )?(?:.*: )?"(.+)" needs a list, got (\w+)$`)},
	{code: "layer-verge-list", re: regexp.MustCompile(`^layer (\S+): (?:line (\d+): )?([a-z-]+): needs a list, got (\w+)$`)},
	{code: "layer-not-mapping", re: regexp.MustCompile(`^layer (\S+): (?:line (\d+): )?override must be a mapping, got (\w+)$`)},
}

// Codes lists every code the table knows, for completeness tests.
func Codes() []string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = p.code
	}
	return out
}

// Parse codes text. Compose puts a layer's ID first ("leak-guard: ...");
// a known text names its source itself, so that prefix is dropped. An
// unknown text comes back with Code "".
func Parse(text string) rpc.Msg {
	if m, ok := parse(text); ok {
		return m
	}
	if layer, rest, ok := strings.Cut(text, ": "); ok && !strings.ContainsAny(layer, " \t") {
		if m, ok := parse(rest); ok && !strings.HasPrefix(m.Code, "layer-") {
			m.Text = text
			return m
		}
	}
	return rpc.Msg{Text: text}
}

func parse(text string) (rpc.Msg, bool) {
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		args := make([]string, 0, len(m)-1)
		for i, v := range m[1:] {
			if p.list[i+1] {
				v = strings.Join(strings.Fields(v), "\n")
			}
			args = append(args, v)
		}
		return rpc.Msg{Code: p.code, Args: args, Text: text}, true
	}
	return rpc.Msg{}, false
}

// All codes each text.
func All(texts []string) []rpc.Msg {
	if len(texts) == 0 {
		return nil
	}
	out := make([]rpc.Msg, len(texts))
	for i, t := range texts {
		out[i] = Parse(t)
	}
	return out
}

// Of returns the coded form of a result's warnings: the helper's own
// Messages when it sent them for every warning, else the texts coded here.
func Of(r rpc.ComposeResult) []rpc.Msg {
	if len(r.Messages) == len(r.Warnings) && len(r.Messages) > 0 {
		return r.Messages
	}
	return All(r.Warnings)
}

// Kind returns the code of a known text, "" for an unknown one.
func Kind(text string) string { return Parse(text).Code }
