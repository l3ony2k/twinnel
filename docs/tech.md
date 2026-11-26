# 技术选型（v1）

日期：2026-11-26。基于 REQUIREMENTS.md v1。采用上一轮推荐的方向：App 和一个以 root 运行的小守护进程分开。

## 1. 总体架构

```
┌──────────────── 用户态 ────────────────┐        ┌──────────── root ────────────┐
│  App（MyGo，原生 Go UI）                │        │  helper 守护进程（Go）        │
│   ├─ 菜单栏图标 + Flyout 弹出面板       │  Unix  │   ├─ 服务管理（launchd/…）     │
│   ├─ 主窗口                             │ socket │   ├─ 内核下载/校验/替换/回滚   │
│   └─ 只读直连：mihomo API、Tailscale    │◀──────▶│   ├─ 覆写合成 → mihomo -t → 写入│
│       LocalAPI（看状态、切节点、up/down）│ JSON   │   ├─ 系统 DNS / 路由读取与修复 │
└────────────────────────────────────────┘  RPC   │   ├─ 健康检查（定时 + 事件）   │
                                                  │   └─ 通知事件推送给 App         │
        ┌── mihomo（官方二进制，LaunchDaemon）─────┘
        └── tailscaled（官方/brew，LaunchDaemon）
```

为什么这样分：
- **权限**：只有 helper 以 root 运行，只在安装时要一次管理员密码，之后 App 不再要求输入密码。
- **App 不在时网络照常**：健康检查、订阅定时更新、Tailscale 设备变化后重新合成覆写都在 helper 里跑，App 关掉也照常进行。
- **跨平台**：平台相关的代码全部放在 helper 的 `platform/` 层（darwin / linux / windows），App 基本不用改。以后在 Linux 上可以只装 helper 加一个 CLI，不装界面也能用。
- **CLI 能力自然获得**：同一套 RPC 再包一个命令行工具，方便脚本调用和远程排查（比如 `ssh` 到另一台机器上看状态）。

App 和 helper 分工：
- 频繁的只读操作和轻量操作（mihomo 的流量、连接、切节点、测延迟；Tailscale 的状态、up/down），App 可以直接调用 mihomo REST/WebSocket 和 Tailscale LocalAPI，不必经过 helper，延迟更低。mihomo 的 secret 由 helper 生成，写进 Keychain，App 从 Keychain 读取。
- 凡是要写系统、写配置、动二进制的操作，一律经过 helper。

## 2. 选型

