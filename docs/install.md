# Installing Twinnel · 安装 Twinnel

[中文](#中文) · [English](#english)

Release builds are signed ad hoc, not with an Apple Developer ID, and are not notarized. macOS
therefore blocks the first launch of a downloaded copy; the steps below open it once, after which
it starts normally.

---

## 中文

### 下载

在 GitHub 的 Releases 页面下载：

| 文件 | 用途 |
|---|---|
| `Twinnel_<版本>_macos_universal.dmg` 或 `.zip` | Twinnel.app，macOS 15 及以上，Apple 芯片和 Intel 通用 |
| `twinnel_<版本>_darwin_<arm64\|amd64>.tar.gz` | 只要 macOS 命令行（`twinnel`、`twinnel-helper`） |
| `twinnel_<版本>_linux_<amd64\|arm64>.tar.gz` | Linux 命令行和后台助手（systemd） |
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

### Linux

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

Linux 上没有图形界面。

---

## English

### Download

From the GitHub Releases page:

| File | What it is |
|---|---|
| `Twinnel_<version>_macos_universal.dmg` or `.zip` | Twinnel.app for macOS 15 and later, Apple silicon and Intel |
| `twinnel_<version>_darwin_<arm64\|amd64>.tar.gz` | macOS command line only (`twinnel`, `twinnel-helper`) |
| `twinnel_<version>_linux_<amd64\|arm64>.tar.gz` | Linux command line and helper (systemd) |
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

### Linux

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

There is no GUI on Linux.
