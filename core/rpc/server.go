package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// AllMethods is the allowlist. Server.Handle panics for any other name.
var AllMethods = []string{
	MethodStatus,
	MethodComposePreview, MethodComposeApply, MethodComposeStatus,
	MethodHistoryList, MethodHistoryRollback,
	MethodLayerList, MethodLayerGet, MethodLayerPut, MethodLayerDelete, MethodLayerSetEnabled, MethodLayerReorder, MethodLayerRestore,
	MethodSubList, MethodSubPut, MethodSubDelete, MethodSubActivate, MethodSubRefresh,
	MethodSettingsGet, MethodSettingsUpdate,
	MethodTailnetRefresh,
	MethodKernelList, MethodKernelCheck, MethodKernelInstall, MethodKernelRollback,
	MethodTailscaleInfo,
	MethodServiceStatus, MethodServiceStart, MethodServiceStop, MethodServiceRestart,
	MethodServiceInstall, MethodServiceUninstall, MethodServiceLog,
	MethodHealthLast, MethodHealthRun, MethodHealthEgress,
	MethodConfigGet,
	MethodConfigExport, MethodConfigImport,
	MethodGeoStatus, MethodGeoCheck, MethodGeoUpdate, MethodUIStatus, MethodUICheck, MethodUIUpdate,
	MethodTakeoverPlan, MethodTakeoverPlans, MethodTakeoverDiscard, MethodTakeoverAdopt, MethodTakeoverUndo,
	MethodControllerSecure, MethodControllerCredentials,
	MethodAgentRecord, MethodAgentActivity,
	MethodHardenPlan, MethodHardenPlans, MethodHardenApply,
	MethodHelperInfo, MethodHelperInventory, MethodHelperRestart, MethodEventsWait,
	MethodDNSStatus, MethodDNSPoint, MethodDNSRestore, MethodTailscaleInstall, MethodTailscaleLogin,
	MethodAutostartList, MethodAutostartChange,
}

// HandlerFunc serves one method. params is the raw JSON (may be empty).
// Returning an *Error picks the error code; any other error is CodeFailed.
type HandlerFunc func(ctx context.Context, peer Peer, params json.RawMessage) (any, error)

// Server dispatches JSON-RPC requests to registered handlers.
type Server struct {
	Transport Transport
	Auth      Authorizer
	Log       *slog.Logger
	// MaxInFlight bounds concurrent requests per connection (default 8).
	MaxInFlight int

	mu       sync.Mutex
	handlers map[string]HandlerFunc
	conns    map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
}

// NewServer returns a server with no handlers.
func NewServer(t Transport, auth Authorizer, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{Transport: t, Auth: auth, Log: log, handlers: map[string]HandlerFunc{}, conns: map[net.Conn]struct{}{}}
}

var allowed = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range AllMethods {
		m[n] = true
	}
	return m
}()

// Handle registers a handler. Only names from AllMethods are accepted.
func (s *Server) Handle(method string, h HandlerFunc) {
	if !allowed[method] {
		panic("rpc: method not in allowlist: " + method)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Typed registers a handler with decoded params and a typed result.
// Unknown fields in params are rejected.
func Typed[P, R any](s *Server, method string, fn func(ctx context.Context, p P) (R, error)) {
	s.Handle(method, func(ctx context.Context, _ Peer, raw json.RawMessage) (any, error) {
		var p P
		if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return nil, Errorf(CodeInvalidParams, "invalid params: %v", err)
			}
		}
		return fn(ctx, p)
	})
}

// Serve accepts connections until l is closed or Close is called.
func (s *Server) Serve(l net.Listener) error {
	for {
		c, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.Close()
			return nil
		}
		s.conns[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			s.serveConn(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
		}()
	}
}

