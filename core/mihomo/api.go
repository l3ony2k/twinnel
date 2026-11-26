package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Version is the answer of GET /version.
type Version struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

// Version returns the core's version.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	err := c.do(ctx, http.MethodGet, "/version", nil, nil, &v)
	return v, err
}

// Mode is the proxy mode.
type Mode string

const (
	ModeRule   Mode = "rule"
	ModeGlobal Mode = "global"
	ModeDirect Mode = "direct"
)

// Modes lists the modes in display order.
var Modes = []Mode{ModeRule, ModeGlobal, ModeDirect}

// Tun is the tun part of GET /configs.
type Tun struct {
	Enable              bool     `json:"enable"`
	Device              string   `json:"device"`
	Stack               string   `json:"stack"`
	AutoRoute           bool     `json:"auto-route"`
	AutoDetectInterface bool     `json:"auto-detect-interface"`
	DNSHijack           []string `json:"dns-hijack"`
	RouteExcludeAddress []string `json:"route-exclude-address"`
	Inet4Address        []string `json:"inet4-address"`
	MTU                 int      `json:"mtu"`
}

// Config is the subset of GET /configs the app uses.
type Config struct {
	Mode            Mode   `json:"mode"`
	Port            int    `json:"port"`
	SocksPort       int    `json:"socks-port"`
	MixedPort       int    `json:"mixed-port"`
	AllowLan        bool   `json:"allow-lan"`
	BindAddress     string `json:"bind-address"`
	LogLevel        string `json:"log-level"`
	IPv6            bool   `json:"ipv6"`
	Tun             Tun    `json:"tun"`
	UnifiedDelay    bool   `json:"unified-delay"`
	InterfaceName   string `json:"interface-name"`
	FindProcessMode string `json:"find-process-mode"`
}

// Configs returns the running configuration.
func (c *Client) Configs(ctx context.Context) (Config, error) {
	var cfg Config
	err := c.do(ctx, http.MethodGet, "/configs", nil, nil, &cfg)
	return cfg, err
}

// DelayRecord is one past delay test.
type DelayRecord struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"` // ms, 0 = failed
}

// Proxy is a node or a group, as GET /proxies lists them.
type Proxy struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Alive   bool          `json:"alive"`
	History []DelayRecord `json:"history"`
	UDP     bool          `json:"udp"`
	// Group fields.
	Now     string   `json:"now"`
	All     []string `json:"all"`
	Hidden  bool     `json:"hidden"`
	TestURL string   `json:"testUrl"`
	Icon    string   `json:"icon"`
}

// IsGroup reports whether p is a proxy group.
func (p Proxy) IsGroup() bool { return IsGroupType(p.Type) }

// IsGroupType reports whether t names a proxy group type.
func IsGroupType(t string) bool {
	switch t {
	case "Selector", "URLTest", "Fallback", "LoadBalance", "Relay", "Smart":
		return true
	}
	return false
}

// LastDelay returns the delay of the latest test in ms, 0 when it failed,
// and false when there is none.
func (p Proxy) LastDelay() (int, bool) {
	if len(p.History) == 0 {
		return 0, false
	}
	return p.History[len(p.History)-1].Delay, true
}

// Proxies returns every proxy and group by name.
func (c *Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var out struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Proxies, nil
}

// Proxy returns one proxy or group.
func (c *Client) Proxy(ctx context.Context, name string) (Proxy, error) {
	var p Proxy
	err := c.do(ctx, http.MethodGet, "/proxies/"+seg(name), nil, nil, &p)
	return p, err
}

