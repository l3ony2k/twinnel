# UI glossary: 简体中文 → English

The English UI (`core/i18n/en.go`, `core/health/strings_en.go`) uses these
terms. Keep them when adding strings, and add new terms here.

## Style

- **Title case** (as macOS) for page and window titles, sidebar rows,
  buttons, menu items and toolbar actions: "Test All", "Check for Updates",
  "Roll Back to This", "Open Main Window", "Install Helper".
- **Sentence case** for section and card headings, steps, field labels,
  status values, messages, hints, toasts and health check titles:
  "Menu bar", "What takeover changes", "Controller address", "Not running",
  "Health: 2 failed". `TestHeadingsSentenceCase` (core/i18n) checks the
  heading keys.
- One name per action: the button and the page it opens say the same
  ("Install Helper"); prose says "the helper".
- **Counts** use a plural group that `i18n.F` resolves by the `%d` before
  it: "%d issue{s} found", "%d prox{y|ies}". Chinese writes none.
  `TestEnglishPluralForms` fails on a fixed plural after `%d`.
- **Errors** never show the system's text: `i18n.PlainError` /
  `Strings.ErrorText` word it (coded helper messages, network and file
  reasons, the generic line); the raw text is for 复制详情 only. Messages
  name a subscription or an override by its display name, never its ID.
- Health check titles name the subject ("VPN conflicts"), not the passing
  condition.
- An ellipsis (…) only where a dialog or picker follows the action
  ("Import from File…", "Delete…"), and on in-progress states ("Testing…").
- Curly quotes around names in messages: Delete subscription “Home”?
- Short, plain, no internals (AGENTS.md copy rule). Address the user as
  "you"; name the computer "this Mac" in the app, "this computer" in health
  checks (they also run on Linux).
- Keep proper nouns and config syntax as written: mihomo (lower case),
  Tailscale, tailscaled, tailnet, DERP, MagicDNS, TUN, fake-ip, DIRECT,
  REJECT, GLOBAL, GEO, LaunchDaemon, Homebrew, config keys
  (`route-exclude-address`, `nameserver-policy`, …).
- Running-text punctuation comes from the table: `ListSep` ("、" / ", "),
  `LabelSep` ("：" / ": "). Don't hard-code Chinese punctuation in code.

## Pages and areas

| 中文 | English |
|---|---|
| 总览 | Overview |
| 策略组 | Proxy Groups (page); proxy group (in text) |
| Tailscale 设备 | Tailscale Devices |
| 健康检查 | Health (page); health check (in text) |
| 日志 | Logs |
| 订阅与覆写 | Subscriptions & Overrides (page title, running text); **Subs & Overrides** (sidebar row and 显示 menu, `NavConfig`: shorter to balance the sidebar) |
| 历史 | History |
| 内核 | Core |
| 接管 | Takeover (page); Take Over (button) |
| 设置 | Settings |
| 关于 | About |
| 配置（侧栏分组） | Configuration |
| 应用（侧栏分组） | App |
| 入门 | Getting Started |
| 常见问题 | FAQ |

## Terms

| 中文 | English |
|---|---|
| 后台助手 | helper (Helper in headings) |
| 控制接口 | controller |
| 控制接口地址 | controller address |
| 密钥 | secret |
| 订阅 / 订阅地址 | subscription / subscription URL |
| 覆写 | override (never 层 / 分层 in Chinese UI) |
| 订阅原文 | original subscription (the file takeover used as the base; never 基础订阅) |
| 基础设置 | base settings (the base-stage layer) |
| 用户覆写 | user overrides |
| 未应用的改动 / 放弃改动 | unapplied changes / Discard Changes |
| 还原为已应用版本 | Revert to Applied Version |
| 接管前 | Before duet (history chip) |
| 托管（覆写） | managed (override) |
| 阶段 | stage |
| 基础 / 用户 | Base / User |
| Tailscale 共存 | Tailscale coexistence |
| 防泄漏 | leak guard |
| 敏感策略组 | sensitive groups |
| 节点 | node |
| 主策略组 | main group |
| 出口 / 出口节点 | exit / exit node |
| 测速 / 延迟 | Test / latency |
| 全部测速 | Test All |
| 超时 / 未测 | Timeout / Untested |
| 模式：规则 / 全局 / 直连 | Mode: Rule / Global / Direct |
| 直连 / DERP 中继 / 节点中继 / 未连接 | Direct / DERP relay / Peer relay / Not connected |
| 预览与应用 | Preview & Apply |
| 应用 | Apply / applied |
| 回滚 | roll back (verb), rollback (noun) |
| 合成 | compose / composed config |
| 现有配置 | current config |
| 接管计划 | takeover plan |
| 重载 | reload |
| 内核（mihomo 程序） | core |
| 外部程序 | external binary |
| 通道：稳定版 / Alpha | Channel: Stable / Alpha |
| 服务 | service |
| 定义文件 | definition |
| 仅预演 | dry run |
| GEO 数据库 | GEO Databases |
| 控制面板（external UI） | Dashboard (external UI) |
| 只读 | Read-only |
| 通过 / 警告 / 失败 / 跳过 | Passed / Warning / Failed / Skipped |
| 依据 | Evidence |
| 修复建议 | How to fix |
| 需要处理 / 其他检查 | Needs attention / Other checks |
| 一切正常 / 需要注意 / 出现故障 / 正在检测 | All good / Needs attention / Something's wrong / Checking |
| 系统通知 | Notifications |
| 语言 / 界面语言 / 跟随系统 | Language / App language / System |
| 本机 | this Mac (app), this computer (health checks) |
| 设备短名 | device short names |
| 解析器 | resolver |
| 更新 / 重启 / 卸载 / 查看日志（后台助手） | Update / Restart / Uninstall / View Log |
| 完全移除 duet | Remove duet Completely |
| 同时删除数据 | Also delete data |
| 登录时打开 | Open at login |
| 在程序坞中显示：打开窗口时 / 始终 / 从不 | Show in Dock: While the window is open / Always / Never |
| 打开 duet（程序坞菜单） | Open duet |
| 通用 | General |
| 需打包版 | Packaged app only |
| 订阅原文 | Subscription source |
| 现在 / 接管后 | Now / After takeover |
| 目标（运行中） | Target (running) |
| 换一个文件… | Choose Another File… |
| 下一步 | Next |
| 撤销接管 | Undo Takeover |
| 停止使用 duet | Stop Using duet |
| duet 在本机安装的内容 | What duet Installed on This Mac |
| 导出配置 | Export Config |
| 复制详情 / 复制诊断信息 | Copy Details / Copy Diagnostics |
| 待重启 | Restart pending |
| 外部程序（接管） / duet 管理 | External binary (adopted) / Managed by duet |
| duet 偏好设置 | duet preferences |
| 在 Finder 中显示 | Show in Finder |

## Language names

Each language is named in itself in the language menu: 简体中文, English.
