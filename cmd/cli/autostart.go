package main

// `twinnel autostart`: other proxy clients registered to start by
// themselves, and turning that off (through the helper; preview first,
// undo with enable).

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
)

func init() {
	register("autostart", "other proxy apps that start by themselves (list | disable ID... | enable ID...)", runAutostart)
}

const autostartUsage = `usage: twinnel autostart [list] [--json]
       twinnel autostart disable [--yes] ID...   turn them off (shows the steps, asks)
       twinnel autostart enable ID...            undo a disable

Lists how other proxy clients (Clash Party, Clash Verge, ...) start by
themselves (%s): a takeover that only stopped them loses at the
next login. Entries marked "helper" are turned off by the helper
(disabled, never deleted); the others say where or how.

flags:
`

// autostartKinds names where other apps start by themselves on the
// system the help is worded for (core/autostart's sources).
func autostartKinds() string {
	if k := byOS(map[string]string{
		i18n.Darwin:  "LaunchAgents, LaunchDaemons and login items",
		i18n.Windows: "scheduled tasks and Run keys",
		i18n.Linux:   "systemd units and autostart files",
	}); k != "" {
		return k
	}
	return "autostart entries"
}

func runAutostart(args []string) error {
	fs, cf := newFlagSet("autostart", fmt.Sprintf(autostartUsage, autostartKinds()))
	var yes bool
	fs.BoolVar(&yes, "yes", false, "with disable: do not ask")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	op := "list"
	if len(pos) > 0 {
		op, pos = pos[0], pos[1:]
	}
	switch op {
	case "list":
		var l rpc.AutostartList
		return cf.callPrint(rpc.MethodAutostartList, nil, &l, func() { printAutostarts(l) })
	case "disable", "enable":
		if len(pos) == 0 {
			fs.Usage()
			return errors.New("name the entries (their IDs from `twinnel autostart`)")
		}
		p := rpc.AutostartChangeParams{IDs: pos, Op: op}
		if op == "disable" && !yes && !cf.json {
			p.Preview = true
			var pr rpc.AutostartChangeResult
			if err := cf.call(rpc.MethodAutostartChange, p, &pr); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "The helper will run:")
			for _, s := range pr.Steps {
				fmt.Fprintln(stdout, "  "+s)
			}
			fmt.Fprint(stdout, "Go ahead? [y/N] ")
			l, _ := bufio.NewReader(stdin).ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "y") {
				return errors.New("nothing changed")
			}
			p.Preview = false
		}
		var r rpc.AutostartChangeResult
		return cf.callPrint(rpc.MethodAutostartChange, p, &r, func() {
			if r.DryRun {
				fmt.Fprintf(stdout, "# dry run: the helper is not running %s, nothing was run\n", service.Current().AsAdmin())
			}
			for _, s := range r.Steps {
				fmt.Fprintln(stdout, "  "+s)
			}
			if r.Undo != nil {
				fmt.Fprintf(stdout, "undo: twinnel autostart %s %s\n", r.Undo.Op, strings.Join(quoteIDs(r.Undo.IDs), " "))
			}
		})
	}
	fs.Usage()
	return fmt.Errorf("unknown autostart command %q", op)
}

func printAutostarts(l rpc.AutostartList) {
	if !l.Supported {
		fmt.Fprintln(stdout, "reading autostart entries is not supported on this system")
		return
	}
	if len(l.Entries) == 0 {
		fmt.Fprintln(stdout, "no other proxy app starts by itself")
	}
	for _, e := range l.Entries {
		state := "starts by itself"
		if !e.Enabled {
			state = "turned off"
		}
		fmt.Fprintf(stdout, "%s  [%s]\n  %s · %s", e.ID, state, e.Client, e.Kind)
		if e.Program != "" {
			fmt.Fprintf(stdout, " · %s", e.Program)
		}
		fmt.Fprintln(stdout)
		if e.Enabled {
			switch e.Disable {
			case autostart.ByHelper:
				fmt.Fprintf(stdout, "  turn off: twinnel autostart disable %s\n", quoteIDs([]string{e.ID})[0])
			case autostart.Manual:
				fmt.Fprintf(stdout, "  turn off: %s\n", e.Command)
			case autostart.Guided:
				fmt.Fprintln(stdout, "  turn off: in the system's login items settings, or the app's own \"launch at login\"")
			}
		}
	}
	if l.Error != "" {
		fmt.Fprintln(stdout, "some places could not be read:", l.Error)
	}
}

func quoteIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		if strings.ContainsAny(id, ` \'"`) {
			id = "'" + strings.ReplaceAll(id, "'", `'\''`) + "'"
		}
		out[i] = id
	}
	return out
}
