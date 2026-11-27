// Command twinnel-helper is the Twinnel daemon. Installed, it runs as root from a
// LaunchDaemon and is the only part of Twinnel that writes system state:
// mihomo configs, kernels and service definitions. Clients (the app, the
// Twinnel CLI) talk to it over a unix socket with JSON-RPC (core/rpc).
//
// For development it runs fine as a normal user against a temp state dir:
//
//	go run ./cmd/helper --state-dir /tmp/x --socket /tmp/x/twinnel.sock --foreground
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

type options struct {
	stateDir      string
	socket        string
	foreground    bool
	group         string
	tailnetStatus string
	version       bool

	secrets        string
	keychain       string
	tailnetIface   string
	noHealth       bool
	noSystemDNS    bool
	healthInterval time.Duration

	// takeover discovery overrides (development against a copied setup)
	takeoverServices string
	takeoverConfig   string
	takeoverMihomo   string
	takeoverHome     string
}

func parseFlags(args []string) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("twinnel-helper", flag.ContinueOnError)
	fs.StringVar(&o.stateDir, "state-dir", store.DefaultDir, "state `DIR` (settings, layers, history, kernels)")
	fs.StringVar(&o.socket, "socket", rpc.DefaultSocket, "unix socket `PATH` to listen on")
	fs.BoolVar(&o.foreground, "foreground", false, "log to stderr instead of <state-dir>/logs/helper.log")
	fs.StringVar(&o.group, "group", "admin", "`GROUP` whose members may use the helper (besides root and the helper's own user)")
	fs.StringVar(&o.tailnetStatus, "tailnet-status-file", "", "development: read tailscale status JSON from `FILE` instead of running 'tailscale status --json' (peers for the coexist layer, takeover probe and health checks)")
	fs.StringVar(&o.secrets, "secrets", "auto", "where secrets live: keychain, file (<state-dir>/secrets.json, development) or auto (keychain when running as root on macOS)")
	fs.StringVar(&o.keychain, "keychain", "", "keychain `FILE` for --secrets keychain (default the System keychain)")
	fs.StringVar(&o.tailnetIface, "tailnet-iface", "auto", "Tailscale interface for the coexist resolver: auto (route to 100.100.100.100), none, or a `NAME`")
	fs.BoolVar(&o.noHealth, "no-health", false, "do not run the health checks (health.last/run then fail)")
	fs.BoolVar(&o.noSystemDNS, "no-system-dns", false, "development: do not read or set the system DNS (dns.* report it as unsupported)")
	fs.DurationVar(&o.healthInterval, "health-interval", 5*time.Minute, "time between scheduled health runs")
	fs.StringVar(&o.takeoverServices, "takeover-services-dir", "", "development: look for launchd plists only in `DIR` (takeover.plan/adopt)")
	fs.StringVar(&o.takeoverConfig, "takeover-config", "", "development: live mihomo config `FILE` for takeover discovery")
	fs.StringVar(&o.takeoverMihomo, "takeover-mihomo", "", "development: mihomo `BIN` for takeover discovery")
	fs.StringVar(&o.takeoverHome, "takeover-home", "", "home `DIR` of the user whose setup is taken over (default: the console user's)")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	return o, nil
}

func main() {
	if n := legacy.EnvNotice(legacy.ApplyEnvAliases()); n != "" {
		fmt.Fprintln(os.Stderr, "twinnel-helper:", n)
	}
	o, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "twinnel-helper:", err)
		os.Exit(2)
	}
	if o.version {
		fmt.Println("twinnel-helper", engine.HelperVersion)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, o, nil); err != nil {
		fmt.Fprintln(os.Stderr, "twinnel-helper:", err)
		os.Exit(1)
	}
}

