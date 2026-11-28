package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/agent"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// 6.12: every MCP tool has words in every language (a verb and, where it
// has one, its object), so 最近操作 never shows a raw call.
func TestAgentToolsWorded(t *testing.T) {
	s := agent.New(agent.Deps{Doctor: func(context.Context, bool) (json.RawMessage, error) { return nil, nil }})
	tools := s.Tools()
	if len(tools) < 20 {
		t.Fatalf("only %d tools", len(tools))
	}
	for _, l := range i18n.Langs() {
		for _, tool := range tools {
			w := l.S.AgentTools
			_, plain := w[tool.Name]
			_, on := w[tool.Name+":on"]
			_, off := w[tool.Name+":off"]
			if !plain && !(on && off) {
				t.Errorf("%s: no words for tool %q", l.Tag, tool.Name)
			}
		}
		for k, f := range l.S.AgentToolsOf {
			if strings.Count(f, "%s") != 1 {
				t.Errorf("%s: AgentToolsOf[%q] = %q", l.Tag, k, f)
			}
			if _, ok := l.S.AgentTools[k]; !ok {
				t.Errorf("%s: AgentToolsOf[%q] has no plain form", l.Tag, k)
			}
		}
	}
}

func TestAgentActionWords(t *testing.T) {
	old := i18n.T
	t.Cleanup(func() { i18n.T = old })
	i18n.Set("zh")
	names := func(kind, id string) string {
		if kind == "layer" && id == "tun-stack" {
			return "TUN stack gvisor"
		}
		return ""
	}
	for _, c := range []struct {
		act  rpc.AgentAction
		want string
	}{
		{rpc.AgentAction{Client: "claude-code 2.1", Tool: "apply", Args: "preview_hash=78834c0a reason=tun", OK: true}, "claude-code 2.1 · 应用配置"},
		{rpc.AgentAction{Client: "codex", Tool: "layer_put", Args: "content=<20 bytes> id=tun-stack", OK: true}, "codex · 保存覆写「TUN stack gvisor」"},
		{rpc.AgentAction{Client: "codex", Tool: "layer_put", Args: "content=<20 bytes> id=x name=Ad block", OK: true}, "codex · 保存覆写「Ad block」"},
		{rpc.AgentAction{Client: "codex", Tool: "layer_enable", Args: "enabled=false id=tun-stack", OK: true}, "codex · 停用覆写「TUN stack gvisor」"},
		{rpc.AgentAction{Client: "codex", Tool: "set_tun", Args: "enabled=true", OK: true}, "codex · 打开 TUN"},
		{rpc.AgentAction{Client: "codex", Tool: "set_mode", Args: "mode=global", OK: true}, "codex · 切换到全局模式"},
		{rpc.AgentAction{Client: "codex", Tool: "select_node", Args: "group=Proxy node=JP 1", OK: false}, "codex · 选择节点 Proxy → JP 1 失败"},
		{rpc.AgentAction{Client: "codex", Tool: "future_tool", Args: "x=1", OK: true}, "codex · future_tool"},
	} {
		if got := agentActionWords(c.act, names); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.act.Tool, c.act.Args, got, c.want)
		}
		if tip := agentActionTip(c.act); !strings.HasPrefix(tip, c.act.Tool+" "+c.act.Args) {
			t.Errorf("tooltip %q lost the raw call", tip)
		}
	}
}

func TestParseAgentArgs(t *testing.T) {
	got := parseAgentArgs("group=Proxy node=JP 1 timeout_ms=5000")
	if got["group"] != "Proxy" || got["node"] != "JP 1" || got["timeout_ms"] != "5000" || len(got) != 3 {
		t.Errorf("%v", got)
	}
	if len(parseAgentArgs("")) != 0 {
		t.Error("empty line")
	}
}
