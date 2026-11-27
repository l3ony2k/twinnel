package status

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/mihomo"
	"github.com/l3ony2k/duet/core/tailscale"
)

// MihomoSource is what the monitor reads from mihomo; *mihomo.Client
// provides it.
type MihomoSource interface {
	Version(ctx context.Context) (mihomo.Version, error)
	Configs(ctx context.Context) (mihomo.Config, error)
	Proxies(ctx context.Context) (map[string]mihomo.Proxy, error)
	Connections(ctx context.Context) (mihomo.Connections, error)
	StreamTraffic(ctx context.Context, fn func(mihomo.Traffic)) error
	StreamLogs(ctx context.Context, level string, fn func(mihomo.LogEntry)) error
	Delay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error)
}

// TailscaleSource is what the monitor reads from Tailscale;
// *tailscale.Client provides it.
type TailscaleSource interface {
	State(ctx context.Context) (tailscale.State, error)
	Watch(ctx context.Context, onChange func()) error
}

// Options configure a Monitor.
type Options struct {
	Mihomo        MihomoSource
	Tailscale     TailscaleSource // nil: Tailscale is not shown as reachable
	Install       tailscale.Install
	Health        HealthSource // nil: no health checks
	PrimaryGroup  string
	TestURL       string
	WritesAllowed bool
	// ExitNodeAccepted is the exit node the user accepted (Config); see
	// SetExitNodeAccepted.
	ExitNodeAccepted string

	// HealthInterval is how often the health checks run in the background
	// (they also run at start and on demand, RunHealth). Default 10 min;
	// negative: only at start and on demand.
	HealthInterval time.Duration

	// Processes lists the running processes, for mihomo's and
	// tailscaled's memory, CPU and uptime; nil: not shown.
	Processes ProcessSource
	// MihomoFile reads the mihomo config file, for what the controller
	// does not report (the DNS mode); nil: not shown.
	MihomoFile func(ctx context.Context) (*health.MihomoConfig, error)

	MihomoPoll    time.Duration // default 3s
	ProcessPoll   time.Duration // default 5s
	DNSPoll       time.Duration // default 1 min
	TailscalePoll time.Duration // default 10s; the IPN bus triggers the rest
	HistoryLen    int           // traffic samples kept, default 120
	LogLen        int           // log lines kept, default 1000
	Now           func() time.Time
}

// Monitor keeps a Snapshot fresh and tells subscribers when it changes.
// All methods are safe for concurrent use.
type Monitor struct {
	o Options

	mu      sync.Mutex
	snap    Snapshot
	mDone   bool // mihomo answered or failed once
	tDone   bool
	history []TrafficSample
	logs    []mihomo.LogEntry
	logGen  int // bumped by ClearLogs
	subs    map[int]func()
	nextSub int
	d       delayState // delay tests (delay.go)

	mKick  chan struct{}
	tsKick chan struct{}

	rawHealth *health.Report // last report as run; snap.Health.Report has the accepted causes applied

	logMu     sync.Mutex
	logCancel context.CancelFunc
	logLevel  string
	ctx       context.Context
}

// NewMonitor returns a monitor; call Run to start it.
func NewMonitor(o Options) *Monitor {
	if o.MihomoPoll == 0 {
		o.MihomoPoll = 3 * time.Second
	}
	if o.TailscalePoll == 0 {
		o.TailscalePoll = 10 * time.Second
	}
	if o.ProcessPoll == 0 {
		o.ProcessPoll = 5 * time.Second
	}
	if o.DNSPoll == 0 {
		o.DNSPoll = time.Minute
	}
	if o.HealthInterval == 0 {
		o.HealthInterval = 10 * time.Minute
	}
	if o.HistoryLen == 0 {
		o.HistoryLen = 120
	}
	if o.LogLen == 0 {
		o.LogLen = 1000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PrimaryGroup == "" {
		o.PrimaryGroup = DefaultPrimaryGroup
	}
	m := &Monitor{
		o:      o,
		subs:   map[int]func(){},
		d:      newDelayState(),
		mKick:  make(chan struct{}, 1),
		tsKick: make(chan struct{}, 1),
	}
	m.snap.WritesAllowed = o.WritesAllowed
	m.snap.AcceptedExitNode = o.ExitNodeAccepted
	m.snap.Tailscale.Install = o.Install
	m.snap.Time = o.Now()
	Evaluate(&m.snap)
	return m
}

// Run starts the loops and returns at once; they stop when ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	m.logMu.Lock()
	m.ctx = ctx
	m.logMu.Unlock()
	go m.mihomoLoop(ctx)
	go m.trafficLoop(ctx)
	if m.o.Processes != nil {
		go m.processLoop(ctx)
	}
	if m.o.MihomoFile != nil {
		go m.dnsLoop(ctx)
	}
	if m.o.Health != nil {
		go m.healthLoop(ctx)
	}
	if m.o.Tailscale != nil {
		go m.tailscaleLoop(ctx)
		go m.tailscaleWatch(ctx)
	} else {
		m.update(func(s *Snapshot) { m.tDone = true })
	}
}

