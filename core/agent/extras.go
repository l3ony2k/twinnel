package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/duet/core/rpc"
)

// ---- never via MCP ----

// AdminActions are the actions agents never get, with where the person
// does them in the duet app.
var AdminActions = map[string]string{
	"helper_install":    "设置 → 后台助手 (install from the app's onboarding)",
	"helper_uninstall":  "设置 → 停止使用 duet → 卸载后台助手",
	"helper_upgrade":    "设置 → 后台助手 → 更新",
	"takeover_adopt":    "接管 page: review the plan, then 接管",
	"takeover_undo":     "设置 → 停止使用 duet → 撤销接管",
	"harden":            "内核 page → 加固 (moves mihomo to duet's own service)",
	"kernel_install":    "内核 page → 更新",
	"kernel_rollback":   "内核 page → 回滚",
	"service_stop":      "内核 page → mihomo service card",
	"service_uninstall": "内核 page → mihomo service card",
	"remove_duet":       "设置 → 停止使用 duet → 完全移除 duet",
	"agent_switches":    "设置 → 本地 Agent",
}

type adminIn struct {
	Action string `json:"action" jsonschema:"what you wanted to do"`
}

func (s *Server) registerAdminOnly() {
	names := make([]string, 0, len(AdminActions))
	for k := range AdminActions {
		names = append(names, k)
	}
	sort.Strings(names)
	addTool(s, rpc.TierRead, "admin_only", "Actions for the person",
		"Actions an agent can never take through duet, because they install, remove or move system pieces "+
			"or change what agents may do: "+strings.Join(names, ", ")+". Call this with the action to get "+
			"where the person does it in the duet app, then tell them. It changes nothing.",
		toolOpts{noHelper: true}, func(c *call, in adminIn) (any, error) {
			where, ok := AdminActions[in.Action]
			if !ok {
				where = "the duet app"
			}
			return map[string]any{
				"action":  in.Action,
				"allowed": false,
				"message": fmt.Sprintf("Agents cannot do this through duet. Ask the person to do it in the duet app: %s.", where),
			}, nil
		})
}

// ---- resources ----

const (
	resStatus = "duet://status"
	resHealth = "duet://health"
	resConfig = "duet://config/summary"
)

func (s *Server) registerResources() {
	s.addResource(resStatus, "status", "duet status snapshot (same as the status tool)", rpc.TierRead, true,
		func(c *call) (any, error) { return s.status(c, noArgs{}) })
	s.addResource(resHealth, "health", "The last health check report", rpc.TierRead, false,
		func(c *call) (any, error) { return s.health(c, healthIn{}) })
	s.addResource(resConfig, "config-summary", "The running mihomo config in numbers and names: mode, TUN, DNS, groups, rule and node counts, overrides. No node details or credentials.", rpc.TierRead, false,
		s.configSummary)
}

func (s *Server) addResource(uri, name, desc, tier string, noHelper bool, fn func(c *call) (any, error)) {
	s.MCP.AddResource(&mcp.Resource{URI: uri, Name: name, Description: desc, MIMEType: "application/json"},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			res := s.run(ctx, nil, tier, name, toolOpts{noHelper: noHelper}, nil, fn)
			text := ""
			if len(res.Content) > 0 {
				if t, ok := res.Content[0].(*mcp.TextContent); ok {
					text = t.Text
				}
			}
			if res.IsError {
				return nil, fmt.Errorf("%s", text)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: text}}}, nil
		})
}

