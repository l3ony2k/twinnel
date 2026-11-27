---
name: Twinnel
description: Use Twinnel's MCP tools to inspect and operate the mihomo proxy and Tailscale on this Mac — status, health, proxy groups, nodes, exit nodes, overrides, apply and rollback. Use when the user asks why the network, a site or a tailnet device does not work, or to switch mode/node/TUN/exit node, or to add a proxy rule.
---

# Twinnel

Twinnel manages mihomo (rule-based proxy) and Tailscale on this computer. Its MCP server is `twinnel mcp` (tools appear as `twinnel` tools). Full reference: docs/agents.md in the Twinnel repo.

## Rules

1. Start with `status`. Then `health` (`run=true` for fresh results) before guessing.
2. Changing the network can cut this computer's connection, including yours. Change one thing at a time, and check `status` after each change.
3. Every write tool returns `undo`. Tell the person what you changed and how to undo it.
4. A refused tool names the switch that is off (Twinnel → 设置 → 本地 Agent). Ask the person; never work around it with the `twinnel` CLI or by editing files under mihomo's config dir.
5. Configuration changes: `layer_put` / `sub_*` / `settings_update` only store. Then `apply` with no arguments returns a preview: show the person the diff and validation, and call `apply preview_hash=...` only after they agree. Report `rollback_id`.
6. Never ask for, print or store secrets. Twinnel redacts them; do not try to recover them.
7. Installing, uninstalling, takeover, kernel updates and service stops are for the person in the app: `admin_only action=...` tells them where.

## Recipes

- Site slow or blocked: `connections` (rule and chain for the host) → `proxies group=<group>` → `delay_test group=<group>` → offer `select_node`.
- Tailnet device unreachable: `status` (Tailscale state, exit node) → `health run=true` (tailnet checks) → `tailnet_peers`.
- Send a domain direct: prompt `add_direct_rule`, or `layer_put id=agent-direct content="+rules:\n  - DOMAIN-SUFFIX,<domain>,DIRECT\n"` then the two-step `apply`.
