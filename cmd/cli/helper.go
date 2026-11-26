package main

// Client subcommands of the helper daemon (core/rpc over its unix socket),
// and `duet helper install --dry-run`.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/service"
	"github.com/l3ony2k/duet/core/store"
)

func init() {
	register("kernel", "mihomo kernels: list | check | install | rollback", runKernel)
	register("layer", "override layers: list | get | put | rm | enable | disable | order", runLayer)
	register("sub", "subscriptions: list | put | refresh | activate | rm", runSub)
	register("history", "composed config history: list | rollback ID", runHistory)
	register("settings", "show or change helper settings (leak guard, coexist, ...)", runSettings)
	register("tailnet", "tailnet: refresh (re-read peers, re-apply if they changed)", runTailnet)
	register("service", "service status NAME (mihomo, tailscaled, helper)", runService)
	register("events", "print helper events (--follow to keep waiting)", runEvents)
	register("helper", "helper status | install --dry-run", runHelper)

	// `duet compose` composes locally; with --apply/--preview it asks the
	// helper to compose from its own state instead.
	local := commands["compose"]
	commands["compose"] = command{local.summary + " (--apply / --preview: through the helper)", func(args []string) error {
		if slices.ContainsFunc(args, func(a string) bool {
			a = strings.TrimLeft(a, "-")
			return a == "apply" || a == "preview" || strings.HasPrefix(a, "apply=") || strings.HasPrefix(a, "preview=")
		}) {
			return runComposeHelper(args)
		}
		return local.run(args)
	}}
}

// ---- shared plumbing ----

type clientFlags struct {
	socket  string
	json    bool
	timeout time.Duration
}

func newFlagSet(name, usage string) (*flag.FlagSet, *clientFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	cf := &clientFlags{}
	fs.StringVar(&cf.socket, "socket", rpc.SocketFromEnv(), "helper socket `PATH` (env DUET_SOCKET)")
	fs.BoolVar(&cf.json, "json", false, "print the raw JSON result")
	fs.DurationVar(&cf.timeout, "timeout", 3*time.Minute, "give up after `DURATION`")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	return fs, cf
}

// parse handles -h (returns done=true) and flags after positional args
// (`duet layer put my-rules file.yaml --stage base`).
func parse(fs *flag.FlagSet, args []string) (pos []string, done bool, err error) {
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, true, nil
			}
			return nil, false, err
		}
		rest := fs.Args()
		i := slices.IndexFunc(rest, func(a string) bool { return strings.HasPrefix(a, "-") && a != "-" })
		if i < 0 {
			return append(pos, rest...), false, nil
		}
		pos = append(pos, rest[:i]...)
		args = rest[i:]
	}
}

func (cf *clientFlags) call(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	c, err := rpc.Dial(ctx, nil, cf.socket)
	if err != nil {
		return fmt.Errorf("cannot reach the helper at %s: %w", cf.socket, err)
	}
	defer c.Close()
	return c.Call(ctx, method, params, result)
}

// callPrint calls and prints JSON when --json is set; otherwise human()
// prints.
func (cf *clientFlags) callPrint(method string, params, result any, human func()) error {
	if cf.json {
		var raw json.RawMessage
		if err := cf.call(method, params, &raw); err != nil {
			return err
		}
		return printJSON(raw)
	}
	if err := cf.call(method, params, result); err != nil {
		return err
	}
	if human != nil {
		human()
	}
	return nil
}

func printJSON(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
	return nil
}

func tw() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0) }

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Second)
	return t.Local().Format("2006-01-02 15:04:05") + " (" + d.String() + " ago)"
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func needArgs(pos []string, n int, usage string) error {
	if len(pos) != n {
		return fmt.Errorf("usage: %s", usage)
	}
	return nil
}

// ---- status ----