// Close closes all connections and waits for their handlers to return.
// The listener is closed by its owner.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) serveConn(c net.Conn) {
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wmu sync.Mutex
	write := func(r Response) {
		r.JSONRPC = Version
		if r.ID == nil {
			r.ID = json.RawMessage("null")
		}
		b, err := json.Marshal(r)
		if err != nil {
			b, _ = json.Marshal(Response{JSONRPC: Version, ID: r.ID, Error: Errorf(CodeInternal, "marshal result: %v", err)})
		}
		b = append(b, '\n')
		wmu.Lock()
		defer wmu.Unlock()
		c.SetWriteDeadline(time.Now().Add(30 * time.Second))
		c.Write(b)
	}

	peer, err := s.Transport.PeerOf(c)
	if err == nil && s.Auth != nil {
		err = s.Auth.Authorize(peer)
	}
	if err != nil {
		s.Log.Warn("rpc: refused connection", "uid", peer.UID, "sid", peer.SID, "pid", peer.PID, "err", err)
		write(Response{Error: Errorf(CodeUnauthorized, "not allowed: %v", err)})
		lingerRefused(c)
		return
	}

	max := s.MaxInFlight
	if max <= 0 {
		max = 8
	}
	sem := make(chan struct{}, max)
	var reqs sync.WaitGroup
	defer reqs.Wait()
	defer cancel() // runs before reqs.Wait: long polls end when the client goes away

	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), MaxMessageSize)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			write(Response{Error: Errorf(CodeParseError, "parse error: %v", err)})
			continue
		}
		if req.JSONRPC != Version || req.Method == "" {
			write(Response{ID: req.ID, Error: Errorf(CodeInvalidRequest, "invalid request")})
			continue
		}
		s.mu.Lock()
		h := s.handlers[req.Method]
		s.mu.Unlock()
		if h == nil {
			if req.ID != nil {
				write(Response{ID: req.ID, Error: Errorf(CodeMethodNotFound, "method not found: %s", req.Method)})
			}
			continue
		}
		sem <- struct{}{}
		reqs.Add(1)
		go func(req Request) {
			defer func() { <-sem; reqs.Done() }()
			res, err := s.call(ctx, h, peer, req)
			if req.ID == nil { // notification
				return
			}
			if err != nil {
				write(Response{ID: req.ID, Error: toRPCError(err)})
				return
			}
			b, merr := json.Marshal(res)
			if merr != nil {
				write(Response{ID: req.ID, Error: Errorf(CodeInternal, "marshal result: %v", merr)})
				return
			}
			write(Response{ID: req.ID, Result: b})
		}(req)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		write(Response{Error: Errorf(CodeInvalidRequest, "read: %v", err)})
	}
}

func (s *Server) call(ctx context.Context, h HandlerFunc, peer Peer, req Request) (res any, err error) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			err = Errorf(CodeInternal, "internal error: %v", r)
			s.Log.Error("rpc: handler panic", "method", req.Method, "panic", r)
		}
		if req.Method != MethodEventsWait {
			s.Log.Info("rpc", "method", req.Method, "uid", peer.UID, "pid", peer.PID, "dur", time.Since(start).Round(time.Millisecond), "err", errString(err))
		}
	}()
	return h(ctx, peer, req.Params)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func toRPCError(err error) *Error {
	var re *Error
	if errors.As(err, &re) {
		return re
	}
	return &Error{Code: CodeFailed, Message: err.Error()}
}

// ErrNotFound wraps a not-found condition as an RPC error.
func ErrNotFound(what string, id string) *Error {
	return Errorf(CodeNotFound, "%s %q not found", what, id)
}

// ErrInvalid wraps a bad parameter as an RPC error.
func ErrInvalid(format string, args ...any) *Error {
	return Errorf(CodeInvalidParams, format, args...)
}

// refuseLinger bounds how long a refused connection stays open for its
// client's first request.
const refuseLinger = time.Second

// lingerRefused keeps a refused connection open until its client has sent
// its first request (or hung up, or refuseLinger passed), with the write
// side shut so the client reads the refusal and then EOF. Closing at once
// races the client: a request written after the close fails with EPIPE,
// and the client would report "broken pipe" instead of the refusal.
func lingerRefused(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
	c.SetReadDeadline(time.Now().Add(refuseLinger))
	r := bufio.NewReaderSize(io.LimitReader(c, MaxMessageSize), 4096)
	for {
		if _, err := r.ReadSlice('\n'); err != bufio.ErrBufferFull {
			return // the first request's end, EOF, the deadline or the limit
		}
	}
}
