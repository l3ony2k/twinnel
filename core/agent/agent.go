// Package agent is Twinnel's MCP server (`twinnel mcp`): it lets a local AI agent
// (Claude Code, Codex, Cursor, ...) read Twinnel's state and, within the tiers
// the person turned on in 设置 → 本地 Agent, operate and configure it.
//
// It reimplements nothing: every tool is a call to the helper (core/rpc),
// to mihomo's external controller (core/mihomo, with the credentials the
// helper hands out) or to Tailscale's LocalAPI (core/tailscale).
//
// Safety model (docs/agents.md):
//   - Tiers: read (always on while MCP is enabled), operate (reversible
//     switches, default on), configure (stored configuration, default off).
//     The switches live in the helper's settings, read on every call, so
//     they hold for every client. An agent cannot change them: the settings
//     tool has no agent field.
//   - Some actions are never available (helper install/uninstall,
//     takeover, kernel install, service stop, removal): AdminOnly answers
//     where to do them in the app.
//   - Every operate/configure call is recorded in the helper's agent log
//     (agent.record) with the client's name; it shows in 日志 → Twinnel.
//   - Every output passes through redact: no controller secret,
//     subscription URL, node password or uuid leaves this package.
//
// The tiers are a guardrail for well-behaved agents, not a security
// boundary: an agent that runs as the person can run the Twinnel CLI itself.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/tailscale"
	"github.com/l3ony2k/twinnel/core/version"
)

// Caller is a connection to the helper (*rpc.Client).
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Deps is what the server reaches the system through. Tests give fakes.
type Deps struct {
	// Dial connects to the helper. The connection is closed after each
	// tool call (close may be nil).
	Dial func(ctx context.Context) (c Caller, close func(), err error)
	// Controller and Secret pin the mihomo controller (development:
	// TWINNEL_CONTROLLER). Empty: the helper's controller.credentials.
	Controller, Secret string
	// HTTPTransport is used for the controller (nil: the default).
	HTTPTransport http.RoundTripper
	// Tailscale is the LocalAPI (nil: Tailscale tools report it missing).
	Tailscale tailscale.API
	// Doctor is `twinnel doctor --json` (with fresh: --fresh); nil: the
	// doctor tool is not offered.
	Doctor func(ctx context.Context, fresh bool) (json.RawMessage, error)
	// HelperBinaries compares the helper binary shipped with Twinnel with
	// the installed one, given the running helper's version ("" unknown);
	// nil: the version tool leaves it out.
	HelperBinaries func(ctx context.Context, running string) *version.HelperBinaries
	// ReadOnly refuses every operate and configure tool (TWINNEL_READ_ONLY).
	ReadOnly bool
	// Version is Twinnel's version.
	Version string
	// Secrets are extra strings never to return (tests; the controller
	// secret is added when it is fetched).
	Secrets []string
	// LogWindow bounds how long the logs and traffic tools listen
	// (default 3s).
	LogWindow time.Duration
}

// Server is the MCP server with Twinnel's tools, resources and prompts.
type Server struct {
	d   Deps
	MCP *mcp.Server

	mu      sync.Mutex
	secrets map[string]bool

	tools []ToolInfo
}

// ToolInfo describes one tool (docs and tests).
type ToolInfo struct {
	Name  string
	Tier  string
	Title string
}

// ServerName is the MCP implementation name.
const ServerName = "twinnel"

// New builds the server.
func New(d Deps) *Server {
	if d.LogWindow <= 0 {
		d.LogWindow = 3 * time.Second
	}
	s := &Server{d: d, secrets: map[string]bool{}}
	for _, x := range d.Secrets {
		s.addSecret(x)
	}
	s.MCP = mcp.NewServer(&mcp.Implementation{Name: ServerName, Title: "Twinnel", Version: d.Version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	s.registerRead()
	s.registerOperate()
	s.registerConfigure()
	s.registerHealthFix()
	s.registerAdminOnly()
	s.registerResources()
	s.registerPrompts()
	return s
}

// Tools lists the registered tools in registration order.
func (s *Server) Tools() []ToolInfo { return append([]ToolInfo(nil), s.tools...) }

// Run serves on t (mcp.StdioTransport for `twinnel mcp`) until the client
// goes away.
func (s *Server) Run(ctx context.Context, t mcp.Transport) error { return s.MCP.Run(ctx, t) }

const instructions = `Twinnel manages mihomo (a rule-based proxy) and Tailscale on this computer.
Start with the status tool. Tools are grouped in tiers the person controls in Twinnel → 设置 → 本地 Agent:
read (always on), operate (reversible switches such as mode, node, TUN, exit node) and configure
(stored overrides, subscriptions, apply, rollback; off by default). A refused tool says which switch
is off: ask the person, do not look for another way. Changing the network can cut this computer's
connection, including your own: change one thing at a time and check status after it.
Configuration changes take effect only through apply, which first returns a preview to show the person.`

func (s *Server) addSecret(x string) {
	if len(x) < 4 { // too short to redact without mangling other text
		return
	}
	s.mu.Lock()
	s.secrets[x] = true
	s.mu.Unlock()
}

func (s *Server) knownSecrets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.secrets))
	for x := range s.secrets {
		out = append(out, x)
	}
	// Longest first, so a secret that contains another is replaced whole.
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// ---- one tool call ----

