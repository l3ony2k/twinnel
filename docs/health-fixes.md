# 健康检查：每个失败原因都能修，或说清为什么不能

规则（owner）：健康检查里每一项失败或警告，都要在界面里有对应的地方让用户调整并真正解决；Twinnel 解决不了的，要说明原因和用户在外面能做什么。

## 机制

- 每个检查在 warn/fail 时把原因写进 `Result.Reasons`（`core/health/reasons.go` 的 `Reason*` 代码，格式 `<检查>/<原因>`）。一个检查可以同时有几个原因。
- 每个原因在修复目录（`fixCatalog`，`health.FixFor(reason, platform)`）里有且只有一种修法，按平台可以不同（`reason@platform`）：
  - **一键修复**（auto）：`Action` 指定动作。先预览（会做哪些事、能否撤销），确认后经后台助手（或 app 自己的 mihomo/Tailscale 客户端）执行，然后重新检查并就地显示新结果；能撤销的给「撤销修复」。
  - **去调整**（guided）：`Target` 指向 app 里的页面和控件。按钮跳过去，控件闪一圈，底部一句话说要改什么。界面还空着（没装助手、没接管）时，按钮先变成缺的那一步。
  - **无法自动修复**（manual）：说明为什么 Twinnel 不能修，以及在外面怎么做；能打开的系统设置给「打开设置」按钮（macOS、Windows；只在报告来自本机时显示）。
- 文案：`core/health/fixtexts.go`，每个原因一句「问题」（Plain）和一句「怎么办」（How），中英双语，平台措辞不同的单独写。
- 执行：`core/healthfix`（Plan / Apply / Undo），app、`twinnel doctor --fix <check>`、MCP（`health_fixes` / `health_fix` / `health_fix_config`）共用同一套。
- 写系统的修复都经后台助手，沿用它的安全规则：配置改动走 compose.apply（`mihomo -t` 校验、备份、失败回滚）；系统 DNS 走 dns.point（先备份，dns.restore 撤销，mihomo DNS 不应答时拒绝）。

## 一键修复的动作

| 动作 | 做什么 | 撤销 |
|---|---|---|
| `tailscale-coexist-override` | 必要时打开「Tailscale 共存」覆写；tailnet.refresh 按当前 tailnet 重新生成；compose.apply | 原来关着：关回去再应用；原来开着：history.rollback 到修复前最新的一条 |
| `leak-guard-override` | 把有问题的敏感策略组加进「防泄漏」的组；compose.apply | 组列表改回原样再应用 |
| `controller-loopback` | 加覆写 `controller-loopback`：`external-controller: 127.0.0.1:<原端口>`；compose.apply | 删掉这个覆写再应用 |
| `controller-secret` | controller.secure：新随机密钥，存助手，应用 | 不能撤销（旧密钥不保留）；预览里说明 |
| `system-dns` | dns.point（预览逐条列出 服务：旧 DNS → 新 DNS） | dns.restore |
| `mode-rule` | mihomo 切回 rule 模式 | 切回原模式 |
| `exit-node-off` | Tailscale 停用出口节点 | 重新选回原出口节点 |
| `tun-stack-gvisor` | 加覆写 `tun-stack`：`tun.stack: gvisor`；compose.apply（TUN 重建，网络断一两秒） | 删掉这个覆写再应用 |
| `restart-mihomo` / `restart-tailscaled` | service.restart（网络/tailnet 断几秒，预览里说明） | 无需撤销 |
| `egress-probe` | health.egress 再测一次出口 | 无需撤销 |
| `rerun` | 重新运行检查 | 无需撤销 |

## 原因 → 修法 → 用户看到什么

「问题」和「怎么办」是 macOS 中文措辞；「平台」一栏列出 Windows/Linux 修法不同或另有措辞的原因（英文见 `enFixTexts`）。