func (s *Server) configSummary(c *call) (any, error) {
	var ct rpc.ConfigText
	if err := c.rpc(rpc.MethodConfigGet, rpc.ConfigGetParams{Source: rpc.ConfigLive}, &ct); err != nil {
		return nil, err
	}
	var cfg struct {
		Mode      string           `yaml:"mode"`
		MixedPort int              `yaml:"mixed-port"`
		AllowLan  bool             `yaml:"allow-lan"`
		Tun       map[string]any   `yaml:"tun"`
		DNS       map[string]any   `yaml:"dns"`
		Proxies   []map[string]any `yaml:"proxies"`
		Providers map[string]any   `yaml:"proxy-providers"`
		Groups    []struct {
			Name    string   `yaml:"name"`
			Type    string   `yaml:"type"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules         []string       `yaml:"rules"`
		RuleProviders map[string]any `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal([]byte(ct.Content), &cfg); err != nil {
		return nil, fmt.Errorf("the running config does not parse: %w", err)
	}
	type group struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Members int    `json:"members"`
	}
	var groups []group
	for _, g := range cfg.Groups {
		groups = append(groups, group{g.Name, g.Type, len(g.Proxies)})
	}
	pick := func(m map[string]any, keys ...string) map[string]any {
		out := map[string]any{}
		for _, k := range keys {
			if v, ok := m[k]; ok {
				out[k] = v
			}
		}
		return out
	}
	providers := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		providers = append(providers, k)
	}
	sort.Strings(providers)
	out := map[string]any{
		"hash": ct.Hash, "size": ct.Size, "mode": cfg.Mode, "mixed_port": cfg.MixedPort, "allow_lan": cfg.AllowLan,
		"tun":             pick(cfg.Tun, "enable", "stack", "auto-route", "auto-detect-interface", "strict-route"),
		"dns":             pick(cfg.DNS, "enable", "enhanced-mode", "listen", "ipv6"),
		"nodes":           len(cfg.Proxies),
		"proxy_providers": providers,
		"groups":          groups,
		"rules":           len(cfg.Rules),
		"rule_providers":  len(cfg.RuleProviders),
	}
	var ls []rpc.Layer
	if c.rpc(rpc.MethodLayerList, nil, &ls) == nil {
		var names []string
		for _, l := range ls {
			st := "on"
			if !l.Enabled {
				st = "off"
			}
			names = append(names, fmt.Sprintf("%s (%s, %s)", l.ID, l.Stage, st))
		}
		out["overrides"] = names
	}
	return out, nil
}

// ---- prompts ----

func (s *Server) registerPrompts() {
	s.MCP.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_connectivity",
		Title:       "Diagnose connectivity",
		Description: "Find out why the network (or a site) does not work through mihomo and Tailscale, read-only first.",
		Arguments:   []*mcp.PromptArgument{{Name: "symptom", Description: "what does not work, e.g. a site, an app or a tailnet device"}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		sym := req.Params.Arguments["symptom"]
		if sym == "" {
			sym = "the network does not work as expected"
		}
		text := "Diagnose this with duet's tools: " + sym + `

1. Call status. Note mihomo reachable/mode/TUN, Tailscale state and exit node, and health not_ok.
2. Call health with run=true and read every warn or fail check's evidence and fix hint.
3. If a site is involved: connections (is it there, which rule and chain), proxies for that chain's group
   (delays), and delay_test on the node in use.
4. logs source=mihomo level=warning while the person retries, and logs source=duet for recent applies.
5. Explain the cause in two or three sentences, with the evidence.
Then propose the smallest fix. Do not change anything until the person agrees; prefer reversible operate
tools, and say how to undo each step.`
		return &mcp.GetPromptResult{Description: "Diagnose connectivity", Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}, nil
	})

	s.MCP.AddPrompt(&mcp.Prompt{
		Name:        "add_direct_rule",
		Title:       "Send a domain direct",
		Description: "Add a rule that sends a domain (and its subdomains) DIRECT, through an override, with a preview before apply.",
		Arguments:   []*mcp.PromptArgument{{Name: "domain", Description: "e.g. example.com", Required: true}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		d := strings.TrimSpace(req.Params.Arguments["domain"])
		if d == "" || strings.ContainsAny(d, " \n,/") {
			return nil, fmt.Errorf("domain must be a plain domain name")
		}
		b, _ := json.Marshal(d)
		text := "Make " + d + ` and its subdomains go DIRECT through mihomo, using duet's configure tools:

1. Call layers. If an override with id "agent-direct" exists, read it (layers id=agent-direct) and add to it.
2. layer_put id=agent-direct name="Direct (agent)" with content:
     +rules:
       - DOMAIN-SUFFIX,` + strings.Trim(string(b), `"`) + `,DIRECT
   ("+rules" puts the rule before the subscription's rules.)
3. Call apply without preview_hash. Show the person the diff and validation, and wait for their yes.
4. apply with the preview_hash. Report the history id and the rollback_id.
If configure tools are off, tell the person and give them the override text to add in duet → 配置 instead.`
		return &mcp.GetPromptResult{Description: "Send a domain direct", Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}, nil
	})
}
