package status

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// HelperState is how the app stands with the root helper.
type HelperState int

const (
	HelperUnknown      HelperState = iota // not tried yet
	HelperNotInstalled                    // no socket and no installed helper
	HelperNotRunning                      // installed (or a socket was named) but nobody answers
	HelperDenied                          // the helper refused this user
	HelperConnected
)

// Text returns the label of the state.
func (s HelperState) Text() string {
	T := i18n.Cur()
	switch s {
	case HelperNotInstalled:
		return T.HelperNotInstalled
	case HelperNotRunning:
		return T.HelperNotRunning
	case HelperDenied:
		return T.HelperDenied
	case HelperConnected:
		return T.HelperConnected
	}
	return T.HelperChecking
}

// Helper is the helper's part of a snapshot.
type Helper struct {
	State   HelperState
	Socket  string
	Err     string
	Status  *rpc.Status // last status.get, while connected
	Updated time.Time
}

// Connected reports whether calls can go to the helper.
func (h Helper) Connected() bool { return h.State == HelperConnected }

// SetHelper records the helper connection state and notifies subscribers.
// The helper does not change the overall level: it is optional.
//
// The DNS mode comes from the helper's status: the controller does not
// report it, and the config file may be the helper's to read alone
// (Windows QA round 5 N4: the app could not read it there). One path on
// every platform; unknown while the helper is not connected.
func (m *Monitor) SetHelper(h Helper) {
	m.update(func(s *Snapshot) {
		s.Helper = h
		s.Mihomo.DNSMode = ""
		if h.Status != nil {
			s.Mihomo.DNSMode = h.Status.Mihomo.DNSMode
		}
	})
}

// ErrHelperNotConnected is returned by HelperConn.Call while there is no
// connection.
// Its text is in the UI language at the time it is printed.
var ErrHelperNotConnected error = errNotConnected{}

type errNotConnected struct{}

func (errNotConnected) Error() string { return i18n.Cur().HelperNotConnectedErr }

// HelperDialer opens a client connection; rpc.Dial with the unix
// transport in the app, a fake in tests.
type HelperDialer func(ctx context.Context, socket string) (*rpc.Client, error)

// HelperConn keeps one connection to the helper: it dials (and redials
// with backoff), follows events.wait with one long-poll and refreshes
// status.get after every batch of events and at each poll timeout.
// All methods are safe for concurrent use.
type HelperConn struct {
	Socket string
	// Resolve, when set, picks the socket again before each dial (the
	// helper's socket moves when a helper from before the rename is
	// migrated).
	Resolve func() string
	// Installed reports whether a helper is installed, to tell "not
	// installed" from "not running" when the socket is missing. nil:
	// installed is assumed (a socket from TWINNEL_SOCKET, a dev run).
	Installed func() bool
	Dial      HelperDialer // default: rpc.Dial over the unix transport
	// OnChange is called (on a connection goroutine) after the state or
	// the status changed. OnEvents gets every batch of events.
	OnChange func(Helper)
	OnEvents func([]rpc.Event)

	PollTimeout time.Duration // events.wait timeout, default 25s
	MinRetry    time.Duration // default 2s
	MaxRetry    time.Duration // default 30s
	Now         func() time.Time

	mu     sync.Mutex
	client *rpc.Client
	state  Helper
	kick   chan struct{}
}

