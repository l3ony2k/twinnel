# TUN 数据通路：检查、修复与 Windows 默认 gvisor

## 起因

一次 Windows 11（build 26200）实机接管中，官方 mihomo v1.19.34 用 `tun.stack: mixed`（TCP 走 system 栈）时：

- TUN 网卡在、分裂路由对、`ping` TUN 网关有应答；
- **任何经 TUN 的 TCP 都超时**（DIRECT 和代理都一样），mihomo 的 debug 日志里看不到入站连接，重启服务也没用；
- 同样的请求走 mixed-port 正常；
- 健康检查 11 项 ok、1 项无关的 warn。

改成 `stack: gvisor` 立刻恢复。所有检查都只读控制面（路由、规则、DNS、控制接口），没有一项真的经过 TUN 发一次连接，所以「全绿但上不了网」。

## 检查 `tun-dataplane`

`core/health/checks_dataplane.go`，所有平台。

1. mihomo 的 TUN 没开：跳过。
2. 选测试目标：`1.1.1.1:443`，要求它的路由（`RouteGet`）进的是 mihomo 的 TUN（`tun.device`；没写时 macOS 认 `utunN`，其他平台认 `Meta`/`MetaN`）。不进 TUN（出口节点、被排除）时，用 mihomo DNS 解析 `connectivitycheck.gstatic.com` 得到的 fake-ip。都不进 TUN：跳过（`vpn-conflict` 会说默认路由的问题）。
3. 用普通 socket 连一次 TCP，2.5 s 超时，握手完成立刻关掉。两种栈都在本地完成握手（system 栈把连接转给 mihomo 自己的监听，gvisor 在用户态完成），所以正常时只花几毫秒。被拒绝（REJECT 规则回 RST）也说明通路正常。
4. 没有应答：fail，并补证据——ICMP 到 TUN 网关（默认 `198.18.0.2`）是否有应答，同一目标经 mihomo 的代理端口 CONNECT 后能否完成 TLS 握手（只握手不发数据，证明出站和规则正常）。

原因和修法（`docs/health-fixes.md` 有完整表格）：

| 原因 | 修法 |
|---|---|
| `tun-dataplane/tcp-dead`（栈不是 gvisor） | 一键 `tun-stack-gvisor`：加覆写 `tun-stack`（`tun:\n  stack: gvisor`），应用；撤销删掉它再应用。同 id 的覆写已存在时，失败会写回原内容，撤销改为回滚到修复前的历史。 |
| `tun-dataplane/tcp-dead-gvisor` | 一键 `restart-mihomo` |
| `tun-dataplane/dial-failed`（不是超时的出错） | 一键 `rerun` |

探测只在真实机器上发：平台 Env 的 `Options.Dataplane` 为空时检查跳过；助手、app、CLI 传 `health.NetProbes`（Windows 用 `windows.Probes()`，ICMP 走 `IcmpSendEcho`）。测试和截图用假数据（`fixture.Machine.TUNTCPDead`）。原始 ICMP 需要 root：app 进程内（非 root）跑时没有 ICMP 那条证据。

## Windows 默认 gvisor

`overrides.Base(goos)`：内置基础层在 Windows 上写 `tun.stack: gvisor`（带注释说明原因），其他平台仍是 `mixed`。

- 只在没有基础层时由 setup 写入；已有的基础层（包括接管时从原配置派生的）不改。
- 用户覆写在基础层之后，用户自己写的 `tun.stack` 照样生效。
- 已经装好的 Windows 机器不会被悄悄改：如果真遇到这个故障，`tun-dataplane` 会报出来并给一键修复。

## mihomo 的启动输出

故障期间 mihomo 启动阶段的输出没人看得到（控制接口的日志流要等 mihomo 起来才有，ProgramData 下的文件非管理员读不了）。现在：

- 服务定义把 stdout/stderr 写到 `<state>/logs/mihomo.out.log`（macOS/Linux 另有 `mihomo.err.log`）。Windows 上 `twinnel-svchost` 写这个文件，运行中超过 10 MB 就转到 `.1`；macOS/Linux 由助手每 10 分钟检查一次，超过 10 MB 复制到 `.1` 再截断（launchd/systemd 以追加方式打开，会接着写）。
- `service.log {name: mihomo}`：助手返回文件末尾（不够时连上 `.1`），URL 去掉 user info 和 query。
- 日志 → mihomo：先列文件里比日志流第一条更早的行（mihomo 没起来时就是全部），按所选级别过滤，清空/暂停同样作用；MCP `logs source=mihomo-output`。

