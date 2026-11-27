# Windows：从 Clash Party / Clash Verge 接管

给在 Windows 上把 mihomo 从 Clash Party（原 mihomo-party）或 Clash Verge Rev 换成 Twinnel 管理的人和 agent。按顺序做；每一步都能单独检查和撤销。文中的名字、路径和地址都是示例。

## 0 先看清现状（只读）

| 看什么 | 命令（PowerShell） | 说明 |
|---|---|---|
| 谁在跑 mihomo | `Get-CimInstance Win32_Process -Filter "Name like 'mihomo%'" \| Select ProcessId,ParentProcessId,ExecutablePath` | 父进程是 `Clash Party.exe` / `clash-verge.exe` 的就是对方的内核。提权进程的路径非管理员读不到，显示为空不代表没有 |
| 谁占着 9090 | `Get-NetTCPConnection -LocalPort 9090 -State Listen \| Select OwningProcess` | 9090 是 mihomo 控制接口的默认端口，Twinnel 也用它 |
| 混合代理端口 | `Get-NetTCPConnection -State Listen \| ? LocalPort -in 7890,7897,7899` | Clash Party 常用 7890，Clash Verge 常用 7897；Twinnel 的基础设置是 7899 |
| TUN 网卡 | `Get-NetAdapter \| ? InterfaceDescription -like '*Wintun*' -or Name -in 'Mihomo','Meta'` | Clash Party 的 TUN 网卡常叫 `Mihomo`，Twinnel（mihomo 默认）叫 `Meta` |
| 自启动 | `twinnel autostart`（经后台助手，能读计划任务） | 也可以看 `Get-ScheduledTask \| ? TaskName -like '*mihomo*'` 和 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` |
| 系统 DNS | `Get-DnsClientServerAddress -AddressFamily IPv4` | 记下原值（多半是 DHCP），接管后会指向 127.0.0.1 |

## 1 关掉对方的自启动（不然下次登录它会回来）

只停掉正在跑的进程不够：Clash Party 的自启动是**计划任务**（`mihomo-party`、`mihomo-party-run`），不在 Run 键里；它会在登录时重新拉起自己的 mihomo，抢回默认路由和 9090。

- 推荐：Twinnel 的「接管」页 → 「会自己启动的其他代理软件」→ 停用自启动；或终端里
  ```
  twinnel autostart                      # 列出，带 ID
  twinnel autostart disable '<ID>' ...   # 先列出要执行的命令，确认后由后台助手执行
  twinnel autostart enable '<ID>' ...    # 撤销
  ```
  后台助手以 SYSTEM 运行，停用计划任务不需要再点 UAC。它只停用，不删除：计划任务 `schtasks /Change /TN <名字> /Disable`，Run 键写 `StartupApproved`（任务管理器「启动应用」的同一个开关），值本身留着。
- 健康检查里的「其他代理软件自启动」也会提示，并有同样的一键修复和撤销。
- 手动（管理员 PowerShell）：`Disable-ScheduledTask -TaskName mihomo-party; Disable-ScheduledTask -TaskName mihomo-party-run`。

## 2 退出对方，不要误杀 Twinnel 的 mihomo

在对方软件里退出（托盘 → 退出），或者按父进程只结束对方的内核：

```powershell
$party = Get-CimInstance Win32_Process -Filter "Name = 'Clash Party.exe'"
Get-CimInstance Win32_Process -Filter "Name = 'mihomo.exe'" |
  ? { $party.ProcessId -contains $_.ParentProcessId } | % { Stop-Process -Id $_.ProcessId -Force }
$party | % { Stop-Process -Id $_.ProcessId -Force }
```

Twinnel 的 mihomo 的父进程是 `twinnel-svchost.exe`（服务 `TwinnelMihomo`），按名字 `mihomo.exe` 一把杀会连它一起杀掉。

## 3 9090 被别人占着时的 SAFE_PATHS 报错

如果对方的 mihomo 还占着 127.0.0.1:9090，Twinnel 的重载请求会发到它那里，得到：

```
reload: PUT /configs?force=true: 400 Bad Request: path is not subpath of home directory or SAFE_PATHS: …\Twinnel\mihomo\config.yaml
 allowed paths: [<对方的 work 目录>]
```

意思是「这个 mihomo 不是 Twinnel 的」：对方只允许读它自己的 work 目录。不要去改对方的 SAFE_PATHS；先做第 1、2 步让 9090 空出来，再在 Twinnel 里应用。

## 4 系统 DNS

接管后 Twinnel 把活动网卡的 DNS 指向 mihomo（127.0.0.1），原值记在后台助手里，卸载或 `dns.restore` 时还原。mihomo 不运行时整台机器解析不了域名；手动恢复（管理员）：

```powershell
Set-DnsClientServerAddress -InterfaceAlias "Wi-Fi" -ResetServerAddresses   # 网卡名按实际
```

更多见 [recovery.md](recovery.md)。

## 5 TUN：TCP 不通时用 gvisor

已知现象：某些 Windows 版本上，官方 mihomo 的 `tun.stack: mixed`（TCP 走 system 栈）会出现 ping 通、TCP 全超时，而控制面的检查都通过。Twinnel 在 Windows 上的基础设置已经默认 `stack: gvisor`，健康检查「TUN 数据面」会真的经 TUN 建一次 TCP 连接。从对方那里沿用了 `stack: mixed` 或 `system` 的覆写时，去掉它，或加一个覆写：

```yaml
tun:
  stack: gvisor
```

## 6 Tailscale

- `twinnel setup --step tailscale` 用官方 MSI 静默安装；只想先装不想马上运行：`--tailscale-install-only`（安装程序会启动服务，Twinnel 装完把它停掉，登录留到以后）。
- 安装被中断可能留下一个大小正常但**全是 0 的 `tailscale.exe`**：服务（`tailscaled.exe`）照常运行，命令行和托盘却报「文件或目录损坏且无法读取」。Twinnel 会检查文件头和 Authenticode 签名：`twinnel setup --step tailscale --repair-tailscale`（向导里是「修复 Tailscale」，健康检查「程序文件完整」里是一键修复）先用 `msiexec /fa` 修复，不行再重新安装。winget 的 `repair` 不支持这种安装包，`winget install --force` 效果相同。

## 7 权限：「没有权限」不是「不存在」

`%ProgramData%\Twinnel`（含 `mihomo\config.yaml` 和 `kernels\`）只有 SYSTEM 和管理员能读。普通 PowerShell 里 `Test-Path` 报 Access denied、直接运行内核报找不到，都是权限，不是文件缺失。`twinnel doctor` 会写明「没有权限读取」；完整的检查看 app 的健康页（由后台助手执行）或用管理员终端运行。

## 8 检查

1. `twinnel setup status`：只有全部步骤完成才会说 Setup complete，否则列出还差什么、怎么做。
2. `twinnel doctor`：没有失败；「其他代理软件自启动」通过。
3. 注销再登录一次，确认对方没有回来（9090 和默认路由仍是 Twinnel 的）。
