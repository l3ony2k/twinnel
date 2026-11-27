# Getting DNS back by hand · 手动恢复 DNS

[中文](#中文) · [English](#english)

---

## 中文

接管后，Twinnel 会把系统 DNS 指向 mihomo（`127.0.0.1`，Windows 上还有 `::1`）。这样 mihomo 一停，整台电脑就解析不了域名：网页打不开，但用 IP 地址还能连。

### 通常不需要手动处理

后台助手一直在看着 mihomo 的 DNS。mihomo 停止、反复崩溃或长时间没有响应时，后台助手会把系统 DNS **临时改回接管前的设置**，菜单栏图标变成警告，并发一条通知「DNS 已临时恢复为原来的设置」。mihomo 恢复正常一段时间后，会自动改回 mihomo。你在这期间手动改过的网络服务（网卡）不会被覆盖。

在 Twinnel 里：

- **重启 mihomo**：总览的提示里，或 菜单栏 → 应急恢复。mihomo 恢复后 DNS 自动改回。
- **保持原设置**：不再改回 mihomo，系统 DNS 留在接管前的设置（和 应急恢复 → 恢复系统 DNS 一样）。之后想再交给 mihomo：健康检查 → 系统 DNS → 修复。
- **查看日志**：日志 → Twinnel 里能看到什么时候、因为什么改的。

只有后台助手也没在运行（或已被卸载）时，才需要下面的手动步骤。

### 先看接管前是什么

Twinnel 改 DNS 前把原来的值记在状态目录的 `dns-backup.json` 里（需要管理员权限才能读）：

| 系统 | 文件 |
|---|---|
| macOS | `/Library/Application Support/twinnel/dns-backup.json` |
| Windows | `C:\ProgramData\Twinnel\dns-backup.json` |

`"servers": []` 或没有 `servers` 表示“自动”（由路由器 / DHCP 分配）。拿不准就选自动，几乎总能上网。

### macOS

图形界面：系统设置 → 网络 → Wi-Fi（或你在用的网络）→ 详细信息… → DNS，删掉 `127.0.0.1`，点“好”。列表为空就是自动。

终端（每个网络服务一行，`Wi-Fi` 换成你的服务名）：

```sh
networksetup -listallnetworkservices           # 看有哪些网络服务
networksetup -getdnsservers Wi-Fi              # 看现在的 DNS
sudo networksetup -setdnsservers Wi-Fi empty   # 改回自动
# 或者改回原来的手动地址，例如：
# sudo networksetup -setdnsservers Wi-Fi 192.0.2.53
sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder   # 清缓存
```

让 mihomo 重新跑起来（DNS 之后可以再交给它）：

```sh
twinnel service restart mihomo
# 后台助手也不在时：
sudo launchctl kickstart -k system/com.l3ony2k.twinnel.mihomo
```

接管来的 mihomo 服务名可能不同，用 `twinnel service status mihomo` 看 label。

### Windows

图形界面：设置 → 网络和 Internet → Wi-Fi（或以太网）→ 硬件属性 → DNS 服务器分配 → 编辑 → 选“自动(DHCP)”→ 保存。

PowerShell（以管理员身份运行；`Wi-Fi` 换成你的适配器名）：

```powershell
Get-DnsClientServerAddress                                          # 看每个适配器现在的 DNS
Set-DnsClientServerAddress -InterfaceAlias "Wi-Fi" -ResetServerAddresses   # IPv4 和 IPv6 都改回自动
# 或者改回原来的手动地址，例如：
# Set-DnsClientServerAddress -InterfaceAlias "Wi-Fi" -ServerAddresses 192.0.2.53
ipconfig /flushdns
```

没有 PowerShell 的 DnsClient 模块时用 netsh：

```bat
netsh interface ipv4 set dnsservers name="Wi-Fi" source=dhcp
netsh interface ipv6 set dnsservers name="Wi-Fi" source=dhcp
```

让 mihomo 重新跑起来：

```powershell
twinnel service restart mihomo
# 后台助手也不在时（管理员）：
Restart-Service TwinnelMihomo
```

### Linux

Twinnel 目前不在 Linux 上改系统 DNS，所以 mihomo 停了也不会影响解析，除非你自己把 DNS 指向了 mihomo。那样的话，用 NetworkManager 改回自动：

```sh
nmcli connection show                                  # 找到连接名
sudo nmcli connection modify "<连接名>" ipv4.ignore-auto-dns no ipv4.dns ""
sudo nmcli connection up "<连接名>"
```

或者 systemd-resolved：`sudo resolvectl revert <网卡>`。

### 卸载时

`twinnel helper uninstall`（以及 App 里的卸载）会先按 `dns-backup.json` 把 DNS 改回去，再删掉后台助手。

---

## English

After takeover, Twinnel points the system DNS at mihomo (`127.0.0.1`, plus `::1` on Windows). If mihomo stops, the whole computer can't resolve names: web pages don't load, though IP addresses still work.

### Usually there is nothing to do

The helper keeps an eye on mihomo's DNS. When mihomo stops, crash-loops or stops answering for a while, the helper **temporarily puts the system DNS back to what it was before takeover**, the menu bar icon shows a warning, and you get a notification, "DNS temporarily back to earlier settings". Once mihomo has been healthy for a little while, the DNS goes back to mihomo by itself. A network service (adapter) you changed by hand in between is left as you set it.

In Twinnel:

- **Restart mihomo**: in the overview's banner, or menu bar → Recovery. The DNS switches back once mihomo recovers.
- **Keep Earlier DNS**: stay on the pre-takeover DNS for good (same as Recovery → Restore System DNS). To hand the DNS to mihomo again later: Health → System DNS → Fix.
- **View Log**: Logs → Twinnel shows when and why it changed.

You only need the steps below when the helper isn't running either (or was removed).

### What it was before

Before changing the DNS, Twinnel records the earlier values in `dns-backup.json` in its state directory (readable by an administrator):

| System | File |
|---|---|
| macOS | `/Library/Application Support/twinnel/dns-backup.json` |
| Windows | `C:\ProgramData\Twinnel\dns-backup.json` |

`"servers": []`, or no `servers`, means automatic (assigned by the router / DHCP). When in doubt, choose automatic: it almost always works.

### macOS

Settings: System Settings → Network → Wi-Fi (or the network you use) → Details… → DNS, remove `127.0.0.1`, click OK. An empty list means automatic.

Terminal (one line per network service; replace `Wi-Fi` with yours):

```sh
networksetup -listallnetworkservices           # list network services
networksetup -getdnsservers Wi-Fi              # current DNS
sudo networksetup -setdnsservers Wi-Fi empty   # back to automatic
# or back to earlier manual servers, for example:
# sudo networksetup -setdnsservers Wi-Fi 192.0.2.53
sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder   # flush the cache
```

Get mihomo running again (you can hand it the DNS again afterwards):

```sh
twinnel service restart mihomo
# when the helper isn't running either:
sudo launchctl kickstart -k system/com.l3ony2k.twinnel.mihomo
```

An adopted mihomo service may have another label: `twinnel service status mihomo` shows it.

### Windows

Settings: Settings → Network & internet → Wi-Fi (or Ethernet) → Hardware properties → DNS server assignment → Edit → Automatic (DHCP) → Save.

PowerShell (run as administrator; replace `Wi-Fi` with your adapter's name):

```powershell
Get-DnsClientServerAddress                                          # each adapter's DNS
Set-DnsClientServerAddress -InterfaceAlias "Wi-Fi" -ResetServerAddresses   # IPv4 and IPv6 back to automatic
# or back to earlier manual servers, for example:
# Set-DnsClientServerAddress -InterfaceAlias "Wi-Fi" -ServerAddresses 192.0.2.53
ipconfig /flushdns
```

Without PowerShell's DnsClient module, use netsh:

```bat
netsh interface ipv4 set dnsservers name="Wi-Fi" source=dhcp
netsh interface ipv6 set dnsservers name="Wi-Fi" source=dhcp
```

Get mihomo running again:

```powershell
twinnel service restart mihomo
# when the helper isn't running either (administrator):
Restart-Service TwinnelMihomo
```

### Linux

Twinnel does not change the system DNS on Linux yet, so a stopped mihomo doesn't affect name resolution unless you pointed the DNS at it yourself. If you did, set NetworkManager back to automatic:

```sh
nmcli connection show                                  # find the connection's name
sudo nmcli connection modify "<connection>" ipv4.ignore-auto-dns no ipv4.dns ""
sudo nmcli connection up "<connection>"
```

Or with systemd-resolved: `sudo resolvectl revert <interface>`.

### When uninstalling

`twinnel helper uninstall` (and uninstalling from the app) first writes the DNS back from `dns-backup.json`, then removes the helper.