// Snapshot returns the current snapshot. Its slices must not be modified.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snap
}

// TrafficHistory returns the recent traffic samples, oldest first.
func (m *Monitor) TrafficHistory() []TrafficSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]TrafficSample(nil), m.history...)
}

// Logs returns the recent log lines, oldest first.
func (m *Monitor) Logs() []mihomo.LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mihomo.LogEntry(nil), m.logs...)
}

// ClearLogs drops the kept log lines.
func (m *Monitor) ClearLogs() {
	m.mu.Lock()
	m.logs = nil
	m.logGen++
	m.mu.Unlock()
	m.notify()
}

// Subscribe calls fn (on a monitor goroutine) after every change. Read the
// new state with Snapshot. The returned func unsubscribes.
func (m *Monitor) Subscribe(fn func()) func() {
	m.mu.Lock()
	id := m.nextSub
	m.nextSub++
	m.subs[id] = fn
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.subs, id)
		m.mu.Unlock()
	}
}

// Refresh asks for an immediate poll of both sides.
func (m *Monitor) Refresh() {
	kick(m.mKick)
	kick(m.tsKick)
}

func kick(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (m *Monitor) notify() {
	m.mu.Lock()
	fns := make([]func(), 0, len(m.subs))
	for _, fn := range m.subs {
		fns = append(fns, fn)
	}
	m.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// update changes the snapshot under the lock, re-evaluates it and notifies.
func (m *Monitor) update(fn func(s *Snapshot)) {
	m.mu.Lock()
	fn(&m.snap)
	if m.rawHealth != nil {
		m.snap.Health.Report = health.Accept(m.rawHealth, m.snap.acceptedCauses()...)
	}
	m.snap.Time = m.o.Now()
	m.snap.Loaded = m.mDone && m.tDone
	Evaluate(&m.snap)
	m.mu.Unlock()
	m.notify()
}

// --- mihomo ---

func (m *Monitor) mihomoLoop(ctx context.Context) {
	t := time.NewTicker(m.o.MihomoPoll)
	defer t.Stop()
	needVersion := true
	for {
		needVersion = m.pollMihomo(ctx, needVersion)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.mKick:
		}
	}
}

// pollMihomo reads configs and proxies; it returns whether the version
// must be read again next time (after a failure: the core may restart).
func (m *Monitor) pollMihomo(ctx context.Context, needVersion bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ver string
	if needVersion {
		v, err := m.o.Mihomo.Version(ctx)
		if err != nil {
			m.mihomoFailed(err)
			return true
		}
		ver = v.Version
	}
	cfg, err := m.o.Mihomo.Configs(ctx)
	if err != nil {
		m.mihomoFailed(err)
		return true
	}
	all, err := m.o.Mihomo.Proxies(ctx)
	if err != nil {
		m.mihomoFailed(err)
		return true
	}
	conns := -1
	var mem int64
	if c, err := m.o.Mihomo.Connections(ctx); err == nil {
		conns, mem = len(c.Connections), c.Memory
	}
	m.update(func(s *Snapshot) {
		m.d.proxies = all
		groups := buildGroups(all, m.d.results)
		m.mDone = true
		mh := &s.Mihomo
		mh.Reachable, mh.Unauthorized, mh.Err = true, false, ""
		if ver != "" {
			mh.Version = ver
		}
		mh.Config = cfg
		mh.Groups = groups
		mh.Primary = PickPrimary(groups, m.o.PrimaryGroup)
		if conns >= 0 {
			mh.Connections, mh.Memory = conns, mem
		}
		mh.Updated = m.o.Now()
	})
	return false
}

func (m *Monitor) mihomoFailed(err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.update(func(s *Snapshot) {
		m.mDone = true
		mh := &s.Mihomo
		mh.Reachable = false
		mh.Unauthorized = mihomo.IsUnauthorized(err)
		mh.Err = ShortError(err)
		mh.Traffic = TrafficSample{}
		mh.Updated = m.o.Now()
	})
}

func (m *Monitor) trafficLoop(ctx context.Context) {
	b := backoff{}
	for ctx.Err() == nil {
		got := false
		_ = m.o.Mihomo.StreamTraffic(ctx, func(t mihomo.Traffic) {
			got = true
			s := TrafficSample{Traffic: t, Time: m.o.Now()}
			m.mu.Lock()
			m.history = append(m.history, s)
			if over := len(m.history) - m.o.HistoryLen; over > 0 {
				m.history = append(m.history[:0:0], m.history[over:]...)
			}
			m.mu.Unlock()
			m.update(func(sn *Snapshot) { sn.Mihomo.Traffic = s })
		})
		if ctx.Err() != nil {
			return
		}
		if got {
			b.reset()
		}
		m.update(func(sn *Snapshot) { sn.Mihomo.Traffic = TrafficSample{} })
		kick(m.mKick)
		if !sleep(ctx, b.next()) {
			return
		}
	}
}

// SetLogLevel starts streaming logs at level ("debug", "info", "warning",
// "error"), or stops it for "". Lines are kept until ClearLogs.
func (m *Monitor) SetLogLevel(level string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	if level == m.logLevel && (level == "") == (m.logCancel == nil) {
		return
	}
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	m.logLevel = level
	if level == "" || m.ctx == nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.logCancel = cancel
	go m.logLoop(ctx, level)
}

// LogLevel returns the level logs stream at, "" when stopped.
func (m *Monitor) LogLevel() string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	return m.logLevel
}

