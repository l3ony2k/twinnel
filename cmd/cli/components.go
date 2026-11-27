package main

// twinnel geo … and twinnel dashboard …: the GEO databases and the web
// dashboard (external UI) in mihomo's config dir, through the helper's
// geo.* and ui.* methods. `twinnel kernel geo|ui check|update` stay as
// the older spelling.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func init() {
	register("geo", "GEO databases: status | check | update | auto weekly|off", runGeo)
	register("dashboard", "web dashboard (external UI): status | check | install | update | open", runDashboard)
}

const geoUsage = `usage: twinnel geo status [--json]
       twinnel geo check [--proxy P] [--json]
       twinnel geo update [--force] [--proxy P] [--no-reload] [--json]
       twinnel geo auto weekly|off

status reads only the disk and the last check (no network). check asks
GitHub for the published files. update downloads what is missing or newer,
verifies it, swaps it in and has mihomo reload (--no-reload: at its next
start). --proxy is auto (default: mihomo's port when it runs, else direct),
direct, subscription (a temporary mihomo with the subscription's nodes) or
a proxy URL (http://, https://, socks5://).

`

const dashboardUsage = `usage: twinnel dashboard status [--json]
       twinnel dashboard check [--proxy P] [--json]
       twinnel dashboard install|update [--force] [--proxy P] [--json]
       twinnel dashboard open [--print-url] [--print-secret]

install downloads the dashboard (zashboard unless the config names another
external-ui-url) and, when the config has no external-ui, has Twinnel add
external-ui: ui and apply. open opens it in the browser with the controller
prefilled; the controller secret is never put in the URL (--print-secret
prints it, to paste into the dashboard's sign-in).

`

func runGeo(args []string) error {
	return runComponent(rpc.AccessoryGeo, geoUsage, args)
}

func runDashboard(args []string) error {
	return runComponent(rpc.AccessoryUI, dashboardUsage, args)
}

func runComponent(kind, usage string, args []string) error {
	name := "geo"
	status, check, update := rpc.MethodGeoStatus, rpc.MethodGeoCheck, rpc.MethodGeoUpdate
	if kind == rpc.AccessoryUI {
		name = "dashboard"
		status, check, update = rpc.MethodUIStatus, rpc.MethodUICheck, rpc.MethodUIUpdate
	}
	fs, cf := newFlagSet(name, usage)
	var force, noReload, printURL, printSecret bool
	var proxy string
	fs.BoolVar(&force, "force", false, "download even what looks up to date")
	fs.StringVar(&proxy, "proxy", "", "download route: auto, direct, subscription or a proxy URL")
	if kind == rpc.AccessoryGeo {
		fs.BoolVar(&noReload, "no-reload", false, "don't have mihomo reload; it reads the new files at its next start")
	} else {
		fs.BoolVar(&printURL, "print-url", false, "print the URL instead of opening the browser")
		fs.BoolVar(&printSecret, "print-secret", false, "also print the controller secret (to paste into the dashboard)")
	}
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if proxy == "auto" {
		proxy = rpc.ProxyAuto
	}
	op := ""
	if len(pos) > 0 {
		op = pos[0]
	}
	switch {
	case op == "status" && len(pos) == 1:
		var c rpc.AccessoryCheck
		return cf.callPrint(status, nil, &c, func() { printComponent(stdout, kind, c) })
	case op == "check" && len(pos) == 1:
		var c rpc.AccessoryCheck
		return cf.callPrint(check, rpc.AccessoryCheckParams{Proxy: proxy}, &c, func() { printComponent(stdout, kind, c) })
	case (op == "update" || (op == "install" && kind == rpc.AccessoryUI)) && len(pos) == 1:
		p := rpc.AccessoryUpdateParams{Force: force, Proxy: proxy, Reload: kind == rpc.AccessoryGeo && !noReload}
		stop := func() {}
		if !cf.json {
			stop = followProgress(cf.socket, kind)
		}
		var r rpc.AccessoryUpdateResult
		err := cf.callPrint(update, p, &r, func() {
			stop()
			printUpdate(stdout, kind, r)
		})
		stop()
		if err != nil && r.Error == "" && !cf.json {
			fmt.Fprintln(os.Stderr, "if GitHub can't be reached, try --proxy subscription or --proxy http://HOST:PORT")
		}
		return err
	case op == "auto" && kind == rpc.AccessoryGeo && len(pos) == 2:
		v := pos[1]
		if v != rpc.GeoAutoWeekly && v != rpc.GeoAutoOff {
			return fmt.Errorf("geo auto: want %s or %s", rpc.GeoAutoWeekly, rpc.GeoAutoOff)
		}
		var r rpc.Settings
		return cf.callPrint(rpc.MethodSettingsUpdate, rpc.SettingsUpdate{GeoAutoUpdate: &v}, &r, func() {
			fmt.Fprintf(stdout, "geo auto-update: %s\n", r.GeoAutoUpdate)
		})
	case op == "open" && kind == rpc.AccessoryUI && len(pos) == 1:
		return openDashboard(cf, printURL, printSecret)
	}
	fs.Usage()
	return errors.New(name + ": unknown or missing subcommand")
}