// call is the state of one tool call.
type call struct {
	s      *Server
	ctx    context.Context
	h      Caller // nil when the helper is not reachable (noHelper tools)
	herr   error  // why h is nil
	agent  rpc.AgentSettings
	client string

	// Set by write handlers for the activity log.
	undo string
}

// helper returns the helper connection or the reason there is none.
func (c *call) helper() (Caller, error) {
	if c.h == nil {
		return nil, helperDown(c.herr)
	}
	return c.h, nil
}

func helperDown(err error) error {
	return fmt.Errorf("the Twinnel helper is not reachable (%v). Open the Twinnel app: it shows how to start or install the helper", err)
}

// rpc calls the helper.
func (c *call) rpc(method string, params, result any) error {
	h, err := c.helper()
	if err != nil {
		return err
	}
	return h.Call(c.ctx, method, params, result)
}

// mihomo returns a controller client; write enables its write methods.
func (c *call) mihomo(write bool) (*mihomo.Client, error) {
	addr, secret := c.s.d.Controller, c.s.d.Secret
	if addr == "" {
		var cr rpc.ControllerCredentials
		if err := c.rpc(rpc.MethodControllerCredentials, nil, &cr); err != nil {
			return nil, fmt.Errorf("controller credentials: %w", err)
		}
		addr, secret = cr.Controller, cr.Secret
	}
	c.s.addSecret(secret)
	// Never change another program's mihomo (Clash Party, Clash Verge
	// Rev…) that holds the controller address.
	if write {
		var st rpc.Status
		if err := c.rpc(rpc.MethodStatus, nil, &st); err == nil && st.Mihomo.Owner.IsForeign() {
			return nil, foreignOwnerError(*st.Mihomo.Owner)
		}
	}
	return mihomo.New(mihomo.Options{Address: addr, Secret: secret, AllowWrites: write, Transport: c.s.d.HTTPTransport, Timeout: 10 * time.Second})
}

// foreignOwnerError says that another program's mihomo holds the
// controller address, and how to free it.
func foreignOwnerError(o foreign.Owner) error {
	who := o.Client.Name()
	if who == "" {
		who = "another program"
	}
	return fmt.Errorf("%s is held by %s's mihomo, not Twinnel's; quit it or move Twinnel's controller (health_fix controller-owner) first", o.Addr, who)
}

// tailscale returns the LocalAPI client.
func (c *call) tailscale(write bool) (*tailscale.Client, error) {
	if c.s.d.Tailscale == nil {
		return nil, errors.New("Tailscale is not available here")
	}
	return tailscale.New(c.s.d.Tailscale, write), nil
}

// toolOpts are the per-tool flags.
type toolOpts struct {
	noHelper    bool // works (partly) without the helper; skips the tier gate except read
	destructive bool
	idempotent  bool
	openWorld   bool
	// enums are the allowed values of string arguments, by name; they go
	// into the input schema and into the error for a wrong value.
	enums map[string][]string
}

// handler is a typed tool body.
type handler[In any] func(c *call, in In) (any, error)

// addTool registers a tool of tier with a redacting, gating, recording
// wrapper. desc is written for agents: what it does, its effect on the
// network, how to undo it.
func addTool[In any](s *Server, tier, name, title, desc string, o toolOpts, h handler[In]) {
	ann := &mcp.ToolAnnotations{Title: title, ReadOnlyHint: tier == rpc.TierRead, IdempotentHint: o.idempotent}
	if tier != rpc.TierRead {
		d := o.destructive
		ann.DestructiveHint = &d
	}
	ow := o.openWorld
	ann.OpenWorldHint = &ow
	desc = strings.TrimSpace(desc)
	if tier != rpc.TierRead {
		desc += fmt.Sprintf("\nTier: %s (Twinnel → 设置 → 本地 Agent).", tier)
	}
	s.tools = append(s.tools, ToolInfo{Name: name, Tier: tier, Title: title})
	// The arguments are checked here (args.go), not by mcp.AddTool, so that
	// a mistake gets an answer an agent can act on.
	ts := newToolSchema[In](o.enums)
	s.MCP.AddTool(&mcp.Tool{Name: name, Title: title, Description: desc, Annotations: ann, InputSchema: ts.s},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			raw, err := ts.check(name, req.Params.Arguments)
			var in In
			if err == nil {
				if err = json.Unmarshal(raw, &in); err != nil {
					err = fmt.Errorf("%s: %v", name, err)
				}
			}
			if err != nil {
				return s.errorResult(err), nil
			}
			return s.run(ctx, req, tier, name, o, in, func(c *call) (any, error) { return h(c, in) }), nil
		})
}

