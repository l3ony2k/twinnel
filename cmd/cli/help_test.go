package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// captureOutput runs fn with os.Stdout, os.Stderr and this package's
// stdout/stderr going to one buffer, and returns what it wrote.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr, oldPkgOut, oldPkgErr := os.Stdout, os.Stderr, stdout, stderr
	os.Stdout, os.Stderr, stdout, stderr = w, w, w, w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stdout, os.Stderr, stdout, stderr = oldOut, oldErr, oldPkgOut, oldPkgErr }()
		fn()
	}()
	w.Close()
	return <-done
}

// subcommands finds the subcommands a usage text lists: "twinnel helper
// install ..." under the usage of "helper".
func subcommands(path []string, usage string) []string {
	re := regexp.MustCompile(`(?m)\btwinnel ` + regexp.QuoteMeta(strings.Join(path, " ")) + ` ([a-z][a-z-]+)\b`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(usage, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// helpTexts renders the CLI's help for goos: the command list, each
// command's --help and, recursively, the --help of every subcommand its
// usage lists. Keys are the command paths.
func helpTexts(t *testing.T, goos string) map[string]string {
	t.Helper()
	defer setHelpOS(goos)()
	out := map[string]string{"": captureOutput(t, usage)}
	var walk func(path []string)
	walk = func(path []string) {
		key := strings.Join(path, " ")
		if _, seen := out[key]; seen || len(path) > 3 {
			return
		}
		args := append(slices.Clone(path[1:]), "--help")
		text := captureOutput(t, func() { _ = commands[path[0]].run(args) })
		out[key] = text
		for _, sub := range subcommands(path, text) {
			if shownOn(append(slices.Clone(path), sub), goos) {
				walk(append(slices.Clone(path), sub))
			}
		}
	}
	for name := range commands {
		if shownOn([]string{name}, goos) {
			walk([]string{name})
		}
	}
	return out
}

// TestHelpGlossary: on each system, the CLI's help (every command's and
// subcommand's --help and flag usage, and the command list) names no
// other system's things (core/i18n's glossary): a Windows user never
// reads "LaunchDaemon", "plist" or "as root".
func TestHelpGlossary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TWINNEL_CONTROLLER", "127.0.0.1:1")
	var bad []string
	for _, goos := range i18n.Platforms {
		texts := helpTexts(t, goos)
		if len(texts) < 30 {
			t.Errorf("%s: only %d help texts rendered", goos, len(texts))
		}
		for path, text := range texts {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: twinnel %s --help printed nothing", goos, path)
			}
			for _, line := range strings.Split(text, "\n") {
				if f := i18n.ForeignTerms(goos, line); len(f) > 0 {
					bad = append(bad, goos+" `twinnel "+path+" --help` ["+strings.Join(f, ", ")+"]: "+strings.TrimSpace(line))
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// TestHelpOnlyOn: a macOS-only subcommand (harden) and its flags are not
// listed on the other systems and refuse to run there; on macOS they are.
func TestHelpOnlyOn(t *testing.T) {
	for _, goos := range i18n.Platforms {
		texts := helpTexts(t, goos)
		mac := goos == i18n.Darwin
		_, listed := texts["helper harden"]
		if listed != mac {
			t.Errorf("%s: helper harden listed=%v", goos, listed)
		}
		for _, path := range []string{"", "helper", "helper uninstall"} {
			if got := strings.Contains(texts[path], "harden"); got != mac {
				t.Errorf("%s: `twinnel %s --help` mentions harden=%v:\n%s", goos, path, got, texts[path])
			}
		}
		if got := strings.Contains(texts["helper install"], "-allow-sid"); got != (goos == i18n.Windows) {
			t.Errorf("%s: helper install shows --allow-sid=%v", goos, got)
		}
		if strings.Contains(texts["helper install"], "-as-root") {
			t.Errorf("%s: helper install shows the internal --as-root", goos)
		}
		if !mac {
			restore := setHelpOS(goos)
			err := runHelper([]string{"harden", "plan"})
			restore()
			if err == nil || !strings.Contains(err.Error(), "not available") {
				t.Errorf("%s: helper harden plan: %v", goos, err)
			}
		}
	}
}

// TestFlagSetsWorded: every flag set is made by newFlags (wording.go), so
// its help hides internal and other systems' flags; none prints
// PrintDefaults directly.
func TestFlagSetsWorded(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "wording.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"flag.NewFlagSet(", ".PrintDefaults()"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s: %s outside wording.go (use newFlags / setUsage)", f, bad)
			}
		}
	}
}
