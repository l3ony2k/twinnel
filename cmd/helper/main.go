// Command duet-helper is the duet daemon. Installed, it runs as root from a
// LaunchDaemon and is the only part of duet that writes system state:
// mihomo configs, kernels and service definitions. Clients (the app, the
// duet CLI) talk to it over a unix socket with JSON-RPC (core/rpc).
//
// For development it runs fine as a normal user against a temp state dir:
//
//	go run ./cmd/helper --state-dir /tmp/x --socket /tmp/x/duet.sock --foreground
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/store"
)

type options struct {
	stateDir      string
	socket        string
	foreground    bool
	group         string
	tailnetStatus string
	version       bool

	tailnetIface   string
	noHealth       bool
	healthInterval time.Duration
}

func parseFlags(args []string) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("duet-helper", flag.ContinueOnError)
	fs.StringVar(&o.stateDir, "state-dir", store.DefaultDir, "state `DIR` (settings, layers, history, kernels)")
	fs.StringVar(&o.socket, "socket", rpc.DefaultSocket, "unix socket `PATH` to listen on")
	fs.BoolVar(&o.foreground, "foreground", false, "log to stderr instead of <state-dir>/logs/helper.log")
	fs.StringVar(&o.group, "group", "admin", "`GROUP` whose members may use the helper (besides root and the helper's own user)")
	fs.StringVar(&o.tailnetStatus, "tailnet-status-file", "", "development: read tailscale status JSON from `FILE` instead of running `tailscale status --json`")
	fs.StringVar(&o.tailnetIface, "tailnet-iface", "auto", "Tailscale interface for the coexist resolver: auto (route to 100.100.100.100), none, or a `NAME`")
	fs.BoolVar(&o.noHealth, "no-health", false, "do not run the health checks (health.last/run then fail)")
	fs.DurationVar(&o.healthInterval, "health-interval", 5*time.Minute, "time between scheduled health runs")
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
	o, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "duet-helper:", err)
		os.Exit(2)
	}
	if o.version {
		fmt.Println("duet-helper", engine.HelperVersion)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, o, nil); err != nil {
		fmt.Fprintln(os.Stderr, "duet-helper:", err)
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

	// TODO(keychain): use the System keychain on macOS. Until then secrets
	// live in a 0600 file in the 0755 state dir owned by root.
	secrets := &store.FileSecrets{Path: st.Path("secrets.json")}

	e := engine.New(st, secrets)
	e.Log = log
	probe := &kernel.TailscaleProbe{Binary: settings.Tailscale.Binary}
	e.TSProbe = probe
	if o.tailnetStatus != "" {
		e.Tailnet = fileTailnet(o.tailnetStatus)
	} else {
		e.Tailnet = engine.CLITailnet{Probe: probe}
	}
	e.Services = platformServices()
	watch := setupPlatform(e, o)

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

// fileTailnet reads a saved `tailscale status --json` (development).
type fileTailnet string

func (f fileTailnet) Status(context.Context) ([]byte, error) { return os.ReadFile(string(f)) }