// Groups returns the groups of a /proxies answer in the configuration's
// order. mihomo returns a map; the order of the GLOBAL group's members is
// the order of proxy-groups in the configuration, so groups follow it, then
// any group GLOBAL does not list, by name, then GLOBAL itself.
func Groups(all map[string]Proxy) []Proxy {
	var out []Proxy
	seen := map[string]bool{}
	if g, ok := all["GLOBAL"]; ok {
		for _, name := range g.All {
			if p, ok := all[name]; ok && p.IsGroup() && !seen[name] {
				out = append(out, p)
				seen[name] = true
			}
		}
	}
	var rest []Proxy
	for name, p := range all {
		if p.IsGroup() && !seen[name] && name != "GLOBAL" {
			rest = append(rest, p)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	out = append(out, rest...)
	if g, ok := all["GLOBAL"]; ok {
		out = append(out, g)
	}
	return out
}

// Resolve follows a group's selection through nested groups to the node
// that carries the traffic, e.g. PROXY → "Auto" → "node-3". It returns the
// chain of names after start, and stops at loops.
func Resolve(all map[string]Proxy, start string) []string {
	var chain []string
	seen := map[string]bool{start: true}
	cur := start
	for {
		p, ok := all[cur]
		if !ok || !p.IsGroup() || p.Now == "" || seen[p.Now] {
			return chain
		}
		chain = append(chain, p.Now)
		seen[p.Now] = true
		cur = p.Now
	}
}

// Delay tests one proxy (GET /proxies/{name}/delay) and returns ms.
func (c *Client) Delay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error) {
	if testURL == "" {
		testURL = DefaultTestURL
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	q := url.Values{"url": {testURL}, "timeout": {strconv.Itoa(int(timeout / time.Millisecond))}}
	var out struct {
		Delay int `json:"delay"`
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, "/proxies/"+seg(name)+"/delay", q, nil, &out)
	return out.Delay, err
}

// GroupDelay tests every member of a group (GET /group/{name}/delay) and
// returns ms by member; failed members are missing.
func (c *Client) GroupDelay(ctx context.Context, group, testURL string, timeout time.Duration) (map[string]int, error) {
	if testURL == "" {
		testURL = DefaultTestURL
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	q := url.Values{"url": {testURL}, "timeout": {strconv.Itoa(int(timeout / time.Millisecond))}}
	out := map[string]int{}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, "/group/"+seg(group)+"/delay", q, nil, &out)
	return out, err
}

// Traffic is one sample of /traffic: bytes per second and totals.
type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

// StreamTraffic calls fn with a sample about every second until ctx ends or
// the stream breaks. It always returns a non-nil error.
func (c *Client) StreamTraffic(ctx context.Context, fn func(Traffic)) error {
	return c.streamLines(ctx, "/traffic", nil, func(b []byte) error {
		var t Traffic
		if err := json.Unmarshal(b, &t); err != nil {
			return err
		}
		fn(t)
		return nil
	})
}

// LogEntry is one line of /logs.
type LogEntry struct {
	Type    string    `json:"type"` // debug, info, warning, error
	Payload string    `json:"payload"`
	Time    time.Time `json:"-"` // when it was received
}

// StreamLogs calls fn with every log line at level or above ("debug",
// "info", "warning", "error"; empty means the core's level).
func (c *Client) StreamLogs(ctx context.Context, level string, fn func(LogEntry)) error {
	var q url.Values
	if level != "" {
		q = url.Values{"level": {level}}
	}
	return c.streamLines(ctx, "/logs", q, func(b []byte) error {
		var e LogEntry
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		e.Time = time.Now()
		fn(e)
		return nil
	})
}

// ConnMetadata describes a connection.
type ConnMetadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	SniffHost       string `json:"sniffHost"`
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

// Connection is one tracked connection.
type Connection struct {
	ID          string       `json:"id"`
	Metadata    ConnMetadata `json:"metadata"`
	Upload      int64        `json:"upload"`
	Download    int64        `json:"download"`
	Start       time.Time    `json:"start"`
	Chains      []string     `json:"chains"`
	Rule        string       `json:"rule"`
	RulePayload string       `json:"rulePayload"`
}

// Connections is the answer of GET /connections.
type Connections struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        int64        `json:"memory"`
}

// Connections returns a snapshot of the tracked connections.
func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var out Connections
	err := c.do(ctx, http.MethodGet, "/connections", nil, nil, &out)
	return out, err
}

// --- write operations: tested only against fakes ---

// SelectProxy chooses name in a Selector group (PUT /proxies/{group}).
func (c *Client) SelectProxy(ctx context.Context, group, name string) error {
	return c.write(ctx, http.MethodPut, "/proxies/"+seg(group), nil, map[string]string{"name": name})
}

// SetMode changes the mode (PATCH /configs).
func (c *Client) SetMode(ctx context.Context, m Mode) error {
	return c.write(ctx, http.MethodPatch, "/configs", nil, map[string]any{"mode": m})
}

// SetTun turns the tun on or off (PATCH /configs).
func (c *Client) SetTun(ctx context.Context, enable bool) error {
	return c.write(ctx, http.MethodPatch, "/configs", nil, map[string]any{"tun": map[string]bool{"enable": enable}})
}

// Reload loads the configuration file at path (PUT /configs?force=true).
// An empty path reloads the file the core started with. Validate the file
// with `mihomo -t` first: the core keeps the old config only on parse
// errors it detects itself.
func (c *Client) Reload(ctx context.Context, path string) error {
	return c.write(ctx, http.MethodPut, "/configs", url.Values{"force": {"true"}}, map[string]string{"path": path})
}

// FlushFakeIP clears the fake-ip cache (POST /cache/fakeip/flush).
func (c *Client) FlushFakeIP(ctx context.Context) error {
	return c.write(ctx, http.MethodPost, "/cache/fakeip/flush", nil, nil)
}

// FlushDNS clears the DNS cache (POST /cache/dns/flush).
func (c *Client) FlushDNS(ctx context.Context) error {
	return c.write(ctx, http.MethodPost, "/cache/dns/flush", nil, nil)
}

// CloseConnection closes one connection (DELETE /connections/{id}).
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	return c.write(ctx, http.MethodDelete, "/connections/"+seg(id), nil, nil)
}
