package rpc

import "time"

// Local AI agents (`duet mcp`, docs/agents.md). The tier switches live in
// the helper's settings so they hold for every client; agent.record keeps
// what an agent changed, for 日志 → duet and 设置 → 本地 Agent.
const (
	// MethodAgentRecord appends one agent action to the activity log and
	// publishes EventAgentAction. The caller redacts Args before sending.
	MethodAgentRecord = "agent.record"
	// MethodAgentActivity lists the recorded actions, newest first.
	MethodAgentActivity = "agent.activity"
)

// EventAgentAction: a local agent changed something through duet (data:
// AgentAction).
const EventAgentAction = "agent.action"

// Agent tiers. Read is always on while MCP is enabled.
const (
	TierRead      = "read"
	TierOperate   = "operate"   // reversible: mode, node, TUN, Tailscale up/down, exit node, refresh
	TierConfigure = "configure" // overrides, subscriptions, apply, rollback, settings
)

// AgentSettings are the switches of 设置 → 本地 Agent.
type AgentSettings struct {
	// MCP: `duet mcp` serves tools at all. Off: every tool answers that
	// MCP is turned off in the app.
	MCP bool `json:"mcp"`
	// Operate: the reversible operate tools (default on).
	Operate bool `json:"operate"`
	// Configure: tools that change the stored configuration (default off).
	Configure bool `json:"configure"`
}

// AgentSettingsUpdate changes some of them (settings.update, field agent).
type AgentSettingsUpdate struct {
	MCP       *bool `json:"mcp,omitempty"`
	Operate   *bool `json:"operate,omitempty"`
	Configure *bool `json:"configure,omitempty"`
}

// Allows reports whether a tool of tier may run.
func (a AgentSettings) Allows(tier string) bool {
	if !a.MCP {
		return false
	}
	switch tier {
	case TierRead:
		return true
	case TierOperate:
		return a.Operate
	case TierConfigure:
		return a.Configure
	}
	return false
}

// AgentAction is one write an agent made. It never holds a secret: the
// MCP server redacts Args before agent.record.
type AgentAction struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"` // always "agent"
	Client string    `json:"client"` // the MCP client's name and version, e.g. "claude-code 2.1"
	Tool   string    `json:"tool"`
	Tier   string    `json:"tier"`
	// Args is a one-line summary of the arguments ("group=Proxy node=JP 1").
	Args  string `json:"args,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Undo says how to undo it, when there is a way ("history rollback ID").
	Undo string `json:"undo,omitempty"`
}

// AgentActivityParams: Limit bounds the list (default 50, max 200).
type AgentActivityParams struct {
	Limit int `json:"limit,omitempty"`
}
