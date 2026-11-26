// Package mihomo is a client of mihomo's external controller (REST API).
//
// It talks only to the documented HTTP interface; it never imports mihomo
// itself. Streams (/traffic, /logs, /connections) use mihomo's plain HTTP
// streaming mode: without a WebSocket upgrade the controller writes one JSON
// value per line and flushes, so no WebSocket library is needed.
//
// Read methods are always allowed. Write methods (select a node, change the
// mode, reload, flush caches) return ErrWritesDisabled unless the client was
// built with AllowWrites.
package mihomo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultController is the controller address mihomo examples use.
const DefaultController = "127.0.0.1:9090"

// DefaultTestURL is used for delay tests when none is given.
const DefaultTestURL = "https://www.gstatic.com/generate_204"

// ErrWritesDisabled is returned by write methods of a read-only client.
var ErrWritesDisabled = errors.New("mihomo: write operations are disabled")

// Client talks to one external controller.
type Client struct {
	base        *url.URL
	secret      string
	allowWrites bool
	http        *http.Client // requests with a timeout
	stream      *http.Client // long-lived streams, no timeout
}

// Options configure a Client.
type Options struct {
	// Address is "host:port" or a full "http(s)://host:port" URL.
	Address string
	// Secret is the controller's bearer token, empty for none.
	Secret string
	// AllowWrites enables write methods.
	AllowWrites bool
	// Timeout bounds ordinary requests; 0 means 5s.
	Timeout time.Duration
	// Transport is used for all requests; nil means http.DefaultTransport.
	Transport http.RoundTripper
}

// New returns a client for the controller at opts.Address.
func New(opts Options) (*Client, error) {
	addr := strings.TrimSpace(opts.Address)
	if addr == "" {
		addr = DefaultController
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("mihomo: bad controller address %q: %w", opts.Address, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("mihomo: bad controller address %q", opts.Address)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	tr := opts.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	return &Client{
		base:        u,
		secret:      opts.Secret,
		allowWrites: opts.AllowWrites,
		http:        &http.Client{Timeout: timeout, Transport: tr},
		stream:      &http.Client{Transport: tr},
	}, nil
}

// WritesAllowed reports whether write methods are enabled.
func (c *Client) WritesAllowed() bool { return c.allowWrites }

// BaseURL returns the controller URL.
func (c *Client) BaseURL() string { return c.base.String() }

// APIError is a non-2xx answer of the controller.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("mihomo: HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("mihomo: HTTP %d", e.Status)
}

// IsUnauthorized reports whether err is a 401 from the controller (wrong
// or missing secret).
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

// endpoint joins the base URL and an escaped path (see seg).
func (c *Client) endpoint(rawPath string, q url.Values) string {
	s := strings.TrimSuffix(c.base.String(), "/") + rawPath
	if q != nil {
		s += "?" + q.Encode()
	}
	return s
}

// seg escapes one path segment. Proxy names contain spaces, emoji and even
// slashes; mihomo unescapes the segment again.
func seg(name string) string { return url.PathEscape(name) }

func (c *Client) newRequest(ctx context.Context, method, path string, q url.Values, body any) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, q), rd)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func checkResponse(resp *http.Response) error {
	if resp.StatusCode/100 == 2 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var m struct {
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &m) == nil && m.Message != "" {
		msg = m.Message
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}

// do sends a request and decodes a JSON answer into out (when non-nil).
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	req, err := c.newRequest(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := checkResponse(resp); err != nil {
		return err
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) write(ctx context.Context, method, path string, q url.Values, body any) error {
	if !c.allowWrites {
		return ErrWritesDisabled
	}
	return c.do(ctx, method, path, q, body, nil)
}

// stream GETs path and calls fn with every JSON line until ctx ends, the
// server closes the stream, or fn returns an error.
func (c *Client) streamLines(ctx context.Context, path string, q url.Values, fn func([]byte) error) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	resp, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := checkResponse(resp); err != nil {
		return err
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return io.EOF
}
