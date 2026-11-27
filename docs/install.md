# Installing Twinnel · 安装 Twinnel

[中文](#中文) · [English](#english)

Release builds are signed ad hoc, not with an Apple Developer ID, and are not notarized. macOS
therefore blocks the first launch of a downloaded copy; the steps below open it once, after which
it starts normally. The Windows build has no Authenticode signature either: SmartScreen warns
before the first run (see Windows below).

---

## 中文

### 下载

在 GitHub 的 Releases 页面下载：

| 文件 | 用途 |
|---|---|
| `Twinnel_<版本>_macos_universal.dmg` 或 `.zip` | Twinnel.app，macOS 15 及以上，Apple 芯片和 Intel 通用 |
| `twinnel_<版本>_darwin_<arm64\|amd64>.tar.gz` | 只要 macOS 命令行（`twinnel`、`twinnel-helper`） |
| `twinnel_<版本>_linux_<amd64\|arm64>.tar.gz` | Linux 命令行和后台助手（systemd） |
| `Twinnel_<版本>_linux_<amd64\|arm64>_app.tar.gz` | Linux 版 Twinnel（应用、命令行、后台助手） |
| `Twinnel_<版本>_linux_<amd64\|arm64>.AppImage` | 同一个应用，单个文件 |
| `PKGBUILD`、`twinnel-bin.install` | Arch Linux 软件包 |
| `Twinnel_<版本>_windows_<amd64\|arm64>.zip` | Windows 10 1809 及以上、Windows 11 的 Twinnel（x64 / Arm64），含命令行和后台助手 |
| `SHA256SUMS` | 校验文件 |

校验下载（可选）：

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

### macOS：安装 Twinnel.app

1. 打开 `.dmg`，把 Twinnel 拖到「应用程序」。用 `.zip` 的话，双击解压后把 `Twinnel.app` 移到 `/Applications`。
2. 第一次打开会被拦下（"无法验证开发者"或"Apple 无法检查其是否包含恶意软件"），因为这个版本没有付费签名和公证。任选一种方式放行：

   **方式 A：系统设置（macOS 15 及以上）**
   1. 双击 Twinnel，在提示框里点「完成」（不要点「移到废纸篓」）。
   2. 打开「系统设置 → 隐私与安全性」，滚动到「安全性」部分，Twinnel 旁边有「仍要打开」，点它。
   3. 再确认一次「仍要打开」，输入登录密码。

   从 macOS 15 起，按住 Control 点按 →「打开」不再能绕过这个提示，必须走上面的步骤。

   **方式 B：终端**

   ```sh
   xattr -dr com.apple.quarantine /Applications/Twinnel.app
   ```

   去掉下载时加上的隔离标记，之后直接双击就能打开。

3. Twinnel 出现在菜单栏。首次运行页里点「安装后台助手」，输入一次管理员密码。

更新：下载新版本，替换 `/Applications/Twinnel.app`，再按第 2 步放行一次；然后在 设置 → 后台助手 点「更新」。

### macOS：只用命令行

```sh
tar -xzf twinnel_<版本>_darwin_arm64.tar.gz
cd twinnel_<版本>_darwin_arm64
xattr -d com.apple.quarantine twinnel twinnel-helper 2>/dev/null || true   # 用浏览器下载时才需要
sudo install -d -m 0755 /usr/local/bin && sudo install -m 0755 twinnel /usr/local/bin/twinnel
twinnel helper install --helper-bin "$PWD/twinnel-helper"                  # 弹一次管理员密码框
twinnel version
```

### Linux：安装 Twinnel

需要 systemd，以及下面「运行库」里的库（桌面发行版一般都已装好）。

```sh
tar -xzf Twinnel_<版本>_linux_amd64_app.tar.gz
cd Twinnel_<版本>_linux_amd64_app
./install.sh --dry-run    # 先看会做什么，不改动任何东西
./install.sh              # 装到 ~/.local，再安装后台助手（弹一次管理员密码框）
```

- 应用装在 `~/.local/lib/twinnel`，`twinnel` 命令链接到 `~/.local/bin`，「应用程序」菜单里出现 Twinnel。
- `sudo ./install.sh`（或 `./install.sh --system`）给所有用户装到 `/usr/local`。
- `--no-helper` 只装应用；`--yes` 不再询问。
- 后台助手装到 `/usr/local/libexec/twinnel-helper`，服务是 `twinnel-helper.service`，数据在 `/var/lib/twinnel`。密码框来自 pkexec（polkit），没有就用 sudo。
- 更新：解压新版本，再运行一次 `./install.sh`（后台助手一并更新）。
- 卸载：`./uninstall.sh`（先卸载后台助手，还原 Twinnel 改过的设置）；`--keep-helper` 保留后台助手，`--purge` 同时删除 `/var/lib/twinnel`。

**AppImage**：`chmod +x Twinnel_<版本>_linux_amd64.AppImage` 后双击或直接运行。需要 FUSE 2（见下表）。
安装后台助手：`sudo ./Twinnel_<版本>_linux_amd64.AppImage cli helper install`。

**Arch Linux**：把 `PKGBUILD` 和 `twinnel-bin.install` 下载到同一个空目录，运行 `makepkg -si`
（它会下载并校验 `_app` 压缩包）。应用装在 `/opt/twinnel`，然后安装后台助手：
`sudo twinnel helper install --helper-bin /opt/twinnel/bin/twinnel-helper`。

#### 运行库

Twinnel 是纯 Go 程序，运行时加载这些库：

| 用途 | Debian / Ubuntu | Fedora | Arch | openSUSE |
|---|---|---|---|---|
| 窗口（GTK 3，含 Pango、Cairo、GdkPixbuf） | `libgtk-3-0`（Ubuntu 24.04 起 `libgtk-3-0t64`） | `gtk3` | `gtk3` | `libgtk-3-0` |
| 绘制（OpenGL / EGL） | `libepoxy0 libegl1` | `libepoxy mesa-libEGL` | `libepoxy libglvnd` | `libepoxy0 libEGL1` |
| 菜单栏图标 | `libayatana-appindicator3-1` | `libayatana-appindicator-gtk3` | `libayatana-appindicator` | `libayatana-appindicator3-1` |
| 管理员密码框 | `pkexec`（旧版本叫 `policykit-1`） | `polkit` | `polkit` | `polkit` |
| 通知（可选） | `libnotify4` | `libnotify` | `libnotify` | `libnotify4` |
| AppImage | `libfuse2`（Ubuntu 24.04 起 `libfuse2t64`） | `fuse-libs` | `fuse2` | `libfuse2` |

X11 和 Wayland 都支持（GTK 自己选）。GNOME 显示菜单栏图标需要 AppIndicator 扩展
（`gnome-shell-extension-appindicator`；Ubuntu 默认已开启）。

### Linux：只用命令行

需要 systemd。

```sh
tar -xzf twinnel_<版本>_linux_amd64.tar.gz
cd twinnel_<版本>_linux_amd64
./install.sh --dry-run    # 先看会做什么，不改动任何东西
sudo ./install.sh         # twinnel → /usr/local/bin，再安装并启动 twinnel-helper 服务
twinnel version
```

`install.sh` 会把 `twinnel-helper` 装到 `/usr/local/libexec/twinnel-helper`，写入
`/etc/systemd/system/twinnel-helper.service`（压缩包里的 `twinnel-helper.service` 是同一份，供查看），数据在
`/var/lib/twinnel`。socket 属于 `sudo` 组（没有就用 `wheel`，都没有就新建 `twinnel` 组），组内用户不用 root 就能用 `twinnel`。

卸载：`sudo ./install.sh --uninstall`（加 `--purge` 同时删除 `/var/lib/twinnel` 里的数据）。

### Windows

1. 解压 `Twinnel_<版本>_windows_amd64.zip`（Arm64 电脑用 `_arm64`）。
2. 在解压出的文件夹里打开 PowerShell，运行：

   ```powershell
   powershell -ExecutionPolicy Bypass -File .\install.ps1
   ```

   它把 Twinnel 装到 `%LOCALAPPDATA%\Programs\Twinnel`（`Twinnel.exe`，命令行和后台助手在 `bin\`），
   加开始菜单快捷方式，把 `bin\` 加进当前用户的 PATH，然后启动 Twinnel。不需要管理员权限。
   加 `-DryRun` 只打印步骤。`twinnel` 命令在这个窗口和之后新打开的终端里可用；已经开着的终端
   （以及 VS Code 这类编辑器）要重开才找得到它。Twinnel 的 设置 → 本地 Agent → 命令行工具 也能把
   `bin\` 加进或移出 PATH。

   更新时不用先关掉 Agent：正在运行的 `twinnel mcp` 继续用旧版本，重启后用新版本。要么全部换成新版本，
   要么出错时全部保持旧版本。正在运行的 Twinnel 会被重新启动（加了 `-NoLaunch` 也会）。
3. Twinnel 的图标在任务栏右侧的通知区域（可能收在 ^ 里）。第一次打开时点「安装后台助手」，Windows
   会弹一次「用户账户控制」，同意后后台助手作为 Windows 服务 `TwinnelHelper` 运行。

#### 两份后台助手

Windows 上有两份 `twinnel-helper.exe`：

| 位置 | 作用 |
|---|---|
| `%LOCALAPPDATA%\Programs\Twinnel\bin\twinnel-helper.exe` | 随安装包一起来的，`install.ps1` 每次安装或更新都会覆盖它 |
| `C:\Program Files\Twinnel\twinnel-helper.exe` | `TwinnelHelper` 服务实际运行的那份，普通用户改不了 |

安装后台助手时（第一次打开 Twinnel 的那一步，或 `twinnel helper install`），第一份被复制成第二份。
之后用 `install.ps1` 更新 Twinnel 只换了第一份；Twinnel 发现运行中的后台助手比自己旧时会提示
更新，在 设置 → 后台助手 → 更新（或 `twinnel helper upgrade`）同意一次「用户账户控制」，第一份就会
再复制过去并重启服务。`twinnel version` 会列出两份各自的版本，不一致时打印一行 warning。

这个版本没有代码签名。直接双击 `Twinnel.exe` 时 SmartScreen 会提示「Windows 已保护你的电脑」：点
「更多信息」→「仍要运行」。用 `install.ps1` 安装会去掉下载标记，之后不再提示。

卸载：在 Twinnel 的 设置 → 停止使用 Twinnel 里选「完全移除 Twinnel」；或者运行
`powershell -ExecutionPolicy Bypass -File "$env:LOCALAPPDATA\Programs\Twinnel\install.ps1" -Uninstall`。
两者都会先停掉从安装文件夹运行的 Twinnel 程序（包括 Agent 的 `twinnel mcp`）。mihomo 和 Tailscale 不受影响。

---

## English

### Download

From the GitHub Releases page:

| File | What it is |
|---|---|
| `Twinnel_<version>_macos_universal.dmg` or `.zip` | Twinnel.app for macOS 15 and later, Apple silicon and Intel |
| `twinnel_<version>_darwin_<arm64\|amd64>.tar.gz` | macOS command line only (`twinnel`, `twinnel-helper`) |
| `twinnel_<version>_linux_<amd64\|arm64>.tar.gz` | Linux command line and helper (systemd) |
| `Twinnel_<version>_linux_<amd64\|arm64>_app.tar.gz` | Twinnel for Linux (app, command line, helper) |
| `Twinnel_<version>_linux_<amd64\|arm64>.AppImage` | the same app in one file |
| `PKGBUILD`, `twinnel-bin.install` | Arch Linux package |
| `Twinnel_<version>_windows_<amd64\|arm64>.zip` | Twinnel for Windows 10 1809+ and Windows 11 (x64 / Arm64), with the command line and the helper |
| `SHA256SUMS` | checksums |

Check the download (optional):

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

### macOS: install Twinnel.app

1. Open the `.dmg` and drag Twinnel to Applications. With the `.zip`, double-click it and move
   `Twinnel.app` to `/Applications`.
2. The first launch is blocked ("cannot verify the developer" or "Apple could not verify … is free
   of malware"), because this build is not signed with a Developer ID or notarized. Allow it once,
   either way:

   **Option A: System Settings (macOS 15 and later)**
   1. Double-click Twinnel and click **Done** in the alert (not **Move to Trash**).
   2. Open **System Settings → Privacy & Security**, scroll to **Security**, and click
      **Open Anyway** next to the line about Twinnel.
   3. Click **Open Anyway** again and enter your login password.

   Since macOS 15, Control-click → Open no longer skips this alert; use the steps above.

   **Option B: Terminal**

   ```sh
   xattr -dr com.apple.quarantine /Applications/Twinnel.app
   ```

   This removes the quarantine flag the browser added; the app then opens with a double-click.

3. Twinnel appears in the menu bar. On the first-run page click **Install Helper** and enter an
   administrator password once.

To update: download the new version, replace `/Applications/Twinnel.app`, allow it once more as in
step 2, then click **Update** in Settings → Helper.

### macOS: command line only

```sh
tar -xzf twinnel_<version>_darwin_arm64.tar.gz
cd twinnel_<version>_darwin_arm64
xattr -d com.apple.quarantine twinnel twinnel-helper 2>/dev/null || true   # only if a browser downloaded it
sudo install -d -m 0755 /usr/local/bin && sudo install -m 0755 twinnel /usr/local/bin/twinnel
twinnel helper install --helper-bin "$PWD/twinnel-helper"                  # one administrator password dialog
twinnel version
```

### Linux: install Twinnel

Needs systemd and the libraries under "Runtime libraries" below (desktop distributions have them).

```sh
tar -xzf Twinnel_<version>_linux_amd64_app.tar.gz
cd Twinnel_<version>_linux_amd64_app
./install.sh --dry-run    # print every step; changes nothing
./install.sh              # into ~/.local, then the helper (one administrator password dialog)
```

- The app goes to `~/.local/lib/twinnel`, the `twinnel` command is linked into `~/.local/bin`, and
  Twinnel appears in the applications menu.
- `sudo ./install.sh` (or `./install.sh --system`) installs it for every user under `/usr/local`.
- `--no-helper` installs the app only; `--yes` does not ask.
- The helper goes to `/usr/local/libexec/twinnel-helper`, as the service `twinnel-helper.service`,
  with its data in `/var/lib/twinnel`. The password dialog is pkexec's (polkit), else sudo's.
- To update: extract the new version and run `./install.sh` again (it updates the helper too).
- To uninstall: `./uninstall.sh` (removes the helper first, which puts back what Twinnel changed);
  `--keep-helper` keeps the helper, `--purge` also deletes `/var/lib/twinnel`.

**AppImage**: `chmod +x Twinnel_<version>_linux_amd64.AppImage`, then run it. It needs FUSE 2 (see
the table). Install the helper with `sudo ./Twinnel_<version>_linux_amd64.AppImage cli helper install`.

**Arch Linux**: download `PKGBUILD` and `twinnel-bin.install` into an empty folder and run
`makepkg -si` (it downloads the `_app` archive and checks it). The app is in `/opt/twinnel`; then
install the helper: `sudo twinnel helper install --helper-bin /opt/twinnel/bin/twinnel-helper`.

#### Runtime libraries

Twinnel is pure Go and loads these libraries when it runs:

| For | Debian / Ubuntu | Fedora | Arch | openSUSE |
|---|---|---|---|---|
| windows (GTK 3, with Pango, Cairo, GdkPixbuf) | `libgtk-3-0` (`libgtk-3-0t64` on Ubuntu 24.04+) | `gtk3` | `gtk3` | `libgtk-3-0` |
| drawing (OpenGL / EGL) | `libepoxy0 libegl1` | `libepoxy mesa-libEGL` | `libepoxy libglvnd` | `libepoxy0 libEGL1` |
| the tray icon | `libayatana-appindicator3-1` | `libayatana-appindicator-gtk3` | `libayatana-appindicator` | `libayatana-appindicator3-1` |
| the password dialog | `pkexec` (`policykit-1` on older releases) | `polkit` | `polkit` | `polkit` |
| notifications (optional) | `libnotify4` | `libnotify` | `libnotify` | `libnotify4` |
| the AppImage | `libfuse2` (`libfuse2t64` on Ubuntu 24.04+) | `fuse-libs` | `fuse2` | `libfuse2` |

X11 and Wayland both work (GTK picks). On GNOME the tray icon needs the AppIndicator extension
(`gnome-shell-extension-appindicator`; on by default in Ubuntu).

### Linux: command line only

Needs systemd.

```sh
tar -xzf twinnel_<version>_linux_amd64.tar.gz
cd twinnel_<version>_linux_amd64
./install.sh --dry-run    # print every step; changes nothing
sudo ./install.sh         # twinnel → /usr/local/bin, then install and start the twinnel-helper service
twinnel version
```

`install.sh` installs the helper as `/usr/local/libexec/twinnel-helper` and writes
`/etc/systemd/system/twinnel-helper.service` (the `twinnel-helper.service` in the archive is the
same unit, for reading); data lives in `/var/lib/twinnel`. The socket belongs to the `sudo` group
(else `wheel`, else a new `twinnel` group), so members can use `twinnel` without root.

Uninstall: `sudo ./install.sh --uninstall` (`--purge` also deletes the data in `/var/lib/twinnel`).

### Windows

1. Unzip `Twinnel_<version>_windows_amd64.zip` (`_arm64` on an Arm64 PC).
2. Open PowerShell in the unzipped folder and run:

   ```powershell
   powershell -ExecutionPolicy Bypass -File .\install.ps1
   ```

   It installs Twinnel in `%LOCALAPPDATA%\Programs\Twinnel` (`Twinnel.exe`; the command line and the
   helper in `bin\`), adds a Start menu shortcut, puts `bin\` on your PATH and starts Twinnel. No
   administrator rights needed. `-DryRun` only prints the steps. The `twinnel` command works in that
   window and in terminals you open afterwards; terminals (and editors such as VS Code) that were
   already open must be restarted to find it. Settings → Local agents → Command line tool in Twinnel
   also adds `bin\` to PATH or removes it.

   You can update without closing your agents: a running `twinnel mcp` keeps the old version until it
   restarts. The update replaces every file or, if something fails, none. A running Twinnel is
   restarted (also with `-NoLaunch`).
3. Twinnel's icon is in the notification area of the taskbar (maybe behind the ^). On first run,
   click Install Helper: Windows shows one User Account Control prompt, and the helper then runs as
   the Windows service `TwinnelHelper`.

#### The two helper binaries

Windows has two copies of `twinnel-helper.exe`:

| Where | What it is |
|---|---|
| `%LOCALAPPDATA%\Programs\Twinnel\bin\twinnel-helper.exe` | shipped with the app; `install.ps1` replaces it on every install or update |
| `C:\Program Files\Twinnel\twinnel-helper.exe` | the one the `TwinnelHelper` service runs; users can't change it |

Installing the helper (the first screen of Twinnel, or `twinnel helper install`) copies the first to
the second. Updating Twinnel with `install.ps1` replaces only the first; when Twinnel sees that the
running helper is older than itself, it offers the update: Settings → Helper → Update (or
`twinnel helper upgrade`) asks for one User Account Control approval, copies the first over the second
and restarts the service. `twinnel version` lists both versions and prints a warning when they differ.

The build is not code-signed. Double-clicking `Twinnel.exe` directly shows SmartScreen's "Windows
protected your PC": click More info → Run anyway. `install.ps1` clears the downloaded-file mark on
what it installs, so the warning does not come back.

Uninstall: Settings → Stop Using Twinnel → Remove Twinnel completely in the app, or
`powershell -ExecutionPolicy Bypass -File "$env:LOCALAPPDATA\Programs\Twinnel\install.ps1" -Uninstall`.
Both first stop Twinnel's programs that run from the install folder (including agents' `twinnel mcp`).
mihomo and Tailscale are left alone.