func runHelperStatus(args []string) error {
	fs, cf := newFlagSet("status", "usage: duet status [--json]\n\n")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	var s rpc.Status
	return cf.callPrint(rpc.MethodStatus, nil, &s, func() {
		w := tw()
		fmt.Fprintf(w, "helper\t%s (pid %d, uid %d), up since %s\n", s.HelperVersion, s.PID, s.UID, s.StartedAt.Local().Format("15:04:05"))
		fmt.Fprintf(w, "state dir\t%s\n", s.StateDir)
		if s.Mihomo.Reachable {
			fmt.Fprintf(w, "mihomo\trunning %s, controller %s\n", s.Mihomo.Version, s.Mihomo.Controller)
		} else {
			fmt.Fprintf(w, "mihomo\tnot reachable at %s: %s\n", s.Mihomo.Controller, s.Mihomo.Error)
		}
		fmt.Fprintf(w, "binary\t%s\n", orNone(s.Mihomo.Binary))
		fmt.Fprintf(w, "config\t%s\n", s.Mihomo.ConfigPath)
		fmt.Fprintf(w, "subscription\t%s\n", orNone(s.ActiveSubscription))
		fmt.Fprintf(w, "layers\t%d stored\n", s.Layers)
		if a := s.LastApply; a != nil {
			state := "ok"
			if a.Error != "" {
				state = "FAILED: " + a.Error
			}
			fmt.Fprintf(w, "last apply\t%s %q: %s (changed=%v written=%v reloaded=%v)\n", a.Time.Local().Format("15:04:05"), a.Reason, state, a.Changed, a.Written, a.Reloaded)
		} else {
			fmt.Fprintf(w, "last apply\tnone since start\n")
		}
		tn := fmt.Sprintf("coexist=%v, %d peers", s.Tailnet.Coexist, len(s.Tailnet.Peers))
		if s.Tailnet.Suffix != "" {
			tn += ", " + s.Tailnet.Suffix
		}
		if s.Tailnet.Error != "" {
			tn += ", last poll error: " + s.Tailnet.Error
		}
		fmt.Fprintf(w, "tailnet\t%s\n", tn)
		k := orNone(s.Kernel.Current)
		if s.Kernel.External {
			k = "external " + s.Kernel.Active
		}
		fmt.Fprintf(w, "kernel\t%s\n", k)
		w.Flush()
	})
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// ---- compose through the helper ----

func runComposeHelper(args []string) error {
	fs, cf := newFlagSet("compose", `usage: duet compose --apply [--reason TEXT] [--force]
       duet compose --preview

--apply asks the helper to compose its subscription and layers, check the
result with mihomo -t, back up the current config, write it and reload
mihomo; any failure keeps the running config. --preview only composes and
checks.

`)
	var apply, preview, force bool
	var reason string
	fs.BoolVar(&apply, "apply", false, "compose, check, write and reload through the helper")
	fs.BoolVar(&preview, "preview", false, "compose and check through the helper; write nothing")
	fs.BoolVar(&force, "force", false, "reload even if the config did not change")
	fs.StringVar(&reason, "reason", "duet compose --apply", "reason recorded in history")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) > 0 || apply == preview {
		return errors.New("compose: use exactly one of --apply or --preview (local flags like --sub don't mix with them)")
	}
	if preview {
		var r rpc.ComposeResult
		return cf.callPrint(rpc.MethodComposePreview, nil, &r, func() { printCompose(os.Stdout, r) })
	}
	var r rpc.ApplyResult
	err = cf.call(rpc.MethodComposeApply, rpc.ApplyParams{Reason: reason, Force: force}, &r)
	if cf.json && err == nil {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if err != nil {
		return err
	}
	printApply(os.Stdout, r)
	return nil
}

func printCompose(w io.Writer, r rpc.ComposeResult) {
	fmt.Fprintf(w, "layers:   %s\n", strings.Join(r.Applied, ", "))
	fmt.Fprintf(w, "hash:     %s (%d bytes)\n", shortHash(r.Hash), r.Size)
	if r.Valid {
		fmt.Fprintln(w, "mihomo -t: ok")
	} else {
		fmt.Fprintln(w, "mihomo -t: FAILED")
		for _, e := range r.ValidateErrors {
			fmt.Fprintln(w, "  ", e)
		}
	}
	fmt.Fprintf(w, "changed:  %v\n", r.Changed)
	for _, x := range r.Warnings {
		fmt.Fprintln(w, "warning: ", x)
	}
}

func printApply(w io.Writer, r rpc.ApplyResult) {
	printCompose(w, r.ComposeResult)
	fmt.Fprintf(w, "written:  %v\nreloaded: %v\n", r.Written, r.Reloaded)
	if r.HistoryID != "" {
		fmt.Fprintf(w, "history:  %s\n", r.HistoryID)
	}
	fmt.Fprintf(w, "took:     %s\n", r.Duration)
}

// ---- kernel ----

func runKernel(args []string) error {
	fs, cf := newFlagSet("kernel", `usage: duet kernel list
       duet kernel check [--channel stable|alpha]
       duet kernel install [--channel stable|alpha] [--version V]
       duet kernel rollback

`)
	var channel, version string
	fs.StringVar(&channel, "channel", "", "release channel (default: the helper's setting)")
	fs.StringVar(&version, "version", "", "install only if the newest release is this version")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errors.New("kernel: missing subcommand")
	}
	printList := func(l rpc.KernelList) {
		if l.External {
			fmt.Printf("active: %s (external binary from settings)\n", l.Active)
		} else if l.Active != "" {
			fmt.Printf("active: %s\n", l.Active)
		} else {
			fmt.Println("active: (none installed)")
		}
		w := tw()
		for _, k := range l.Installed {
			mark := ""
			if k.Current {
				mark = "current"
			} else if k.Previous {
				mark = "previous"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", k.Version, mark, k.Path)
		}
		w.Flush()
	}
	switch pos[0] {
	case "list":
		var l rpc.KernelList
		return cf.callPrint(rpc.MethodKernelList, nil, &l, func() { printList(l) })
	case "check":
		var r rpc.KernelRelease
		return cf.callPrint(rpc.MethodKernelCheck, rpc.KernelCheckParams{Channel: channel}, &r, func() {
			fmt.Printf("channel:   %s\nlatest:    %s (%s, published %s)\nasset:     %s (%d bytes)\ndigest:    %s\ninstalled: %s\n",
				r.Channel, r.Version, r.Tag, r.Published.Local().Format("2006-01-02"), r.Asset, r.Size, orNone(r.Digest), orNone(r.Current))
			if r.UpdateAvailable {
				fmt.Println("update available: duet kernel install")
			} else {
				fmt.Println("up to date")
			}
		})
	case "install":
		var l rpc.KernelList
		return cf.callPrint(rpc.MethodKernelInstall, rpc.KernelInstallParams{Channel: channel, Version: version}, &l, func() {
			printList(l)
			fmt.Println("The mihomo service keeps running the old binary until it restarts.")
		})
	case "rollback":
		var l rpc.KernelList
		return cf.callPrint(rpc.MethodKernelRollback, nil, &l, func() { printList(l) })
	}
	return fmt.Errorf("kernel: unknown subcommand %q", pos[0])
}

// ---- layers ----

func runLayer(args []string) error {
	fs, cf := newFlagSet("layer", `usage: duet layer list
       duet layer get ID
       duet layer put ID FILE|- [--name N] [--stage base|user] [--format merge|verge] [--disabled]
       duet layer rm ID
       duet layer enable|disable ID
       duet layer order ID...

Changes take effect at the next compose --apply.

`)
	var name, stage, format string
	var disabled bool
	fs.StringVar(&name, "name", "", "display name")
	fs.StringVar(&stage, "stage", "", "base (ports, DNS, TUN...) or user (default)")
	fs.StringVar(&format, "format", "", "merge (Clash Party syntax, default) or verge (Clash Verge Rev Merge)")
	fs.BoolVar(&disabled, "disabled", false, "store the layer disabled")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) == 0 {
		fs.Usage()
		return errors.New("layer: missing subcommand")
	}
	switch pos[0] {
	case "list":
		var ls []rpc.Layer
		return cf.callPrint(rpc.MethodLayerList, nil, &ls, func() {
			w := tw()
			fmt.Fprintln(w, "ID\tSTAGE\tFORMAT\tENABLED\tSIZE\tNAME")
			for _, l := range ls {
				size := fmt.Sprint(l.Size)
				if l.Managed {
					size = "managed"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\n", l.ID, l.Stage, l.Format, l.Enabled, size, l.Name)
			}
			w.Flush()
		})
	case "get":
		if err := needArgs(pos, 2, "duet layer get ID"); err != nil {
			return err
		}
		var lc rpc.LayerContent
		return cf.callPrint(rpc.MethodLayerGet, rpc.IDParams{ID: pos[1]}, &lc, func() { fmt.Print(lc.Content) })
	case "put":
		if err := needArgs(pos, 3, "duet layer put ID FILE|-"); err != nil {
			return err
		}
		content, err := readFileOrStdin(pos[2])
		if err != nil {
			return err
		}
		p := rpc.LayerPutParams{ID: pos[1], Name: name, Stage: stage, Format: format, Content: string(content)}
		if disabled {
			f := false
			p.Enabled = &f
		}
		var l rpc.Layer
		return cf.callPrint(rpc.MethodLayerPut, p, &l, func() {
			fmt.Printf("stored layer %s (%s, %s, %d bytes, enabled=%v)\n", l.ID, l.Stage, l.Format, l.Size, l.Enabled)
		})
	case "rm":
		if err := needArgs(pos, 2, "duet layer rm ID"); err != nil {
			return err
		}
		return cf.callPrint(rpc.MethodLayerDelete, rpc.IDParams{ID: pos[1]}, nil, func() { fmt.Println("removed", pos[1]) })
	case "enable", "disable":
		if err := needArgs(pos, 2, "duet layer enable|disable ID"); err != nil {
			return err
		}
		return cf.callPrint(rpc.MethodLayerSetEnabled, rpc.LayerEnableParams{ID: pos[1], Enabled: pos[0] == "enable"}, nil,
			func() { fmt.Println(pos[0]+"d", pos[1]) })
	case "order":
		return cf.callPrint(rpc.MethodLayerReorder, rpc.ReorderParams{IDs: pos[1:]}, nil, func() { fmt.Println("order:", strings.Join(pos[1:], " ")) })
	}
	return fmt.Errorf("layer: unknown subcommand %q", pos[0])
}

func readFileOrStdin(p string) ([]byte, error) {
	if p == "-" {
		return io.ReadAll(io.LimitReader(os.Stdin, rpc.MaxMessageSize))
	}
	return os.ReadFile(p)
}

// ---- subscriptions ----

func runSub(args []string) error {
	fs, cf := newFlagSet("sub", `usage: duet sub list
       duet sub put ID [--name N] [--url URL|-] [--file FILE] [--interval MINUTES]
       duet sub refresh ID
       duet sub activate ID
       duet sub rm ID

The URL is stored as a secret by the helper and is never shown again.
Use --url - to read it from stdin (keeps it out of the process list).

`)
	var name, rawURL, file string
	interval := -1
	fs.StringVar(&name, "name", "", "display name")
	fs.StringVar(&rawURL, "url", "", "subscription `URL` (http/https), or - for stdin")
	fs.StringVar(&file, "file", "", "set the content from a local `FILE` instead of downloading")
	fs.IntVar(&interval, "interval", -1, "auto-refresh every `MINUTES` (0 = manual)")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) == 0 {
		fs.Usage()
		return errors.New("sub: missing subcommand")
	}
	switch pos[0] {
	case "list":
		var ss []rpc.Subscription
		return cf.callPrint(rpc.MethodSubList, nil, &ss, func() {
			w := tw()
			fmt.Fprintln(w, "ID\tACTIVE\tURL\tEVERY\tHASH\tSIZE\tFETCHED\tERROR")
			for _, s := range ss {
				every := "manual"
				if s.IntervalMinutes > 0 {
					every = (time.Duration(s.IntervalMinutes) * time.Minute).String()
				}
				fmt.Fprintf(w, "%s\t%v\t%v\t%s\t%s\t%d\t%s\t%s\n", s.ID, s.Active, s.HasURL, every, shortHash(s.Hash), s.Size, ago(s.LastFetch), s.LastError)
			}
			w.Flush()
		})
	case "put":
		if err := needArgs(pos, 2, "duet sub put ID [flags]"); err != nil {
			return err
		}
		p := rpc.SubPutParams{ID: pos[1], Name: name}
		if rawURL == "-" {
			b, err := io.ReadAll(io.LimitReader(os.Stdin, 8192))
			if err != nil {
				return err
			}
			rawURL = strings.TrimSpace(string(b))
		}
		p.URL = rawURL
		if file != "" {
			b, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			p.Content = string(b)
		}
		if interval >= 0 {
			p.IntervalMinutes = &interval
		}
		var s rpc.Subscription
		return cf.callPrint(rpc.MethodSubPut, p, &s, func() {
			fmt.Printf("stored subscription %s (active=%v, url=%v, %d bytes)\n", s.ID, s.Active, s.HasURL, s.Size)
		})
	case "refresh":
		if err := needArgs(pos, 2, "duet sub refresh ID"); err != nil {
			return err
		}
		var r rpc.SubRefreshResult
		return cf.callPrint(rpc.MethodSubRefresh, rpc.IDParams{ID: pos[1]}, &r, func() {
			fmt.Printf("downloaded via %s, hash %s, changed=%v\n", r.Via, shortHash(r.Hash), r.Changed)
			if r.Apply != nil {
				fmt.Println("re-applied:")
				printApply(os.Stdout, *r.Apply)
			}
		})
	case "activate":
		if err := needArgs(pos, 2, "duet sub activate ID"); err != nil {
			return err
		}
		return cf.callPrint(rpc.MethodSubActivate, rpc.IDParams{ID: pos[1]}, nil, func() { fmt.Println("active:", pos[1]) })
	case "rm":
		if err := needArgs(pos, 2, "duet sub rm ID"); err != nil {
			return err
		}
		return cf.callPrint(rpc.MethodSubDelete, rpc.IDParams{ID: pos[1]}, nil, func() { fmt.Println("removed", pos[1]) })
	}
	return fmt.Errorf("sub: unknown subcommand %q", pos[0])
}