## 根因候选（待 Windows 实机验证）

报告里的对照：Clash Party 2.0.5 自带的 `mihomo.exe` 也是 v1.19.34（go1.26.11），同机 mixed 正常。

1. **Windows 防火墙挡住了 system 栈的入站连接（首要怀疑）**。system 栈把 TUN 里的 TCP 改写后交给 mihomo 进程自己在 TUN 地址上的监听，这对 Windows 防火墙来说是一个「入站连接」。Twinnel 的 mihomo 作为服务在 `C:\ProgramData\Twinnel\kernels\mihomo\<版本>\mihomo.exe` 运行，没有任何放行规则，服务进程也不会弹出允许提示，入站会被静默丢弃；ICMP 由协议栈回应，不受影响；gvisor 在用户态直接从 wintun 读包，不经过这个入站过滤，所以立即恢复。Clash Party 首次运行时（以管理员身份）通常已经为它自己的 mihomo.exe 路径建了放行规则。这和所有现象吻合：TCP 全死、ICMP 正常、mihomo 日志里没有入站、重启无效、gvisor 立好、Clash Party 的 mixed 正常。
2. **构建不同**：报告推测 Clash Party 的内核是「重编译」的。但 MetaCubeX 的发布本身就用新版 Go 构建（`go.mod` 的 `go 1.20` 只是最低版本），Clash Party 很可能直接打包了官方发布文件，两边可能是同一个二进制或只差 `amd64`（GOAMD64=v3）与 `-compatible`（v1）变体。需要实测两边 `mihomo -v` 和 SHA256。
3. **配置差异**：Twinnel 的 `route-exclude-address` 含 IPv6 段而 `ipv6: false`；`dns-hijack` 含 `tcp://any:53`；设备名 `Meta` 与 `Mihomo`。Clash Party 工作时 exclude 和 hijack 都为空。
4. **OS 构建 26200 的网络栈 / wintun 变化**：需要另一台旧构建对照。

## 实机实验计划（Windows，管理员 PowerShell）

每步只改一个变量，测完恢复。测法：`Test-NetConnection 1.1.1.1 -Port 443`（经 TUN）、`curl.exe -x http://127.0.0.1:<mixed-port> https://www.gstatic.com/generate_204`（经代理端口）、`twinnel doctor --only tun-dataplane`。

1. 基线：删掉 `tun-stack` 覆写（或撤销修复）→ 复现 TCP 超时，确认 `tun-dataplane` 报 `tcp-dead`，证据里 ICMP 有应答、代理端口能连。
2. 防火墙（候选 1）：
   - `Get-NetFirewallApplicationFilter | ? Program -like '*mihomo*' | Get-NetFirewallRule` 看两边 mihomo.exe 的规则（Clash Party 的路径有、Twinnel 的没有即为强证据）。
   - 打开审计：`auditpol /set /subcategory:"Filtering Platform Connection" /failure:enable`，复现时看安全日志事件 5157/5152 里有没有 mihomo.exe 被丢弃的入站。
   - 临时加放行：`New-NetFirewallRule -DisplayName twinnel-mihomo-test -Direction Inbound -Program <Twinnel 的 mihomo.exe 全路径> -Action Allow`，mixed 下再测；测完 `Remove-NetFirewallRule -DisplayName twinnel-mihomo-test`。若这一步就恢复：后续由助手在安装/切换内核时为 mihomo 加规则（按服务 SID 或 `current` 路径，避免每个版本一条），并保留 gvisor 作 Windows 默认。
   - 也可临时关闭当前网络配置文件的防火墙做一次快速对照（测完立刻打开）。
3. 构建（候选 2）：以管理员运行两边 `mihomo.exe -v`，`Get-FileHash` 比较；把 Clash Party 的 `mihomo.exe` 临时装进 Twinnel 的内核目录（或反过来）各测一次。
4. 配置（候选 3，mixed 下逐项、各测一次）：`route-exclude-address` 置空 → 去掉其中 IPv6 项 → `dns-hijack` 置空 → `device: Mihomo`。
5. 每次失败后取 日志 → mihomo（或 `logs source=mihomo-output`）里的启动输出，看有没有 TUN/stack 初始化的报错或告警。
6. 结论回填本文档，并决定 Windows 默认是否改回 mixed（若防火墙规则能彻底解决）。
