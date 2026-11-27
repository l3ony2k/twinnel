package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

func init() {
	register("status", "print the mihomo + Tailscale status snapshot (read-only)", runStatus)
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the snapshot as JSON")
	controller := fs.String("controller", "", "mihomo controller host:port (default: the app's setting, else 127.0.0.1:9090)")
	timeout := fs.Duration("timeout", 6*time.Second, "how long to wait for both sides")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := status.LoadConfig(appConfigPath(), os.Getenv)
	if err != nil {
		cfg, _ = status.LoadConfig("", os.Getenv)
	}
	if *controller != "" {
		cfg.Controller = *controller
	}
	mh, err := mihomo.New(mihomo.Options{Address: cfg.Controller, Secret: cfg.Secret})
	if err != nil {
		return err
	}
	install := tailscale.DetectInstall(runtime.GOOS, func(p string) bool { _, err := os.Stat(p); return err == nil })
	mon := status.NewMonitor(status.Options{
		Mihomo:       mh,
		Tailscale:    tailscale.New(tailscale.LocalAPI(nil), false),
		Install:      install,
		PrimaryGroup: cfg.PrimaryGroup,
	})
	s := snapshotOnce(mon, *timeout)
	if *asJSON {
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Print(statusText(s))
	return nil
}

// snapshotOnce runs the monitor until both sides answered (or timeout).
func snapshotOnce(mon *status.Monitor, timeout time.Duration) status.Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ready := make(chan struct{}, 1)
	unsub := mon.Subscribe(func() {
		if mon.Snapshot().Loaded {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	defer unsub()
	mon.Run(ctx)
	select {
	case <-ready:
	case <-ctx.Done():
	}
	return mon.Snapshot()
}

func appConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return dir + "/twinnel/app.json"
}

func statusText(s status.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "twinnel: %s (%s)\n", s.Level.Text(), s.Level)
	for _, r := range s.Reasons {
		fmt.Fprintf(&b, "  - [%s] %s\n", r.Level, r.Text)
	}
	m := s.Mihomo
	b.WriteString("\nmihomo\n")
	if !m.Reachable {
		fmt.Fprintf(&b, "  unreachable: %s\n", m.Err)
	} else {
		fmt.Fprintf(&b, "  version     %s\n", m.Version)
		fmt.Fprintf(&b, "  mode        %s\n", m.Config.Mode)
		tun := "off"
		if m.Config.Tun.Enable {
			tun = "on · " + m.Config.Tun.Device + " · " + m.Config.Tun.Stack
		}
		fmt.Fprintf(&b, "  tun         %s\n", tun)
		if p := m.Primary; p != nil {
			fmt.Fprintf(&b, "  %-11s %s (%s)\n", p.Name, strings.Join(p.Chain, " → "), status.DelayText(p.Delay, p.Tested))
		}
		fmt.Fprintf(&b, "  connections %d\n", m.Connections)
		if !m.Traffic.Time.IsZero() {
			fmt.Fprintf(&b, "  traffic     %s\n", status.TrayTitle(m.Traffic))
		}
	}
	ts := s.Tailscale
	b.WriteString("\nTailscale\n")
	if !ts.Reachable {
		fmt.Fprintf(&b, "  unreachable: %s (install: %s)\n", ts.Err, ts.Install.Kind)
		return b.String()
	}
	st := ts.State
	fmt.Fprintf(&b, "  state       %s\n", st.BackendState)
	fmt.Fprintf(&b, "  self        %s %s\n", st.Self.Name, st.Self.IPv4())
	fmt.Fprintf(&b, "  peers       %d / %d online\n", st.OnlinePeers(), len(st.Peers))
	exit := "none"
	if st.ExitNode != nil {
		exit = st.ExitNode.Name
	}
	fmt.Fprintf(&b, "  exit node   %s\n", exit)
	if st.KeyExpiry != nil {
		fmt.Fprintf(&b, "  key expiry  %s (%s)\n", st.KeyExpiry.Local().Format("2006-01-02"), status.Until(s.Time, *st.KeyExpiry))
	}
	fmt.Fprintf(&b, "  install     %s\n", ts.Install.Kind)
	return b.String()
}