func printComponent(w io.Writer, kind string, c rpc.AccessoryCheck) {
	fmt.Fprintf(w, "dir:     %s\n", c.Dir)
	switch {
	case c.CheckedAt.IsZero():
		fmt.Fprintln(w, "checked: never (twinnel "+cliName(kind)+" check)")
	default:
		fmt.Fprintf(w, "checked: %s", c.CheckedAt.Local().Format("2006-01-02 15:04"))
		if c.Via != "" {
			fmt.Fprintf(w, " (via %s)", c.Via)
		}
		fmt.Fprintln(w)
	}
	if kind == rpc.AccessoryGeo {
		fmt.Fprintf(w, "auto:    %s", orDash(c.AutoUpdate))
		if !c.LastAuto.IsZero() {
			fmt.Fprintf(w, ", last %s", c.LastAuto.Local().Format("2006-01-02 15:04"))
		}
		if c.AutoError != "" {
			fmt.Fprintf(w, ", failed: %s", c.AutoError)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "served:  %v (external-ui in the config)\n", c.Configured)
	}
	t := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(t, "NAME\tINSTALLED\tDATE\tVERSION\tSOURCE\tUPDATE\tFILE")
	for _, f := range c.Files {
		date, src := "-", "-"
		if !f.ModTime.IsZero() {
			date = f.ModTime.Local().Format("2006-01-02")
		}
		if f.Installed {
			src = "other"
			if f.Source != "" {
				src = "twinnel"
			}
		}
		upd := "-"
		switch {
		case f.Error != "":
			upd = "error"
		case !f.Installed && f.Unused:
			upd = "not used by the config"
		case f.UpdateAvailable && !f.Installed:
			upd = "missing"
		case f.UpdateAvailable:
			upd = "newer"
			if !f.RemoteTime.IsZero() {
				upd += " (" + f.RemoteTime.Local().Format("2006-01-02") + ")"
			} else if f.RemoteTag != "" {
				upd += " (" + f.RemoteTag + ")"
			}
		case c.Checked:
			upd = "up to date"
		}
		fmt.Fprintf(t, "%s\t%v\t%s\t%s\t%s\t%s\t%s\n", f.Name, f.Installed, date, orDash(f.Version), src, upd, f.File)
	}
	t.Flush()
	for _, f := range c.Files {
		if f.Error != "" {
			fmt.Fprintf(w, "%s: %s\n", f.Name, f.Error)
		}
	}
}

func cliName(kind string) string {
	if kind == rpc.AccessoryUI {
		return "dashboard"
	}
	return "geo"
}