| 原因 | 问题（行上显示） | 修法 | 怎么办（预览 / 提示 / 解释） | 平台 |
|---|---|---|---|---|
| `check-panicked` | 这项检查自己出错了，不代表你的网络有问题。 | 无法自动修复 `rerun` | 这是 Twinnel 的问题，不是设置能修的。重新检查一次；仍然出错就复制报告反馈给我们。 |  |
| `check-timeout` | 检查用了太久，没等到结果。 | 一键修复 `rerun` | 重新检查一次。网络刚切换或 mihomo 正忙时常见，多半下次就好。 |  |
| `controller-security/exposed` | mihomo 的控制接口对局域网开放，别的设备可能控制它。 | 一键修复 `controller-loopback` | 加一个覆写把控制接口只绑定到本机（127.0.0.1）并应用。可以撤销。 |  |
| `controller-security/no-secret` | mihomo 的控制接口没有密钥，本机任何程序都能控制它。 | 一键修复 `controller-secret` | 生成随机密钥并应用，密钥存在后台助手里。网页面板等其他工具之后需要新密钥。 |  |
| `fake-ip-filter-mode/wildcard-mode` | fake-ip 过滤的模式不是 blacklist，单标签名字会拿到假地址。 | 去调整 `config/overrides` | 在设置 fake-ip-filter-mode 的覆写里，把它改回 blacklist（或删掉这一行），保存并应用。 |  |
| `leak-guard-egress/failed` | 经 mihomo 的测试请求没成功，没测到出口。 | 一键修复 `egress-probe` | 再测一次。仍然失败多半是节点不可用，换一个节点再测。 |  |
| `leak-guard-egress/same-ip` | 走代理出去的 IP 和本机一样，这个节点实际没有代理。 | 去调整 `proxies/group` | 在节点页给这个策略组换一个节点，然后再测一次出口。 |  |
| `leak-guard/may-go-direct` | 敏感策略组可能切到 DIRECT，用本机网络出去。 | 一键修复 `leak-guard-override` | 打开「防泄漏」覆写并应用：从这些策略组里去掉 DIRECT，没节点可用时宁可断开。 |  |
| `leak-guard/mode` | mihomo 不在规则模式，防泄漏的策略组不起作用。 | 一键修复 `mode-rule` | 把 mihomo 切回规则模式。 |  |
| `leak-guard/no-group` | 要保护的策略组在当前配置里不存在。 | 去调整 `config/leak-guard` | 在「防泄漏」里选上你配置里真正要保护的策略组。 |  |
| `leak-guard/selected-direct` | 敏感策略组现在直接用本机网络出去。 | 一键修复 `leak-guard-override` | 打开「防泄漏」覆写并应用：从这些策略组里去掉 DIRECT，没节点可用时宁可断开。 |  |
| `magicdns/mihomo-wrong` | 经 mihomo 解析 tailnet 设备名会得到假地址或没有结果。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：tailnet 名字交给 Tailscale 解析，并为设备写好 hosts。 |  |
| `magicdns/no-system-resolver` | 系统没有把 tailnet 名字交给 Tailscale 解析。 | 无法自动修复 `` | Twinnel 不改 Tailscale 自己的设置。在 Tailscale 里打开「使用 Tailscale DNS」，或在终端运行 tailscale set --accept-dns=true。 | windows 另有措辞；linux 另有措辞 |
| `magicdns/system-wrong` | 用 tailnet 设备名访问时，系统解析出的不是设备的地址。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：tailnet 名字直接交给 Tailscale 解析，不用 fake-ip。 |  |
| `peer-short-names/none` | 规则里还没有 tailnet 设备的短名，用短名访问设备会走代理。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：按当前设备写入短名规则。 |  |
| `peer-short-names/outdated` | 规则里的设备短名和现在的 tailnet 对不上。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：按当前设备更新短名规则。 |  |
| `read-failed` | 读取系统信息时出错，这次没能检查完。 | 一键修复 `rerun` | 重新检查一次；如果一直出错，复制报告反馈给我们。 |  |
| `system-dns/not-mihomo` | 有网络连接的 DNS 没交给 mihomo，分流和防泄漏对它不起作用。 | 一键修复 `system-dns` | 把这些连接的 DNS 指向 mihomo。会先备份原来的设置，可以撤销。 | linux：无法自动修复；linux 另有措辞 |
| `system-dns/via-hijack` | 有网络连接的 DNS 没交给 mihomo，现在靠 TUN 劫持兜底。 | 一键修复 `system-dns` | 把这些连接的 DNS 直接指向 mihomo。会先备份原来的设置，可以撤销。 | linux：无法自动修复；linux 另有措辞 |
| `tailnet-resolver-binding/missing` | tailnet 名字的查询会从物理网卡发出，到不了 Tailscale。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：把查询绑定到 Tailscale 的网卡，并写好设备 hosts。 |  |
| `tailnet-resolver-binding/stale` | tailnet 名字的查询绑在 Tailscale 旧的网卡上，会超时。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：绑定到 Tailscale 现在的网卡。 |  |
| `tailnet-route/elsewhere` | 发往 tailnet 设备的流量走了别的网卡，没进 Tailscale。 | 一键修复 `restart-tailscaled` | 重启 tailscaled，让它把路由重新写一遍。期间 tailnet 连接会断几秒。 |  |
| `tailnet-route/into-mihomo` | 发往 tailnet 设备的流量进了 mihomo，而不是 Tailscale。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：把 tailnet 网段排除出 mihomo 的 TUN。 |  |
| `tailnet-route/lookup-failed` | 有的 tailnet 地址查不到路由，这次没法判断。 | 一键修复 `rerun` | 重新检查一次。 |  |
| `tailnet-route/no-interface` | Tailscale 在运行，但系统里找不到它的网卡。 | 一键修复 `restart-tailscaled` | 重启 tailscaled，让它重新建网卡和路由。期间 tailnet 连接会断几秒。 |  |
| `tailscale-direct-rules/global-mode` | mihomo 在全局模式，Tailscale 自己的连接也被送去了代理。 | 一键修复 `mode-rule` | 把 mihomo 切回规则模式。随时可以在概览里切回来。 |  |
| `tailscale-direct-rules/not-direct` | Tailscale 的连接会被规则送去代理，tailnet 可能连不上或很慢。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：把 Tailscale 的直连规则放到最前面。 |  |
| `tailscale-direct-rules/shadowed` | 前面有规则可能抢先截走 Tailscale 的连接。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：把 Tailscale 的直连规则放到最前面。 |  |
| `tun-route-exclude/missing` | mihomo 的 TUN 会把 tailnet 流量也抢过去。 | 一键修复 `tailscale-coexist-override` | 重新生成「Tailscale 共存」覆写并应用：它在 TUN 的排除网段里加上 tailnet。 |  |
| `tun-dataplane/dial-failed` | 经 TUN 的测试连接出错，这次没测出结果。 | 一键修复 `rerun` | 重新检查一次。 |  |
| `tun-dataplane/tcp-dead` | TUN 开着，但经它的 TCP 连接没有应答，上网会超时。 | 一键修复 `tun-stack-gvisor` | 改用 gvisor 栈：加一个覆写把 tun.stack 设为 gvisor 并应用，TUN 会重建，网络断一两秒。可以撤销。 |  |
| `tun-dataplane/tcp-dead-gvisor` | TUN 开着，但经它的 TCP 连接没有应答，上网会超时。 | 一键修复 `restart-mihomo` | 重启 mihomo，让它重建 TUN。期间网络会断几秒。 |  |
| `vpn-conflict/exit-node` | Tailscale 正在用出口节点，它和 mihomo 的 TUN 都想接管全部流量。 | 一键修复 `exit-node-off` | 停用出口节点。之后在 Tailscale 页可以随时再选回来。 |  |
| `vpn-conflict/other-tunnel` | 另一个隧道接管了默认路由，流量没有经过 mihomo。 | 无法自动修复 `` | 这个隧道属于别的 VPN 应用，Twinnel 不去关它。在那个应用里断开，或在 系统设置 → VPN 里关掉。 | windows 另有措辞；linux 另有措辞 |
| `vpn-conflict/other-vpn` | 另一个 VPN 已连接，它和 mihomo 抢同一条路。 | 无法自动修复 `` | 这个 VPN 是你另外装的应用，Twinnel 不去断开别人的连接。在那个 VPN 应用里断开，或在 系统设置 → VPN 里关掉它。 | windows 另有措辞；linux 另有措辞 |
| `vpn-conflict/tailscale-ne` | 装的是 App Store 版 Tailscale，它占着系统的 VPN 通道。 | 无法自动修复 `` | 这个版本由系统管理，Twinnel 改不了。退出它并改用开源版 tailscaled（Twinnel 的设置向导能装），或接受这个冲突。 |  |
| `vpn-conflict/tun-not-default` | mihomo 的 TUN 开着，却没拿到默认路由，流量没经过它。 | 一键修复 `restart-mihomo` | 重启 mihomo，让它重新接管默认路由。期间网络会断几秒。 |  |

