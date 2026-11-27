# duet for AI agents (MCP and the CLI)

duet lets a local AI agent (Claude Code, Codex, Cursor, or any MCP client) see and operate the mihomo + Tailscale setup it manages, within limits the person sets in the app. 中文摘要见文末。

There are two ways in:

- **`duet mcp`**: an MCP server on stdin/stdout. This is the supported way: it is tiered, redacted and logged.
- **The `duet` CLI**: every command takes `--json` with a stable shape (below). Use it in scripts. It is not tiered: it does what the person running it may do.

## Set up

1. In duet: **设置 → 本地 Agent** (Settings → Local agents) → turn on **允许 MCP**. Optionally turn off **操作** (operate) or turn on **修改配置** (configure).
2. Install the command line tool from the same section (a symlink to the CLI inside duet.app, in `/usr/local/bin` or `~/.local/bin`; **移除** takes it away; 完全移除 duet removes it too).
3. Add duet to your client. The section shows these with the right path and a copy button; duet never edits another app's config:

```sh
claude mcp add duet -- /usr/local/bin/duet mcp     # Claude Code
codex mcp add duet -- /usr/local/bin/duet mcp      # Codex
```

```json
{ "mcpServers": { "duet": { "command": "/usr/local/bin/duet", "args": ["mcp"] } } }
```

(Cursor: `~/.cursor/mcp.json`; most other clients take the same JSON.)

The switches can also be set from a terminal: `duet settings --mcp on --agent-operate on --agent-configure off`. `duet mcp --list-tools` prints the tools and their tiers.

## Tiers

| Tier | Default | What |
|---|---|---|
| read | on (while MCP is on) | everything that only reads |
| operate | **on** | reversible switches of the running system |
| configure | **off** | the stored configuration, apply, rollback, settings |
| never | n/a | install/uninstall/upgrade the helper, adopt/harden/undo takeover, kernel install/rollback, stopping or uninstalling services, removing duet, the agent switches themselves |

The switches live in the helper's settings (`agent` in `settings.json`), read on every tool call, so they apply to every client at once and take effect without restarting anything. A refused tool says which switch is off. `admin_only` tells the agent where in the app the person does a "never" action.

With `DUET_READ_ONLY=1` in its environment, `duet mcp` refuses every operate and configure tool.

## Tools

Every result is JSON text. Write tools return `previous` and `undo` (the call that puts it back).

**read**

