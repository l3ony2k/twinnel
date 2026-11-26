# mihomo + Tailscale 桌面管理助手：需求（v1）

日期：2026-11-26。v0 草稿加上用户的五点回答。技术选型下一步讨论（UI 已定：[MyGo](https://github.com/egoist/mygo)）。

## 0. 定位

**这是一个管理官方内核的助手，不是又一个 Clash 客户端。**

- 不内置、不魔改 mihomo 内核，也不内置 Tailscale。App 负责下载**官方发布**的 mihomo 和 Tailscale，或者接管机器上已有的安装，然后帮用户把它们以系统服务的方式跑起来、看清状态、做日常操作。
- 内核以系统服务的形式独立运行（macOS 上是 LaunchDaemon），App 只是控制面：App 退出或崩溃都不影响网络。用户卸载 App 后，内核和配置仍然能单独工作。
- 核心卖点是**在桌面端同时用好"翻墙代理 + Tailscale"**：两边都不抢系统 VPN 通道，App 负责生成并持续校验让它们共存所需的设置。
- 开源。第一阶段只解决作者自己的需求，但代码里不写死任何个人信息（设备名、订阅、路径），个人设置只存在本机。

## 1. 平台路线

| 阶段 | 平台 | 服务管理 | 路由 / DNS 相关 |
|---|---|---|---|
| 现在 | macOS（这台 Mac，arm64） | launchd | `networksetup`、`/etc/resolver`、`route`、utun |
| 以后 | Linux | systemd | systemd-resolved / resolv.conf、`ip route`/`ip rule`、tun |
| 最后 | Windows | Windows Service | wintun、NRPT、路由表 |

要求：功能逻辑（状态模型、健康检查的定义、覆写合成、下载升级）和平台相关部分（服务、DNS、路由、权限）分开写，Mac 版本不能把 macOS 专有的概念写进核心逻辑。健康检查每一项都要标明在哪些平台上适用。

## 2. 现状（作者这台 Mac，用来做第一批测试）

- mihomo：裸核 `~/mihomo-migration/bin/mihomo`，LaunchDaemon `com.mihomo.proxy`（root）。TUN + fake-ip，DNS 在 `127.0.0.1:53`，Wi-Fi DNS 手动设为 127.0.0.1。zashboard 作为 external-ui。
- Tailscale：Homebrew `tailscaled`，LaunchDaemon `sh.brew.tailscale`。MagicDNS 开着，不用 exit node。
- 共存手段：TUN `route-exclude-address` 排除 tailnet 网段；规则最前面放 `PROCESS-NAME,tailscaled,DIRECT`、tailscale/DERP 域名和 tailnet 网段的直连；`ts.net` 不走 fake-ip、交给 system 解析；tailnet 设备短名手写在规则里。
- 配置：订阅转换得到的静态 yaml + 手工补丁，订阅一更新补丁就丢。

App 第一版要能**原样接管这套现有安装**，不要求用户重装。

## 3. 界面形态（MyGo）

- **菜单栏小面板**：一个综合状态图标（绿 / 黄 / 红）。点开是一个小面板：两边的状态摘要、当前出口、健康检查结果、最常用的开关（模式、主策略组节点、Tailscale up/down、场景切换、应急恢复），以及"打开主窗口"。
- **主窗口**：完整的总览、策略组与节点、Tailscale 设备、健康检查详情、订阅与覆写、内核管理、日志、设置。
- 需要验证 MyGo 是否支持"挂在菜单栏图标下的弹出面板"（MyGo 有 tray、菜单、通知 API，但能否做成 popover 式窗口还不确定），不支持的话退回成托盘菜单加一个小窗口。

## 4. 功能需求

### 4.1 内核管理（安装 / 接管 / 升级）

**通用要求**
- 检测机器上已有的安装：路径、版本、来源、是否以服务方式运行。
- 可以"接管"已有安装（只记录路径，不移动文件），也可以由 App 下载安装到 App 管理的目录。
- 下载只从官方发布渠道获取，校验 checksum 和签名（有签名时），保留上一版本，升级失败自动回滚。
- 服务管理：生成、安装、卸载、启动、停止、重启；服务定义（plist / unit）可以预览。
- 显示"有新版本"，但默认不自动升级。

**mihomo**
- 来源：MetaCubeX/mihomo 的 GitHub Release；可选正式版 / Alpha；按平台和架构选对应文件。
- 附属文件：geo 数据库（geoip/geosite/ASN）、外部面板（zashboard 等，可选）。
- 配置目录、工作目录由 App 管理，或指向已有目录。

**Tailscale：所有安装方式都要兼容**

| 安装方式 | 平台 | 用不用系统 VPN 通道 | App 的处理 |
|---|---|---|---|
| 开源 `tailscaled`（brew / 官方静态二进制 / 包管理器） | mac / Linux / Win | 不用（自己的 tun） | **推荐方式**；可以下载、安装服务、升级 |
| 官方 Standalone / App Store 版 | macOS | 用 NetworkExtension | 能读状态、做日常操作；明确提示它和"不占 VPN 通道"的设计冲突，并给出迁移到 tailscaled 的引导 |
| 官方 Windows 客户端 | Windows | 服务 + wintun | 以后再定 |

- 无论哪种安装方式，状态读取和日常操作都通过 `tailscale` CLI / LocalAPI 完成，界面上不区分安装来源。
- 健康检查要根据 Tailscale 的安装方式调整检查项。

### 4.2 状态总览

**mihomo**：进程/服务状态、运行时长、最近崩溃与重启；模式；TUN（是否启用、设备名、是否拿到默认路由）；DNS（监听地址、系统 DNS 是否指向它）；各策略组的当前节点和延迟；实时速率、流量；出口 IP 和地区（可按策略组分别测）；内核版本、配置合成时间、订阅更新时间。

**Tailscale**：BackendState、账户、本机 IP 和名字、密钥过期时间；peer 列表（在线状态、IP、OS、直连还是 DERP、经过哪个 DERP）；exit node、MagicDNS、Shields Up、SSH 等开关；版本与来源。

### 4.3 共存健康检查（核心）

持续运行（30–60 秒一次，加上网络变化事件触发），每一项都给出：状态、证据（读到了什么）、修复建议，能自动修复的提供一键修复。

| 检查项 | 期望 |
|---|---|
| tailnet 路由 | `100.64.0.0/10`、`fd7a:115c:a1e0::/48` 走 Tailscale 的 tun，不进 mihomo TUN |
| TUN 排除网段 | mihomo `route-exclude-address` 包含 tailnet 网段 |
| tailscaled 直连 | tailscaled 的进程规则和控制面 / DERP 域名规则位于规则最前面，指向 DIRECT |
| MagicDNS | `*.ts.net`、tailnet 设备短名、100.x 反解由 Tailscale 解析，不返回 fake-ip |
| 设备短名同步 | 覆写里的设备名列表和当前 tailnet peers 一致 |
| 系统 DNS | 所有活动网络接口的 DNS 指向 mihomo（或确认由 dns-hijack 兜底） |
| 没有冲突的 VPN | 没有其他 NetworkExtension / VPN 抢默认路由；Tailscale 没开 exit node（或用户明确接受） |
| 打洞质量 | 用户标记的常用 peer 为直连；走 DERP 时给出可能的原因 |
| 防泄露 | 用户标记的敏感域名（如 Google / Meta 广告相关）出口不是本机真实 IP；敏感策略组没有可用节点时不回退 DIRECT |
| 控制接口安全 | mihomo external-controller 只监听本机并设置了 secret |

结果可以一键导出为纯文本，方便贴给别人或 AI 排查。

### 4.4 日常操作

- **mihomo**：切模式；选节点、测延迟；开关 TUN；开关系统代理（备用）；重载（先校验配置）/ 重启；实时日志；清 DNS / fake-ip 缓存；快速新增一条规则（写进覆写，不改订阅）；打开外部面板。
- **Tailscale**：up/down；登录 / 登出 / 切换账户；选择 exit node（切换前提示冲突）；复制设备 IP / 名字、ping、SSH（在终端里打开）、Taildrop；常用 prefs 开关。
- **场景**：一键切到"代理 + Tailscale"、"只用代理"、"只用 Tailscale"、"全部直连"。
- **应急恢复**：恢复系统 DNS、重新拉起服务、跑一次连通性测试。断网时也必须能用，不依赖任何网络请求。

### 4.5 订阅与覆写

概念：
- **订阅**：通用内容（节点、策略组、规则），来自远端，只读，可以刷新。可以有多个订阅，同一时间启用一个。
- **覆写**：每台机器本地的需求，叠加在订阅之上。可以有多个覆写，按顺序应用，每个可以单独开关。例子：
  - "Tailscale 共存"覆写：**由 App 内置并根据当前 tailnet 自动生成**（排除网段、直连规则、fake-ip-filter、nameserver-policy、设备短名），这就是 App 的核心能力
  - 本机端口、DNS、TUN、外部面板设置
  - 个人规则（如 Microsoft ISO 下载直连）
  - 防泄露约束（敏感组禁止 DIRECT，空组改成 REJECT）
- 覆写格式要兼容社区习惯：至少支持 YAML 合并（包括"在规则最前/最后插入"这类操作），JS 脚本覆写可以后面再考虑。参考 Clash Verge Rev / Mihomo Party 的覆写语义，方便用户迁移。

流程：订阅 + 覆写 → 合成最终配置 → `mihomo -t` 校验 → 备份旧配置 → 写入 → 通过 API 热重载。任何一步失败都不应用，旧配置继续运行。

其他要求：
- 订阅可以手动或定时更新；Tailscale 设备变化时自动重新合成共存覆写。
- 合成结果有历史版本，可以 diff、回滚。
- 订阅 URL 和 controller secret 当作凭证，存系统钥匙串（macOS Keychain，以后 Linux 用 Secret Service、Windows 用 Credential Manager），不进日志、不进备份明文、不进仓库。

### 4.6 通知

服务崩溃或反复重启；健康检查由通过变成失败；敏感策略组没有可用节点；Tailscale 掉线、需要登录、密钥快过期；有新版本（可关闭）。

## 5. 非功能需求

- **权限**：内核以 root（Windows 上是 SYSTEM）运行；写操作（装服务、改 DNS、替换二进制）需要特权助手或等价机制；只读状态和日常操作不应该每次都要求输入密码。
- **App 不在时网络照常**：App 只是控制面。
- **轻量**：常驻时空闲 CPU 接近 0；状态轮询 2–5 秒，健康检查 30–60 秒，出口 IP 检测更慢；优先用事件（mihomo WebSocket、Tailscale IPN bus、系统网络变化通知）代替轮询。
- **可回滚**：所有改动系统或配置的操作都先备份。
- **开源卫生**：仓库里不出现个人数据；自带的覆写模板是通用的，个人覆写只存在用户目录。
- **界面**：先做中文，文案集中管理，方便以后加英文。

## 6. 分期

- **v0.1 只读看板**：接管现有安装 → 菜单栏状态和主窗口总览 → 健康检查（只报告）。
- **v0.2 日常操作**：模式、节点、Tailscale up/down 和 exit node、重载、日志、场景切换、应急恢复。
- **v0.3 订阅与覆写**：合成流水线、内置 Tailscale 共存覆写、防泄露约束、历史和回滚。把作者现在的手工补丁迁移成覆写。
- **v0.4 内核管理**：下载、升级、回滚，服务安装，首次运行向导（在一台全新的 Mac 上从零配好"代理 + Tailscale"）。
- **之后**：Linux → Windows；面向其他用户的通用化。

## 7. 进入技术选型前要确认的问题

1. 验证 MyGo 能否做菜单栏弹出面板，以及纯 Go、不用 cgo 的情况下怎么做特权助手（macOS 的 SMAppService / 一个 root 的辅助守护进程）。
2. 控制面架构：App 直接调 mihomo API 和 Tailscale LocalAPI，还是自己再跑一个以 root 运行的小守护进程，App 只和它通信？后者也解决了权限问题，而且方便以后跨平台。
3. 覆写格式选哪种：自定义 YAML 合并语法，还是直接沿用 Clash Verge Rev 的格式。
4. Tailscale 共存覆写里的设备短名：继续用 DOMAIN 规则逐个写，还是改成用 nameserver-policy 把 tailnet 的搜索域交给 100.100.100.100，彻底不用维护设备名列表。