// ---- history ----

func runHistory(args []string) error {
	fs, cf := newFlagSet("history", "usage: duet history [list]\n       duet history rollback ID\n\n")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) == 0 || pos[0] == "list" {
		var hs []rpc.HistoryEntry
		return cf.callPrint(rpc.MethodHistoryList, nil, &hs, func() {
			w := tw()
			fmt.Fprintln(w, "ID\tKIND\tSIZE\tREASON")
			for _, h := range hs {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", h.ID, h.Kind, h.Size, h.Reason)
			}
			w.Flush()
		})
	}
	if pos[0] == "rollback" {
		if err := needArgs(pos, 2, "duet history rollback ID"); err != nil {
			return err
		}
		var r rpc.ApplyResult
		return cf.callPrint(rpc.MethodHistoryRollback, rpc.IDParams{ID: pos[1]}, &r, func() { printApply(os.Stdout, r) })
	}
	return fmt.Errorf("history: unknown subcommand %q", pos[0])
}

// ---- settings, tailnet, service, events ----

func runSettings(args []string) error {
	fs, cf := newFlagSet("settings", `usage: duet settings [--leak-guard G1,G2|""] [--coexist on|off] [--resolver R]
                     [--history-keep N] [--channel stable|alpha] [--auto-apply on|off]

`)
	var u rpc.SettingsUpdate
	fs.Func("leak-guard", "comma-separated proxy `GROUPS` that must never use DIRECT (\"\" clears)", func(s string) error {
		gs := []string{}
		for _, g := range strings.Split(s, ",") {
			if g = strings.TrimSpace(g); g != "" {
				gs = append(gs, g)
			}
		}
		u.LeakGuardGroups = &gs
		return nil
	})
	fs.Func("coexist", "Tailscale coexist layer `on|off`", boolFlag(&u.TailscaleCoexist))
	fs.Func("auto-apply", "re-apply after subscription/tailnet changes `on|off`", boolFlag(&u.AutoApply))
	fs.Func("resolver", "nameserver for tailnet names (empty = 100.100.100.100 on the Tailscale interface)", func(s string) error { u.TailscaleResolver = &s; return nil })
	fs.Func("history-keep", "number of history entries to keep", func(s string) error {
		var n int
		if _, err := fmt.Sscan(s, &n); err != nil {
			return err
		}
		u.HistoryKeep = &n
		return nil
	})
	fs.Func("channel", "kernel channel stable|alpha", func(s string) error { u.KernelChannel = &s; return nil })
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("settings: unexpected %q", pos)
	}
	method, params := rpc.MethodSettingsGet, any(nil)
	if u != (rpc.SettingsUpdate{}) {
		method, params = rpc.MethodSettingsUpdate, u
	}
	var s rpc.Settings
	return cf.callPrint(method, params, &s, func() {
		w := tw()
		fmt.Fprintf(w, "leak guard\t%s\n", orNone(strings.Join(s.LeakGuardGroups, ", ")))
		fmt.Fprintf(w, "tailscale coexist\t%v (resolver %s)\n", s.TailscaleCoexist, orDefault(s.TailscaleResolver, "100.100.100.100 (default)"))
		fmt.Fprintf(w, "auto apply\t%v\n", s.AutoApply)
		fmt.Fprintf(w, "history keep\t%d\n", s.HistoryKeep)
		fmt.Fprintf(w, "kernel channel\t%s\n", s.KernelChannel)
		w.Flush()
	})
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func boolFlag(dst **bool) func(string) error {
	return func(s string) error {
		var v bool
		switch strings.ToLower(s) {
		case "on", "true", "yes", "1":
			v = true
		case "off", "false", "no", "0":
		default:
			return fmt.Errorf("want on or off, got %q", s)
		}
		*dst = &v
		return nil
	}
}