// State returns the current state.
func (h *HelperConn) State() Helper {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Call invokes a helper method, or returns ErrHelperNotConnected.
func (h *HelperConn) Call(ctx context.Context, method string, params, result any) error {
	h.mu.Lock()
	c := h.client
	h.mu.Unlock()
	if c == nil {
		return ErrHelperNotConnected
	}
	return c.Call(ctx, method, params, result)
}

// Retry makes a waiting Run dial again at once (e.g. after installing).
func (h *HelperConn) Retry() {
	h.mu.Lock()
	ch := h.kick
	h.mu.Unlock()
	if ch != nil {
		kick(ch)
	}
}

func (h *HelperConn) defaults() {
	if h.Dial == nil {
		h.Dial = func(ctx context.Context, socket string) (*rpc.Client, error) {
			return rpc.Dial(ctx, nil, socket)
		}
	}
	if h.PollTimeout == 0 {
		h.PollTimeout = 25 * time.Second
	}
	if h.MinRetry == 0 {
		h.MinRetry = 2 * time.Second
	}
	if h.MaxRetry == 0 {
		h.MaxRetry = 30 * time.Second
	}
	if h.Now == nil {
		h.Now = time.Now
	}
}

func (h *HelperConn) set(fn func(s *Helper)) {
	h.mu.Lock()
	old := h.state
	fn(&h.state)
	h.state.Socket = h.Socket
	h.state.Updated = h.Now()
	s := h.state
	h.mu.Unlock()
	if h.OnChange != nil && (old.State != s.State || old.Err != s.Err || old.Status != s.Status) {
		h.OnChange(s)
	}
}

// Run keeps the connection until ctx ends.
func (h *HelperConn) Run(ctx context.Context) {
	h.defaults()
	h.mu.Lock()
	h.kick = make(chan struct{}, 1)
	kickCh := h.kick
	h.mu.Unlock()
	wait := h.MinRetry
	for ctx.Err() == nil {
		connected := h.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			wait = h.MinRetry // it was up: retry soon
		}
		select {
		case <-ctx.Done():
			return
		case <-kickCh:
			wait = h.MinRetry
		case <-time.After(wait):
			wait = min(wait*2, h.MaxRetry)
		}
	}
}

// session dials, then follows events until the connection breaks. It
// reports whether it got connected.
func (h *HelperConn) session(ctx context.Context) bool {
	if h.Resolve != nil {
		sock := h.Resolve()
		h.mu.Lock()
		h.Socket = sock
		h.mu.Unlock()
	}
	h.mu.Lock()
	sock := h.Socket
	h.mu.Unlock()
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	c, err := h.Dial(dctx, sock)
	cancel()
	if err != nil {
		h.set(func(s *Helper) { s.State, s.Err, s.Status = h.classify(err), ShortError(err), nil })
		return false
	}
	defer c.Close()
	st, err := h.status(ctx, c)
	if err != nil {
		h.set(func(s *Helper) { s.State, s.Err, s.Status = h.classify(err), ShortError(err), nil })
		return false
	}
	// Skip what happened before we connected: the views load fresh anyway.
	// Taken before announcing the connection, so nothing after it is lost.
	var after uint64
	if r, err := c.WaitEvents(ctx, ^uint64(0)>>1, time.Millisecond); err == nil {
		after = r.Next
	}
	h.mu.Lock()
	h.client = c
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.client = nil
		h.mu.Unlock()
	}()
	h.set(func(s *Helper) { s.State, s.Err, s.Status = HelperConnected, "", st })
	for ctx.Err() == nil {
		wctx, cancel := context.WithTimeout(ctx, h.PollTimeout+10*time.Second)
		r, err := c.WaitEvents(wctx, after, h.PollTimeout)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				h.set(func(s *Helper) { s.State, s.Err, s.Status = h.classify(err), ShortError(err), nil })
			}
			return true
		}
		after = r.Next
		if len(r.Events) > 0 && h.OnEvents != nil {
			h.OnEvents(r.Events)
		}
		if st, err := h.status(ctx, c); err == nil {
			h.set(func(s *Helper) { s.Status = st })
		}
	}
	return true
}

func (h *HelperConn) status(ctx context.Context, c *rpc.Client) (*rpc.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var st rpc.Status
	if err := c.Call(ctx, rpc.MethodStatus, nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// classify maps a dial or call error to a state.
func (h *HelperConn) classify(err error) HelperState {
	var re *rpc.Error
	switch {
	case errors.As(err, &re) && re.Code == rpc.CodeUnauthorized:
		return HelperDenied
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EACCES):
		return HelperDenied
	}
	// Not installed and installed-but-down are told apart by the files
	// install puts down, not by the socket (a stale socket answers
	// "connection refused", a missing one "no such file").
	if h.Installed != nil && !h.Installed() {
		return HelperNotInstalled
	}
	return HelperNotRunning
}

// FileExists is the usual Installed probe: any of paths exists.
func FileExists(paths ...string) func() bool {
	return func() bool {
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
		return false
	}
}
