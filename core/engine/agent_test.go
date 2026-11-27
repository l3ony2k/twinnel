package engine

import (
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
)

func TestAgentSettingsDefaults(t *testing.T) {
	h := newHarness(t)
	st, err := h.e.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if want := (rpc.AgentSettings{MCP: false, Operate: true, Configure: false}); st.Agent != want {
		t.Fatalf("defaults = %+v, want %+v", st.Agent, want)
	}
	on, off := true, false
	st, err = h.e.UpdateSettings(rpc.SettingsUpdate{Agent: &rpc.AgentSettingsUpdate{MCP: &on, Operate: &off, Configure: &on}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (rpc.AgentSettings{MCP: true, Operate: false, Configure: true}); st.Agent != want {
		t.Fatalf("after update = %+v, want %+v", st.Agent, want)
	}
	// Other updates keep the agent switches.
	keep := 7
	st, _ = h.e.UpdateSettings(rpc.SettingsUpdate{HistoryKeep: &keep})
	if !st.Agent.MCP || st.Agent.Operate || !st.Agent.Configure {
		t.Fatalf("agent switches changed by another update: %+v", st.Agent)
	}
}

func TestAgentRecord(t *testing.T) {
	h := newHarness(t)
	after := h.e.Bus.Since
	_, next := after(0)
	if _, err := h.e.RecordAgentAction(rpc.AgentAction{Tool: "", Tier: rpc.TierOperate}); err == nil {
		t.Fatal("empty tool accepted")
	}
	if _, err := h.e.RecordAgentAction(rpc.AgentAction{Tool: "x", Tier: "root"}); err == nil {
		t.Fatal("bad tier accepted")
	}
	for i, tool := range []string{"select_node", "set_mode"} {
		_, err := h.e.RecordAgentAction(rpc.AgentAction{
			Client: "test-client 1.0\n", Tool: tool, Tier: rpc.TierOperate,
			Args: "a\nb " + strings.Repeat("x", 2000), OK: i == 0, Source: "spoofed",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := h.e.AgentActivity(rpc.AgentActivityParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Tool != "set_mode" || list[1].Tool != "select_node" {
		t.Fatalf("activity = %+v", list)
	}
	a := list[0]
	if a.Source != "agent" || a.Client != "test-client 1.0" || strings.Contains(a.Args, "\n") || len(a.Args) > agentFieldMax+4 || a.Time.IsZero() {
		t.Fatalf("record not normalized: %+v", a)
	}
	evs, _ := after(next)
	n := 0
	for _, e := range evs {
		if e.Type == rpc.EventAgentAction {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d agent.action events, want 2", n)
	}
	// Bounded.
	for range agentActivityKeep + 5 {
		h.e.RecordAgentAction(rpc.AgentAction{Tool: "t", Tier: rpc.TierRead})
	}
	list, _ = h.e.AgentActivity(rpc.AgentActivityParams{Limit: 1000})
	if len(list) != agentActivityKeep {
		t.Fatalf("kept %d, want %d", len(list), agentActivityKeep)
	}
}