func runTailnet(args []string) error {
	fs, cf := newFlagSet("tailnet", "usage: duet tailnet refresh\n\n")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 1 || pos[0] != "refresh" {
		fs.Usage()
		return errors.New("tailnet: want `refresh`")
	}
	var r rpc.TailnetRefreshResult
	return cf.callPrint(rpc.MethodTailnetRefresh, nil, &r, func() {
		fmt.Printf("peers: %s\nsuffix: %s\nchanged: %v\n", strings.Join(r.Tailnet.Peers, ", "), orNone(r.Tailnet.Suffix), r.Changed)
		if r.Apply != nil {
			fmt.Println("re-applied:")
			printApply(os.Stdout, *r.Apply)
		}
	})
}

func runService(args []string) error {
	fs, cf := newFlagSet("service", "usage: duet service status mihomo|tailscaled|helper\n\n")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 2 || pos[0] != "status" {
		fs.Usage()
		return errors.New("service: want `status NAME`")
	}
	var s rpc.ServiceStatus
	return cf.callPrint(rpc.MethodServiceStatus, rpc.ServiceParams{Name: pos[1]}, &s, func() {
		w := tw()
		fmt.Fprintf(w, "label\t%s\n", s.Label)
		fmt.Fprintf(w, "installed\t%v (%s)\n", s.Installed, s.Definition)
		fmt.Fprintf(w, "loaded\t%v\n", s.Loaded)
		fmt.Fprintf(w, "running\t%v", s.Running)
		if s.PID > 0 {
			fmt.Fprintf(w, " (pid %d)", s.PID)
		}
		fmt.Fprintln(w)
		if s.LastExitCode != nil {
			fmt.Fprintf(w, "last exit\t%d (runs %d)\n", *s.LastExitCode, s.Runs)
		}
		if s.Error != "" {
			fmt.Fprintf(w, "error\t%s\n", s.Error)
		}
		w.Flush()
	})
}