| 部分 | 选择 | 理由 / 备注 |
|---|---|---|
| 语言 | **Go**（App、helper、CLI 统一） | mihomo、Tailscale、MyGo 都是 Go；可以直接用 Tailscale 官方 Go 客户端；交叉编译方便 |
| 界面 | **MyGo 原生 UI**（不用 webview） | 已确认：`NewTray` + `NewFlyout{Tray, Popover: true}` 在 macOS 上是系统 popover，Windows 上是任务栏弹出面板；`ActivationPolicyAccessory` 可以隐藏 Dock 图标；自带 chart（流量图）、code editor（编辑覆写 YAML）、diff viewer（配置历史对比）、`ui.Icon`（SVG 图标）。纯 Go，不需要 Bun，也没有前端构建链 |
| 托盘标题 | `tray.SetTitle` + `MonospacedTitle` | 可以在菜单栏直接显示网速，宽度不跳 |
| mihomo 控制 | 官方 REST API + WebSocket（`/traffic`、`/logs`、`/connections`、`/proxies`、`/configs`） | 不引入 mihomo 源码，只用它对外的接口 |
| Tailscale 控制 | `tailscale.com/client/local`（LocalAPI 官方 Go 客户端） | brew/开源 tailscaled、Standalone 版、App Store 版都能连；状态订阅用 IPN bus（`WatchIPNBus`），不需要轮询 |
| 配置合成 | `gopkg.in/yaml.v3` 的 Node 层面合并 | 保留字段顺序和注释，合成后的配置仍然便于人工阅读 |
| 配置校验 | 调用**官方** `mihomo -t -d <dir> -f <file>` | 校验和运行用的是同一个内核，不会出现自己的校验逻辑和内核不一致 |
| 热重载 | `PUT /configs?force=true` | 失败时不写入，旧配置继续运行 |
| 下载 | GitHub Releases API + 官方 checksum；Tailscale 用 pkgs.tailscale.com | 下载走 mihomo 的代理端口（如果正在运行），没有可用代理时直连 |
| 凭证 | macOS Keychain（`security` CLI 或 purego 调 Security.framework）；以后 Linux 用 Secret Service，Windows 用 Credential Manager | 订阅 URL、controller secret |
| App ↔ helper | Unix socket + JSON-RPC（Windows 以后用 named pipe） | socket 文件权限 `0660`，组为 `admin`；每个请求校验对端 uid（`getpeereid`） |
| helper 安装 | v0.x：`osascript … with administrator privileges` 执行一次安装脚本，写入 LaunchDaemon | 不需要签名，开源用户可以自己编译。以后有 Developer ID 再改成 `SMAppService`（MyGo 支持用 Swift native module 调用） |
| 状态存储 | helper：`/Library/Application Support/<app>/`（配置、覆写、历史、内核）；App：用户目录只放界面偏好 | 历史配置用简单的带时间戳文件 + 索引，不需要数据库 |
| 图标 | 菜单栏：单色 template PNG（16pt / 32px）；界面：用 **kubai-icons 线性风格**统一绘制，以 SVG 嵌入 `ui.Icon` | 两个 icon skill 已装到全局：`kubai-icons`、`draw-icons`。界面图标先用 kubai-icons 的线性风格（和文字搭配、线宽可调）；状态图标（mihomo / Tailscale / 健康）需要专门画 |
| 发布 | GitHub Releases；`.app`（arm64 + amd64）；先不签名，README 写清楚怎么放行 | 以后再考虑公证和自动更新（MyGo 有 updater 插件） |

## 3. 覆写格式

推荐用 **Mihomo Party 风格的 YAML 覆写**作为主格式，并且能导入 Clash Verge Rev 的 Merge 文件：

```yaml
# 覆写：Tailscale 共存（由 App 生成，标记为 managed，不手动编辑）
dns:
  fake-ip-filter+:            # 追加到数组末尾
    - '+.ts.net'
  nameserver-policy:
    '+.ts.net': [system]
tun:
  route-exclude-address+:
    - 100.64.0.0/10
    - fd7a:115c:a1e0::/48
+rules:                       # 插到数组开头
  - PROCESS-NAME,tailscaled,DIRECT
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
```

- 语义照搬 Clash Party（原 Mihomo Party）的 `src/main/utils/merge.ts`（已核对源码）：数组字段 `+key` 前插、`key+` 追加，不带符号时整体替换；对象字段默认深度合并，`key!` 整体替换；键名本身带 `+`/`!` 时用 `<key>` 包起来。
- Clash Verge Rev 的 `prepend-rules` / `append-rules` / `prepend-proxies` 等字段，导入时自动转成上面的语法。
- JS 脚本覆写暂不支持。真的需要时用 goja（纯 Go 的 JS 引擎），放到 v0.3 之后。
- 覆写有两类：**managed**（App 生成，比如 Tailscale 共存、防泄露、本机端口和 DNS 设置；界面上只能开关）和 **user**（用户手写，可以用 code editor 编辑）。

合成顺序：订阅 → 本机基础设置（端口、DNS、TUN、controller）→ 用户覆写（按顺序）→ Tailscale 共存 → 防泄露约束（最后执行，确保不会被前面的覆写绕过）。

## 4. 健康检查的实现