func printUpdate(w io.Writer, kind string, r rpc.AccessoryUpdateResult) {
	fmt.Fprintf(w, "updated: %s", orNone(strings.Join(r.Updated, ", ")))
	if r.Via != "" {
		fmt.Fprintf(w, " (via %s)", r.Via)
	}
	fmt.Fprintln(w)
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "up to date: %s\n", strings.Join(r.Skipped, ", "))
	}
	switch {
	case r.Configured:
		fmt.Fprintln(w, "external-ui: ui added to the config and applied")
	case r.Reloaded:
		fmt.Fprintln(w, "mihomo reloaded: the new files are in use")
	case r.ReloadNeeded:
		fmt.Fprintf(w, "mihomo reads the new files at its next reload: twinnel compose --apply --force (or restart the service)")
		if r.ReloadError != "" {
			fmt.Fprintf(w, " [%s]", r.ReloadError)
		}
		fmt.Fprintln(w)
	}
}

// followProgress prints accessory.progress events of kind to stderr
// until the returned stop is called.
func followProgress(socket, kind string) func() {
	ctx, cancel := context.WithCancel(context.Background())
	c, err := rpc.Dial(ctx, nil, socket)
	if err != nil {
		cancel()
		return func() {}
	}
	r, err := c.WaitEvents(ctx, 0, time.Millisecond) // start after what is buffered
	if err != nil {
		c.Close()
		cancel()
		return func() {}
	}
	after := r.Next
	done := make(chan struct{})
	go func() {
		defer close(done)
		last := ""
		for ctx.Err() == nil {
			r, err := c.WaitEvents(ctx, after, 2*time.Second)
			if err != nil {
				return
			}
			after = r.Next
			for _, e := range r.Events {
				if e.Type != rpc.EventAccessoryProgress {
					continue
				}
				var p rpc.AccessoryProgress
				if json.Unmarshal(e.Data, &p) != nil || p.Kind != kind {
					continue
				}
				line := p.Stage
				if p.Name != "" {
					line += " " + p.Name
				}
				if p.Stage == rpc.AccStageDownload && p.Total > 0 {
					line += fmt.Sprintf(" %d%%", min(100, p.Done*100/p.Total))
				}
				if line != last {
					fmt.Fprintln(os.Stderr, "… "+line)
					last = line
				}
			}
		}
	}()
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		cancel()
		c.Close()
		<-done
	}
}

func openDashboard(cf *clientFlags, printURL, printSecret bool) error {
	var st rpc.AccessoryCheck
	if err := cf.call(rpc.MethodUIStatus, nil, &st); err != nil {
		return err
	}
	if len(st.Files) == 0 || !st.Files[0].Installed {
		return errors.New("the dashboard is not installed: twinnel dashboard install")
	}
	if !st.Configured {
		return errors.New("the config does not serve the dashboard (no external-ui): twinnel dashboard install")
	}
	var cr rpc.ControllerCredentials
	if err := cf.call(rpc.MethodControllerCredentials, nil, &cr); err != nil {
		return err
	}
	if cr.Controller == "" {
		return errors.New("no controller address in the helper's settings")
	}
	u := mihomo.DashboardURL(cr.Controller, st.Path)
	if printSecret && cr.Secret != "" {
		fmt.Fprintf(stdout, "secret: %s\n", cr.Secret)
	}
	if printURL || os.Getenv("SSH_CONNECTION") != "" {
		fmt.Fprintln(stdout, u)
		return nil
	}
	name, args := urlOpener(runtime.GOOS, u)
	p, err := exec.LookPath(name)
	if err != nil {
		fmt.Fprintln(stdout, u)
		return nil
	}
	if err := exec.Command(p, args...).Start(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "opened %s\n", u)
	if cr.Secret != "" && !printSecret {
		fmt.Fprintln(stdout, "the dashboard asks for the controller secret: twinnel dashboard open --print-secret")
	}
	return nil
}
