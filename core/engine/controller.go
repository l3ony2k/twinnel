package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Controller is the part of the mihomo REST API the helper needs. The app
// agent's core/mihomo client can implement it later; this package keeps a
// small HTTP implementation so it does not depend on that work.
type Controller interface {
	// Version returns the running kernel version (GET /version).
	Version(ctx context.Context) (string, error)
	// Reload makes mihomo load the config file at path
	// (PUT /configs?force=true).
	Reload(ctx context.Context, path string) error
}

// GentleReloader is implemented by controllers that can reload without
// force. mihomo applies inbound port/allow-lan/bind-address changes only
// with force=true; everything else (rules, DNS, TUN, proxies) is applied
// either way. The engine sends force=false when those inbound settings are
// unchanged (see ReloadImpact), so the reload does strictly less.
type GentleReloader interface {
	ReloadWith(ctx context.Context, path string, force bool) error
}

// ErrUnauthorized: the controller refused the secret (HTTP 401).
var ErrUnauthorized = errors.New("controller refused the secret (401)")

// reloadWith reloads with force when the controller can't do without.
func reloadWith(ctx context.Context, c Controller, path string, force bool) error {
	if g, ok := c.(GentleReloader); ok {
		return g.ReloadWith(ctx, path, force)
	}
	return c.Reload(ctx, path)
}

// HTTPController talks to mihomo's external-controller.
type HTTPController struct {
	Addr   string // host:port or http://host:port
	Secret string
	HTTP   *http.Client
}

func (c *HTTPController) base() string {
	a := strings.TrimRight(c.Addr, "/")
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return a
}

func (c *HTTPController) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	cl := c.HTTP
	if cl == nil {
		// Never through a proxy: the controller is local.
		cl = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}
	}
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if ok := asURLError(err, &ue); ok {
			return nil, fmt.Errorf("%s %s: %w", method, path, ue.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%s %s: %w", method, path, ErrUnauthorized)
	}
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(b))
		var e struct{ Message string }
		if json.Unmarshal(b, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
	}
	return b, nil
}

func asURLError(err error, target **url.Error) bool {
	ue, ok := err.(*url.Error)
	if ok {
		*target = ue
	}
	return ok
}

// Version implements Controller.
func (c *HTTPController) Version(ctx context.Context) (string, error) {
	b, err := c.do(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Version == "" {
		return "", fmt.Errorf("GET /version: unexpected body %q", truncate(string(b), 200))
	}
	return v.Version, nil
}

// Reload implements Controller.
func (c *HTTPController) Reload(ctx context.Context, path string) error {
	return c.ReloadWith(ctx, path, true)
}

// ReloadWith implements GentleReloader.
func (c *HTTPController) ReloadWith(ctx context.Context, path string, force bool) error {
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/configs?force=%v", force), map[string]string{"path": path})
	return err
}

// controllerAddr turns an external-controller value into a dialable
// address: wildcard hosts become loopback.
func controllerAddr(v string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(v))
	if err != nil {
		return v
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