- 每一项检查是一个 Go 接口：`ID / Platforms / Run(ctx) Result{Status, Evidence, Fix}`，放在 helper 里。
- macOS 的数据来源：
  - `route -n get 100.100.100.100`、`netstat -rn`：tailnet 路由走哪个 utun
  - `scutil --dns`：resolver 列表
  - `networksetup -listallnetworkservices` / `-getdnsservers`：各网络服务的 DNS
  - `scutil --nc list`：系统 VPN 和 NetworkExtension 状态
  - mihomo `/configs`、`/rules`，以及最终配置文件本身
  - Tailscale LocalAPI 的 Status：peer 直连还是走 DERP、exit node
  - 发往 mihomo `127.0.0.1:53` 的 DNS 查询：`*.ts.net` 是否返回 fake-ip
- 触发方式：定时（60 秒）+ 网络变化（macOS 上用 `scutil` 的通知或 SystemConfiguration 的 watch；先用轮询路由表的 hash 代替）+ 手动。
- 结果一键导出为 Markdown 文本。

## 5. 待验证的风险

| 风险 | 怎么处理 |
|---|---|
| MyGo 很新（2026-09 创建，现在 v0.10，一周发好几个版本），API 可能变 | 锁定版本；界面层写薄，业务逻辑全部放在 helper 和 `core` 包里；第一步先做一个 spike 验证 flyout、托盘标题、图表能用 |
| root helper 是提权面：本机其他程序可能通过它下载、执行任意二进制 | RPC 只开放固定的操作，不接受任意路径或命令；只下载官方来源并校验 checksum；socket 权限加上对端 uid 校验 |
| tailnet 设备短名 | v0.3 做实验：用 `nameserver-policy` 把 tailnet 搜索域和单标签域名交给 100.100.100.100，看能不能去掉逐个设备的 DOMAIN 规则；不行就继续由 App 根据 peers 自动生成 |
| App Store 版 Tailscale 的 LocalAPI 只有同一用户能访问 | helper 是 root，可能连不上；这种安装方式下由 App 自己读 Tailscale 状态再交给 helper |
| 本机还没装 Go | 开工前 `brew install go`（需要满足 MyGo 要求的版本） |

## 6. 仓库结构草案

```
cmd/
  app/        MyGo 界面（菜单栏 + 主窗口）
  helper/     root 守护进程
  cli/        命令行工具
core/         平台无关逻辑：状态模型、覆写合成、健康检查定义、下载器、RPC 协议
platform/
  darwin/     launchd、networksetup、scutil、Keychain
  linux/      （以后）systemd、resolved、ip route
  windows/    （以后）
overrides/    内置覆写模板（Tailscale 共存、防泄露）
assets/icons/ SVG 图标源文件 + 菜单栏 template PNG
```

## 7. 第一步：spike（1–2 天）

1. 装 Go，用 MyGo 的 native 模板跑起来：托盘 + flyout + 主窗口，flyout 里显示 mihomo 当前模式、PROXY 节点，以及 Tailscale 在线 peer 数（直接调 API，先不写 helper）。
2. 菜单栏标题实时显示网速（mihomo `/traffic` WebSocket）。
3. 把现有的手工补丁拆成两份覆写，在临时目录里合成，用 `mihomo -t` 校验，和现在线上的 `config.yaml` 做 diff。期望除了顺序和注释外完全一致。**这一步不动线上配置。**

spike 通过后进入 v0.1：写 helper、接管现有安装、做健康检查。

## 更新记录

- 2026-11-26：Linux 平台层落地。`platform/linux` 实现了健康检查的 `Env`（`ip -j`、`resolvectl` / `resolv.conf`、`nmcli`、`getent`）、接管用的 systemd `Probe`，以及 `platform/linux/systemd` 服务管理（unit 渲染、`systemctl` 计划、`systemctl show` 解析）；`duet doctor`、`duet takeover plan` 可以在 Linux 上运行。共用的解析代码移到 `platform/common`。helper 还没有接入 systemd。整体结构见 `docs/architecture.md`。