func (s *Server) run(ctx context.Context, req *mcp.CallToolRequest, tier, name string, o toolOpts, in any, body func(*call) (any, error)) *mcp.CallToolResult {
	c := &call{s: s, ctx: ctx, client: clientName(req)}
	if tier != rpc.TierRead && s.d.ReadOnly {
		return s.errorResult(fmt.Errorf("%s changes the system, and this Twinnel runs read-only (TWINNEL_READ_ONLY is set)", name))
	}
	if s.d.Dial != nil {
		h, closeFn, err := s.d.Dial(ctx)
		if err == nil {
			c.h = h
			if closeFn != nil {
				defer closeFn()
			}
		} else {
			c.herr = err
		}
	} else {
		c.herr = errors.New("no helper configured")
	}
	if c.h == nil {
		if !o.noHelper {
			return s.errorResult(helperDown(c.herr))
		}
	} else {
		var st rpc.Settings
		if err := c.h.Call(ctx, rpc.MethodSettingsGet, nil, &st); err != nil {
			return s.errorResult(fmt.Errorf("reading the agent switches from the helper: %w", err))
		}
		c.agent = st.Agent
		// noHelper only matters when the switches cannot be read: with the
		// helper up, MCP off means every tool is off.
		if !st.Agent.Allows(tier) {
			return s.errorResult(refusal(tier, name, st.Agent))
		}
	}
	out, err := body(c)
	if tier != rpc.TierRead && c.h != nil {
		s.record(c, tier, name, in, err)
	}
	if err != nil {
		return s.errorResult(err)
	}
	return s.textResult(out)
}

// refusal explains which switch keeps a tool off.
func refusal(tier, name string, a rpc.AgentSettings) error {
	if !a.MCP {
		return errors.New("agent access is turned off in Twinnel. The person can turn it on in Twinnel → 设置 → 本地 Agent → 允许 MCP")
	}
	switch tier {
	case rpc.TierOperate:
		return fmt.Errorf("%s is an operate tool, and operate tools are turned off in Twinnel → 设置 → 本地 Agent. Ask the person to do it in the app, or to turn 操作 on", name)
	case rpc.TierConfigure:
		return fmt.Errorf("%s is a configure tool, and configure tools are turned off in Twinnel → 设置 → 本地 Agent (off by default). Ask the person to do it in the app, or to turn 修改配置 on", name)
	}
	return fmt.Errorf("%s is not available", name)
}

func clientName(req *mcp.CallToolRequest) string {
	if req == nil {
		return ""
	}
	ci := req.ClientInfo()
	if ci == nil {
		return ""
	}
	n := ci.Name
	if ci.Version != "" {
		n += " " + ci.Version
	}
	return n
}

// record writes the action to the helper's agent log. A failure to record
// is not the tool's failure; it is logged in the result's absence only.
func (s *Server) record(c *call, tier, name string, in any, err error) {
	a := rpc.AgentAction{Client: c.client, Tool: name, Tier: tier, Args: s.argsSummary(in), OK: err == nil, Undo: s.redactString(c.undo)}
	if err != nil {
		a.Error = s.redactString(err.Error())
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), 5*time.Second)
	defer cancel()
	c.h.Call(ctx, rpc.MethodAgentRecord, a, nil)
}

// argsSummary is a one-line, redacted "key=value" list of the arguments.
// Long contents show as their size; URLs never show.
func (s *Server) argsSummary(in any) string {
	b, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := m[k]
		var val string
		switch {
		case k == "url":
			val = "<set>"
		case k == "content":
			str, _ := v.(string)
			val = fmt.Sprintf("<%d bytes>", len(str))
		default:
			vb, _ := json.Marshal(v)
			val = string(vb)
			if str, ok := v.(string); ok {
				val = str
			}
		}
		parts = append(parts, k+"="+val)
	}
	return s.redactString(strings.Join(parts, " "))
}

func (s *Server) textResult(v any) *mcp.CallToolResult {
	b, err := s.redactJSON(v)
	if err != nil {
		return s.errorResult(err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func (s *Server) errorResult(err error) *mcp.CallToolResult {
	msg := err.Error()
	var re *rpc.Error
	if errors.As(err, &re) {
		msg = re.Message
		if len(re.Data) > 0 {
			msg += "\n" + string(re.Data)
		}
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: s.redactString(msg)}}}
}