func (m *Monitor) logLoop(ctx context.Context, level string) {
	b := backoff{}
	for ctx.Err() == nil {
		_ = m.o.Mihomo.StreamLogs(ctx, level, func(e mihomo.LogEntry) {
			b.reset()
			m.mu.Lock()
			m.logs = append(m.logs, e)
			if over := len(m.logs) - m.o.LogLen; over > 0 {
				m.logs = append(m.logs[:0:0], m.logs[over:]...)
			}
			m.mu.Unlock()
			m.notify()
		})
		if !sleep(ctx, b.next()) {
			return
		}
	}
}

// --- tailscale ---

func (m *Monitor) tailscaleLoop(ctx context.Context) {
	t := time.NewTicker(m.o.TailscalePoll)
	defer t.Stop()
	for {
		m.pollTailscale(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.tsKick:
		}
		// Bus notifications come in bursts: read at most once a second.
		if !sleep(ctx, time.Second) {
			return
		}
	}
}

func (m *Monitor) pollTailscale(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := m.o.Tailscale.State(ctx)
	if errors.Is(err, context.Canceled) {
		return
	}
	m.update(func(s *Snapshot) {
		m.tDone = true
		ts := &s.Tailscale
		ts.Updated = m.o.Now()
		if err != nil {
			ts.Reachable, ts.Err = false, ShortError(err)
			return
		}
		ts.Reachable, ts.Err, ts.State = true, "", st
	})
}

func (m *Monitor) tailscaleWatch(ctx context.Context) {
	b := backoff{}
	for ctx.Err() == nil {
		_ = m.o.Tailscale.Watch(ctx, func() {
			b.reset()
			kick(m.tsKick)
		})
		kick(m.tsKick)
		if !sleep(ctx, b.next()) {
			return
		}
	}
}

// --- helpers ---

type backoff struct{ d time.Duration }

func (b *backoff) reset() { b.d = 0 }
func (b *backoff) next() time.Duration {
	switch {
	case b.d == 0:
		b.d = time.Second
	case b.d < 15*time.Second:
		b.d *= 2
	}
	return b.d
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ShortError drops the request URL and dial noise from network errors:
// "connection refused" instead of `Get "http://…": dial tcp …: connect:
// connection refused`.
func ShortError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ENOENT):
		return "no such socket"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission denied"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}
