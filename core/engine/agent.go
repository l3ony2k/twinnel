package engine

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// What local agents changed (agent.record / agent.activity): a short log
// in the state dir, kept apart from the event ring so health events do not
// push it out. The MCP server redacts what it sends; the helper still
// bounds every field.

const (
	agentActivityFile = "agent-activity.json"
	agentActivityKeep = 200
	agentFieldMax     = 512
)

// RecordAgentAction appends a to the activity log and publishes it. The
// caller is the peer of ctx's call (rpc.PeerFrom), whatever a says.
func (e *Engine) RecordAgentAction(ctx context.Context, a rpc.AgentAction) (rpc.Empty, error) {
	if a.Tool == "" || len(a.Tool) > 64 {
		return rpc.Empty{}, rpc.ErrInvalid("invalid tool name")
	}
	switch a.Tier {
	case rpc.TierRead, rpc.TierOperate, rpc.TierConfigure:
	default:
		return rpc.Empty{}, rpc.ErrInvalid("invalid tier %q", a.Tier)
	}
	a.Source = "agent"
	a.Time = e.now().UTC()
	a.Client = clip(oneLine(a.Client), 128)
	a.Args = clip(oneLine(a.Args), agentFieldMax)
	a.Error = clip(oneLine(a.Error), agentFieldMax)
	a.Undo = clip(oneLine(a.Undo), 256)
	if a.Client == "" {
		a.Client = "unknown"
	}
	a.Caller = ""
	if p, ok := rpc.PeerFrom(ctx); ok {
		a.Caller = p.Caller()
	}
	e.agentMu.Lock()
	defer e.agentMu.Unlock()
	list, err := e.agentActivity()
	if err != nil {
		return rpc.Empty{}, err
	}
	list = append(list, a)
	if over := len(list) - agentActivityKeep; over > 0 {
		list = append(list[:0:0], list[over:]...)
	}
	if err := e.Store.WriteJSON(agentActivityFile, list); err != nil {
		return rpc.Empty{}, err
	}
	e.publish(rpc.EventAgentAction, a)
	return rpc.Empty{}, nil
}

// AgentActivity lists the recorded actions, newest first.
func (e *Engine) AgentActivity(p rpc.AgentActivityParams) ([]rpc.AgentAction, error) {
	n := p.Limit
	if n <= 0 {
		n = 50
	}
	n = min(n, agentActivityKeep)
	e.agentMu.Lock()
	list, err := e.agentActivity()
	e.agentMu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([]rpc.AgentAction, 0, min(n, len(list)))
	for i := len(list) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, list[i])
	}
	return out, nil
}

// agentActivity reads the log, oldest first. agentMu held.
func (e *Engine) agentActivity() ([]rpc.AgentAction, error) {
	var list []rpc.AgentAction
	if err := e.Store.ReadJSON(agentActivityFile, &list); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// A damaged log is not worth refusing writes for: start again.
		if e.Log != nil {
			e.Log.Warn("agent activity unreadable; starting a new log", "err", err)
		}
		return nil, nil
	}
	return list, nil
}

func (e *Engine) registerAgent(s *rpc.Server) {
	rpc.Typed(s, rpc.MethodAgentRecord, func(ctx context.Context, a rpc.AgentAction) (rpc.Empty, error) {
		return e.RecordAgentAction(ctx, a)
	})
	rpc.Typed(s, rpc.MethodAgentActivity, func(ctx context.Context, p rpc.AgentActivityParams) ([]rpc.AgentAction, error) {
		return e.AgentActivity(p)
	})
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