func runEvents(args []string) error {
	fs, cf := newFlagSet("events", "usage: duet events [--follow] [--after SEQ]\n\n")
	var follow bool
	var after uint64
	fs.BoolVar(&follow, "follow", false, "keep waiting for new events")
	fs.Uint64Var(&after, "after", 0, "only events after this sequence number")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	ctx := context.Background()
	c, err := rpc.Dial(ctx, nil, cf.socket)
	if err != nil {
		return fmt.Errorf("cannot reach the helper at %s: %w", cf.socket, err)
	}
	defer c.Close()
	wait := 25 * time.Second
	if !follow {
		wait = 0
	}
	for {
		r, err := c.WaitEvents(ctx, after, max(wait, time.Millisecond))
		if err != nil {
			return err
		}
		for _, e := range r.Events {
			if cf.json {
				b, _ := json.Marshal(e)
				fmt.Println(string(b))
			} else {
				fmt.Printf("%d %s %-22s %s\n", e.Seq, e.Time.Local().Format("15:04:05"), e.Type, truncate(string(e.Data), 160))
			}
		}
		after = r.Next
		if !follow {
			return nil
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- helper install --dry-run ----

func runHelper(args []string) error {
	if len(args) > 0 && args[0] == "status" {
		return runHelperStatus(args[1:])
	}
	fs := flag.NewFlagSet("helper", flag.ContinueOnError)
	var dryRun bool
	var helperBin, stateDir, socket, label string
	fs.BoolVar(&dryRun, "dry-run", false, "print the LaunchDaemon plist and the commands; change nothing")
	fs.StringVar(&helperBin, "helper-bin", "", "duet-helper `BINARY` to install (default: next to this duet binary)")
	fs.StringVar(&stateDir, "state-dir", store.DefaultDir, "helper state `DIR`")
	fs.StringVar(&socket, "socket", rpc.DefaultSocket, "socket `PATH` the helper will listen on")
	fs.StringVar(&label, "label", store.DefaultHelperLabel, "launchd `LABEL`")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: duet helper status [--json] | duet helper install --dry-run [flags]\n\n")
		fs.PrintDefaults()
	}
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 1 || pos[0] != "install" {
		fs.Usage()
		return errors.New("helper: want `install --dry-run`")
	}
	if !dryRun {
		return errors.New("helper install: only --dry-run is supported in this version; nothing was changed")
	}
	if helperBin == "" {
		helperBin = defaultHelperBin()
	}
	steps, err := planHelperInstall(helperBin, stateDir, socket, label)
	if err != nil {
		return err
	}
	fmt.Println("# duet helper install --dry-run: nothing below has been run.")
	fmt.Println("# The real install runs these steps as root (one administrator prompt).")
	service.FormatPlan(os.Stdout, steps)
	return nil
}

func defaultHelperBin() string {
	if exe, err := os.Executable(); err == nil {
		p := strings.TrimSuffix(exe, "/duet") + "/duet-helper"
		if p != exe {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "./duet-helper"
}