// run starts the helper and blocks until ctx ends. ready, if not nil, is
// called with the engine once the socket is listening (used by tests).
func run(ctx context.Context, o *options, ready func(*engine.Engine)) error {
	st, err := store.Open(o.stateDir)
	if err != nil {
		return err
	}
	log, closeLog, err := newLogger(o, st)
	if err != nil {
		return err
	}
	defer closeLog()

	settings, err := st.Settings()
	if err != nil {
		return err
	}
	if _, err := os.Stat(st.Path("settings.json")); errors.Is(err, os.ErrNotExist) {
		// Write the defaults so they can be inspected and edited.
		if err := st.SaveSettings(settings); err != nil {
			return err
		}
	}

	secrets, err := openSecrets(o, st, log)
	if err != nil {
		return err
	}

	e := engine.New(st, secrets)
	e.Log = log
	probe := &kernel.TailscaleProbe{Binary: settings.Tailscale.Binary, Managed: st.Path("tailscale", "current", "tailscale")}
	e.TSProbe = probe
	if o.tailnetStatus != "" {
		e.Tailnet = fileTailnet(o.tailnetStatus)
	} else {
		e.Tailnet = engine.CLITailnet{Probe: probe}
	}
	e.Services = platformServices()
	// launchctl and plist writes only as root; a development helper reports
	// the plans as dry runs.
	e.ServiceRunner = &engine.ServiceRunner{DryRun: true}
	if os.Geteuid() == 0 {
		e.ServiceRunner = &engine.ServiceRunner{Exec: service.ExecExecutor{}, Files: service.OSFiles{}}
	}
	watch := setupPlatform(e, o)
	e.TakeoverDiscover.ConfigPath = o.takeoverConfig
	e.TakeoverDiscover.MihomoBin = o.takeoverMihomo

	tr := rpc.NewUnixTransport()
	auth, err := rpc.NewGroupAuthorizer(o.group)
	if err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		tr.GID = auth.GID
	}
	srv := rpc.NewServer(tr, auth, log)
	e.Register(srv)

	l, err := tr.Listen(o.socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", o.socket, err)
	}
	log.Info("helper started", "version", engine.HelperVersion, "pid", os.Getpid(), "uid", os.Getuid(),
		"state_dir", st.Dir, "socket", o.socket, "group", o.group, "config", settings.Mihomo.ConfigFile)
	e.Bus.Publish(rpc.EventHelperStarted, map[string]any{"version": engine.HelperVersion, "pid": os.Getpid()})

	// helper.restart: end like on SIGTERM; launchd (KeepAlive) or systemd
	// starts the helper again.
	ctx, restart := context.WithCancel(ctx)
	defer restart()
	e.Restart = restart
	e.Socket = o.socket

	loopCtx, cancelLoops := context.WithCancel(ctx)
	loopsDone := make(chan struct{})
	go func() { e.Run(loopCtx); close(loopsDone) }()
	go watch(loopCtx)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(l) }()
	if ready != nil {
		ready(e)
	}

	select {
	case <-ctx.Done():
	case err = <-serveErr:
	}
	log.Info("helper stopping")
	e.Bus.Publish(rpc.EventHelperStopping, nil)
	cancelLoops()
	l.Close()
	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Warn("timed out waiting for requests to finish")
	}
	<-loopsDone
	return err
}

func newLogger(o *options, st *store.Store) (*slog.Logger, func(), error) {
	var w io.Writer = os.Stderr
	closeFn := func() {}
	if !o.foreground {
		dir := st.Path("logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		f, err := os.OpenFile(filepath.Join(dir, "helper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		w, closeFn = f, func() { f.Close() }
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})), closeFn, nil
}

// openSecrets picks the secrets backend. With the keychain, secrets left in
// <state-dir>/secrets.json (a helper that ran with --secrets file before)
// are moved into it and the file is deleted.
func openSecrets(o *options, st *store.Store, log *slog.Logger) (store.Secrets, error) {
	file := &store.FileSecrets{Path: st.Path("secrets.json")}
	kind := o.secrets
	if kind == "" || kind == "auto" {
		kind = "file"
		if hasKeychain && os.Geteuid() == 0 {
			kind = "keychain"
		}
	}
	switch kind {
	case "file":
		log.Info("secrets in file", "path", file.Path)
		return file, nil
	case "keychain":
		kc, err := newKeychain(o.keychain)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(file.Path); err == nil {
			moved, err := store.MigrateSecrets(file, kc)
			if err != nil {
				return nil, fmt.Errorf("move secrets.json into the keychain: %w", err)
			}
			log.Info("moved secrets into the keychain", "count", len(moved))
		}
		log.Info("secrets in keychain", "keychain", orDefault(o.keychain, "System"))
		return kc, nil
	}
	return nil, fmt.Errorf("unknown --secrets %q (keychain, file, auto)", o.secrets)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// devTailnetStatus is the tailnet source of the health checks when the
// helper runs with --tailnet-status-file (development), so a rehearsal never
// reads the live tailnet; nil otherwise (the checks run the tailscale CLI).
func devTailnetStatus(e *engine.Engine, o *options) func(context.Context) ([]byte, error) {
	if o == nil || o.tailnetStatus == "" || e.Tailnet == nil {
		return nil
	}
	return e.Tailnet.Status
}

// fileTailnet reads a saved `tailscale status --json` (development).
type fileTailnet string

func (f fileTailnet) Status(context.Context) ([]byte, error) { return os.ReadFile(string(f)) }

// egressProbe runs the egress test alone over a fresh Env with the test
// on (health.egress).
func egressProbe(newEnv func() health.Env) func(context.Context) health.CheckReport {
	return func(ctx context.Context) health.CheckReport {
		r := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 30 * time.Second, Only: []string{health.LeakEgress{}.ID()}}
		rep := r.Run(ctx, newEnv())
		if len(rep.Checks) == 0 {
			return health.CheckReport{ID: health.LeakEgress{}.ID(), Result: health.Result{Status: health.StatusSkip}}
		}
		return rep.Checks[0]
	}
}