| Tool | Does |
|---|---|
| `status` | helper, mihomo (mode, TUN, the node of each selector group), stored config (subscription, overrides, last apply, drift), Tailscale, last health, the tiers. Works without the helper (reports it). |
| `version` | duet, helper, kernel, Tailscale; `check_updates` asks GitHub for the latest kernel and geo files |
| `health` | the helper's checks; `run=true` runs them now |
| `doctor` | `duet doctor` in-process (when the helper is down) |
| `proxies` | groups, current node, members, last delays; `group` for one with its chain |
| `connections` | open connections summarized: top destinations, rules, chains, processes |
| `traffic` | throughput over ~3 s and totals |
| `logs` | `source=duet` (the helper's events) or `source=mihomo` (live log for ~3 s, `level`) |
| `subscriptions` | id, name, active, interval, last fetch/error (never the URL) |
| `layers` | the overrides; `id` returns one with its content (credentials redacted) |
| `history` | applied configs; `diff=<id>` (and `against=<id>`) gives a semantic diff |
| `preview` | what apply would do: diff against the running config, `mihomo -t`, culprits, `preview_hash` |
| `takeover_plans` | the takeover plans (read only) |
| `tailnet_peers` | peers, IPs, path (direct/derp/peer-relay), exit node options |
| `agent_activity` | what agents changed, newest first |
| `admin_only` | where the person does an action agents never get |

**operate**: `set_mode`, `select_node`, `delay_test`, `health_egress_run` (two GETs to an IP-echo address; operate because it sends traffic out), `set_tun`, `tailscale_up`, `tailscale_down`, `set_exit_node`, `clear_exit_node`, `refresh_subscription`, `flush_fakeip`.

**configure**: `layer_put`, `layer_remove`, `layer_enable`, `layer_order`, `sub_put`, `sub_activate`, `sub_remove`, `apply`, `history_rollback`, `settings_update` (no agent fields).

`apply` always takes two calls: without `preview_hash` it returns the preview (diff, validation, reload impact) for the agent to show the person; with that `preview_hash` it applies exactly that config, or refuses if the overrides changed since. The result has `history_id` and `rollback_id`; `history_rollback id=<rollback_id>` undoes it. An invalid config is never written; one that breaks mihomo after reload is restored by the helper.

Managed overrides (Tailscale coexist, leak guard) cannot be edited or removed through `layer_*`; use `settings_update`.

**Resources**: `duet://status`, `duet://health`, `duet://config/summary` (mode, TUN, DNS, group/rule/node counts, overrides; no node details).

**Prompts**: `diagnose_connectivity` (symptom), `add_direct_rule` (domain).

## Examples

"Why is example.com slow?": `status` → `connections` (which chain) → `proxies group=…` → `delay_test group=…` → maybe `select_node` (undo given).

"Send example.org direct": `layer_put id=agent-direct content="+rules:\n  - DOMAIN-SUFFIX,example.org,DIRECT\n"` → `apply` → show the diff → `apply preview_hash=…`.

## Safety model

- **Tiers** as above; the agent cannot change them (`settings_update` has no agent field; the helper's `settings.update` is only sent with one by the app or `duet settings`).
- **Logged**: every operate/configure call that runs, whether it succeeds or fails, goes to the helper's agent log (`agent.record`: time, client name and version from MCP, tool, redacted arguments, result, undo). It shows in 日志 → duet (chip "Agent") and in 设置 → 本地 Agent → 最近操作. The log keeps the last 200 (`<state>/agent-activity.json`).
- **Redacted**: nothing leaves `duet mcp` without passing the redactor: the controller secret (and any secret the server has seen, such as a URL passed to `sub_put`) is replaced, values under credential keys (`password`, `uuid`, `secret`, `token`, `private-key`, `psk`, `auth`, `short-id`, …) become `<redacted>`, URL user info and query strings are stripped, proxy provider URLs lose their path, and token-shaped path segments in free text are hidden. Configs are shown as semantic diffs, which never render node contents. `core/agent` tests call every tool against fixtures full of secrets and fail if any appears in any output or log record.
- **Not a security boundary**: an agent that runs as the person can run the CLI (or anything else) directly. The tiers keep a well-behaved agent inside what the person agreed to; they do not contain a hostile one. The helper itself only serves the admin group.

## CLI `--json`

Every helper command prints the helper's result (`core/rpc` types; field names are snake_case and only grow) with `--json`:

| Command | Shape |
|---|---|
| `status`, `version`, `doctor` | `rpc.Status`-based snapshot, versions object, `health.Report` |
| `layer list\|get\|put\|rm\|enable\|disable\|order\|restore` | `[]rpc.Layer`, `rpc.LayerContent`, `rpc.Layer`, `{}` … |
| `sub list\|put\|refresh\|activate\|rm` | `[]rpc.Subscription`, `rpc.Subscription`, `rpc.SubRefreshResult`, `{}` |
| `history list\|rollback` | `[]rpc.HistoryEntry`, `rpc.ApplyResult` |
| `compose --apply\|--preview` | `rpc.ApplyResult`, `rpc.ComposeResult` |
| `settings` | `rpc.Settings` (with `agent`) |
| `kernel …`, `service …`, `tailnet refresh`, `events` | the matching `rpc` result |
| `helper status\|info\|inventory\|health\|plans\|adopt\|discard\|secure-controller\|harden\|undo` | the matching `rpc` result |
| `helper uninstall --dry-run`, `helper upgrade --check` | the plan / the version check |
| `helper install`, `helper restart`, `helper upgrade` | `OpResult`: `{"op","ok","dry_run","output":[lines],"error"}` |
| `mcp --list-tools` | `[{"Name","Tier","Title"}]` |

Errors go to stderr and exit with status 1; with `--json` the `OpResult` commands still print their object.

## Agent skill

[docs/skill/SKILL.md](skill/SKILL.md) is a short skill an agent can install (Claude Code: copy it to `~/.claude/skills/duet/SKILL.md`).

## 中文摘要

在 duet 的 设置 → 本地 Agent 打开「允许 MCP」，即可让本机的 AI Agent 通过 `duet mcp` 查看状态（只读工具始终可用）；「操作」（默认开）允许切换模式、节点、TUN、Tailscale、出口节点等可撤销操作；「修改配置」（默认关）允许改覆写和订阅、应用、回滚。应用一定先返回预览，确认后才写入。安装、卸载、接管、内核更新只能在 duet 里做。每次改动都记录在 日志 → duet，所有输出都会去掉密钥、订阅地址和节点密码。
