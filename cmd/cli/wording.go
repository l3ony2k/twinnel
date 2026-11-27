package main

// How the CLI's help and usage text is worded per system. Every text that
// names a system's thing (its service manager, the account plans run as,
// where a service is defined) reads it from sys(), the dialect of the
// system the text is worded for; commands and subcommands that exist on
// some systems only are listed in onlyOn and hidden elsewhere.
// TestHelpGlossary renders every --help for each system and checks it
// against core/i18n's glossary.

import (
	"flag"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/windows/scm"
)

// helpOS is the system the help is worded for: this one (tests word it
// for each in turn, setHelpOS).
var helpOS = runtime.GOOS

// setHelpOS words the help for goos until the returned func runs.
func setHelpOS(goos string) (restore func()) {
	old := helpOS
	helpOS = goos
	return func() { helpOS = old }
}

// sys is the dialect of the system the help is worded for: the running
// system's own (service.Current) unless a test words it for another.
func sys() service.Dialect {
	if helpOS == runtime.GOOS {
		return service.Current()
	}
	return dialectFor(helpOS)
}

// dialectFor is goos's plan dialect on any host.
func dialectFor(goos string) service.Dialect {
	if goos == i18n.Windows {
		return scm.Dialect
	}
	return service.UnixFor(goos)
}

// onlyOn are the commands and subcommands (by path) that exist on some
// systems only. They are not listed, and refuse to run, elsewhere.
var onlyOn = map[string][]string{
	// harden moves an adopted LaunchDaemon (docs/runbook-adopt-macos.md).
	"helper harden": {i18n.Darwin},
}

// shownOn reports whether the command at path exists on goos.
func shownOn(path []string, goos string) bool {
	if c, ok := commands[path[0]]; ok && c.only != nil && !slices.Contains(c.only, goos) {
		return false
	}
	for i := range path {
		if only, ok := onlyOn[strings.Join(path[:i+1], " ")]; ok && !slices.Contains(only, goos) {
			return false
		}
	}
	return true
}

// flagOnlyOn are the flags (by name) that only some systems have. Elsewhere
// they are still accepted (the app passes --restore-mihomo everywhere) but
// not shown.
var flagOnlyOn = map[string][]string{
	// The helper's pipe and the elevated copy's output pipe.
	"allow-sid": {i18n.Windows}, "output": {i18n.Windows},
	// What uninstall does after harden (macOS only).
	"restore-mihomo": {i18n.Darwin}, "keep-mihomo": {i18n.Darwin},
}

// flagShown reports whether a flag is shown in help on the system it is
// worded for: not internal ones (usage "internal: …", the elevated copy's
// protocol) and not another system's.
func flagShown(f *flag.Flag) bool {
	if strings.HasPrefix(f.Usage, "internal") {
		return false
	}
	only, ok := flagOnlyOn[f.Name]
	return !ok || slices.Contains(only, helpOS)
}

// newFlags is every command's flag set: -h prints usage (default
// "usage: twinnel NAME [flags]") and then the flags shown on this system.
// Help is worded when the set is made, so usage may use sys().
func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if usage == "" {
		usage = "usage: twinnel " + name + " [flags]\n\nflags:\n"
	}
	setUsage(fs, usage)
	return fs
}

// setUsage makes -h print usage and then the flags shown on this system.
func setUsage(fs *flag.FlagSet, usage string) {
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		printFlags(fs)
	}
}

// printFlags is fs.PrintDefaults without the flags this system does not
// show (flagShown).
func printFlags(fs *flag.FlagSet) {
	shown := flag.NewFlagSet(fs.Name(), flag.ContinueOnError)
	shown.SetOutput(fs.Output())
	fs.VisitAll(func(f *flag.Flag) {
		if flagShown(f) {
			shown.Var(f.Value, f.Name, f.Usage)
			shown.Lookup(f.Name).DefValue = f.DefValue
		}
	})
	shown.PrintDefaults()
}

// byOS picks the text of the system the help is worded for ("" when it
// has none).
func byOS(texts map[string]string) string { return texts[helpOS] }
