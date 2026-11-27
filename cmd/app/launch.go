package main

import (
	"fmt"
	"log"
	"slices"
	"strings"
)

// What a launch opens, decided in one place for every platform.
//
// A normal launch shows only the menu bar or tray icon, except the first
// launch of a machine not set up: wizardLaunchCheck opens the wizard as
// soon as the helper's state is known (launchOpensWizard). A development
// or agent run against a scratch helper (TWINNEL_SOCKET) never opens a
// window by itself, so it never takes the focus of the person at the
// machine; it asks for windows explicitly:
//
//	TWINNEL_OPEN=1            the main window, as a real launch would show
//	                          it (the wizard when the machine is not set up)
//	TWINNEL_OPEN=panel        the main window and the flyout
//	TWINNEL_OPEN=page:<id>    the main window at page <id> (allPages), for
//	                          scripted GUI QA that must not click by
//	                          coordinates; page:wizard opens the wizard
//
// The same values work as a command line argument, --open=<value>, which
// also reaches a running copy: on Windows a second launch hands its
// arguments to the first (Twinnel.exe --open=page:kernel turns the open
// window to 内核).

// envOpen names the development variable.
const envOpen = "TWINNEL_OPEN"

// launchOpen is what a launch was asked to open.
type launchOpen struct {
	main  bool   // the main window
	panel bool   // and the flyout
	page  string // at this page; "" leaves the page to the launch
}

// parseLaunchOpen reads a TWINNEL_OPEN or --open value. An unknown page
// is an error; the main window still opens (at the page it would show).
func parseLaunchOpen(v string) (launchOpen, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "" || v == "0":
		return launchOpen{}, nil
	case v == "panel":
		return launchOpen{main: true, panel: true}, nil
	case strings.HasPrefix(v, "page:"):
		id := strings.TrimSpace(strings.TrimPrefix(v, "page:"))
		if !slices.Contains(allPages, id) {
			return launchOpen{main: true}, fmt.Errorf("unknown page %q (pages: %s)", id, strings.Join(allPages, ", "))
		}
		return launchOpen{main: true, page: id}, nil
	}
	return launchOpen{main: true}, nil // "1" and anything else
}

// openArg is the --open value among args ("" when there is none).
func openArg(args []string) (string, bool) {
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, "--open="):
			return strings.TrimPrefix(a, "--open="), true
		case a == "--open" && i+1 < len(args):
			return args[i+1], true
		}
	}
	return "", false
}

// launchRequest is what this launch was asked to open: --open, else
// TWINNEL_OPEN.
func launchRequest(args []string, getenv func(string) string) (launchOpen, error) {
	if v, ok := openArg(args); ok {
		return parseLaunchOpen(v)
	}
	return parseLaunchOpen(getenv(envOpen))
}

// openRequested opens what o asks for. Main thread.
func (a *app) openRequested(o launchOpen) {
	if !o.main {
		return
	}
	switch {
	case o.page == pageWizard:
		a.wiz.checked = true // the launch check would only open it again
		a.openPage(pageWizard)
	case o.page != "":
		a.wiz.checked = true // the page asked for stays in front
		a.openPage(o.page)
	default:
		a.openMain()
	}
	if o.panel {
		a.togglePanel()
	}
}

// openAtLaunch opens what this launch asked for (nothing for a normal
// launch: the icon, then wizardLaunchCheck). Main thread.
func (a *app) openAtLaunch() { a.openRequested(a.launch) }

// secondLaunch is a second copy started while this one runs (Windows,
// the Start menu or the login entry again): its --open, else the main
// window. Main thread.
func (a *app) secondLaunch(args []string) {
	v, ok := openArg(args)
	if !ok {
		a.openMain()
		return
	}
	o, err := parseLaunchOpen(v)
	if err != nil {
		log.Printf("open: %v", err)
	}
	if !o.main {
		o.main = true // a second launch always shows the window
	}
	a.openRequested(o)
}
