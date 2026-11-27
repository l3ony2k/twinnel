package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// SocketFromEnv returns $TWINNEL_SOCKET, else DefaultSocket, else (while
// only a helper from before the rename runs) legacy.Socket.
func SocketFromEnv() string {
	if s := os.Getenv("TWINNEL_SOCKET"); s != "" {
		return s
	}
	return pickSocket(DefaultSocket, legacy.Socket, socketExists)
}

// pickSocket: def unless only old exists.
func pickSocket(def, old string, exists func(string) bool) string {
	if !exists(def) && exists(old) {
		return old
	}
	return def
}

func socketExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Client is a JSON-RPC client over one connection. Calls may run
// concurrently; responses are matched by id.
type Client struct {
	conn net.Conn

	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan Response
	err     error // set when the read loop ends
	done    chan struct{}
}

// Dial connects to the helper at addr.
func Dial(ctx context.Context, t Transport, addr string) (*Client, error) {
	if t == nil {
		t = DefaultTransport()
	}
	c, err := t.Dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	return NewClient(c), nil
}

// NewClient wraps an established connection.
func NewClient(c net.Conn) *Client {
	cl := &Client{conn: c, pending: map[string]chan Response{}, done: make(chan struct{})}
	go cl.readLoop()
	return cl
}

// Close closes the connection; pending calls fail.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) readLoop() {
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 64<<10), MaxMessageSize)
	var err error
	for sc.Scan() {
		var r Response
		if e := json.Unmarshal(sc.Bytes(), &r); e != nil {
			continue
		}
		id := string(r.ID)
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- r
			continue
		}
		if r.Error != nil && (id == "null" || id == "") {
			// A connection-level error (e.g. unauthorized): fail everything.
			err = r.Error
			break
		}
	}
	if err == nil {
		err = sc.Err()
	}
	if err == nil {
		err = errors.New("connection closed by helper")
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// Call invokes method with params and decodes the result into result
// (which may be nil).
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := strconv.FormatInt(c.nextID, 10)
	ch := make(chan Response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := Request{JSONRPC: Version, ID: json.RawMessage(id), Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			c.forget(id)
			return err
		}
		req.Params = b
	}
	b, err := json.Marshal(req)
	if err != nil {
		c.forget(id)
		return err
	}
	b = append(b, '\n')
	c.wmu.Lock()
	if dl, ok := ctx.Deadline(); ok {
		c.conn.SetWriteDeadline(dl)
	} else {
		c.conn.SetWriteDeadline(time.Time{})
	}
	_, err = c.conn.Write(b)
	c.wmu.Unlock()
	if err != nil {
		c.forget(id)
		return c.writeErr(ctx, err)
	}

	select {
	case r, ok := <-ch:
		if !ok {
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			return err
		}
		if r.Error != nil {
			return r.Error
		}
		if result != nil && len(r.Result) > 0 {
			if err := json.Unmarshal(r.Result, result); err != nil {
				return fmt.Errorf("%s: decode result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	}
}

// writeErrGrace is how long a failed write waits for the helper's reason.
const writeErrGrace = time.Second

// writeErr is what a call whose request could not be written returns: the
// helper's own connection-level error when it sent one before closing
// (unauthorized: the helper refuses a peer as soon as it connects, and may
// close before the request is written, EPIPE), else the write's error.
func (c *Client) writeErr(ctx context.Context, err error) error {
	t := time.NewTimer(writeErrGrace)
	defer t.Stop()
	select {
	case <-c.done:
	case <-t.C:
		return err
	case <-ctx.Done():
		return err
	}
	c.mu.Lock()
	rerr := c.err
	c.mu.Unlock()
	var re *Error
	if errors.As(rerr, &re) {
		return rerr
	}
	return err
}

func (c *Client) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// WaitEvents long-polls for events after `after`.
func (c *Client) WaitEvents(ctx context.Context, after uint64, timeout time.Duration) (*EventsWaitResult, error) {
	var r EventsWaitResult
	err := c.Call(ctx, MethodEventsWait, EventsWaitParams{After: after, TimeoutMS: int(timeout / time.Millisecond)}, &r)
	return &r, err
}