## 不在这里的

- **内核 / 后台助手版本**：不是健康检查。版本落后由「内核」页和「设置 → 后台助手」（需要更新后台助手）处理，健康检查里没有对应项。
- **Tailscale 未登录 / 未运行**：健康检查遇到时是「跳过」，不是失败；登录由概览、托盘和 Tailscale 页的登录面板（`tsLoginPanel`）处理。
- **跳过**（mihomo 连不上、TUN 关着、没开 MagicDNS 等）不算问题，不给修复。

## 测试

- `core/health/reasons_test.go`：每个原因有修法、有中英文、每个平台都有；每个原因有一个合成场景真的触发它；任何 warn/fail 都带原因；`reasons.go` 里新加的 `Reason*` 常量没进目录就失败。
- `core/healthfix/healthfix_test.go`：每个原因 × 每个平台：一键修复在假助手上执行后检查变为通过，能撤销的撤销后复原；去调整有目标；无法自动修复有解释。另测失败回滚、没有助手时被挡住、同类原因合并。
- `cmd/app/healthfix_test.go`：每个原因在 app 里都有行上的文字和一个 app 认识的动作（去调整的目标有页面）。
- `cmd/cli/doctor_fix_test.go`、`core/agent/healthfix_test.go`：CLI 预览、询问、执行、重新检查、给出撤销；MCP 按层级分 operate / configure。
