# Testing on real machines

The automated tests only use fakes (`httptest` controllers, temp dirs, fake
executors). This page is the plan for the tests that need a real system: a
fresh macOS install, a Linux desktop, and Windows. Each one runs in a
throwaway VM (or Windows Sandbox) first. A real machine comes second, and
only one that is not anyone's daily driver until the VM run is clean.

Scripts (all under `scripts/e2e/`):

| Script | Runs on | Does |
|---|---|---|
| `macos-tart.sh` | the host Mac | creates, boots, feeds and deletes a macOS 26 VM with tart; runs the checklist in it over ssh |
| `macos-checklist.sh` | inside the macOS VM | read-only pass/fail table of the fresh setup |
| `linux-check.sh` | the Linux machine | read-only pass/fail table (systemd, routes, DNS, tray, status/doctor JSON) |
| `windows-check.ps1` | the Windows machine | read-only pass/fail table (services, pipe, routes, DNS, NRPT, status/doctor JSON) |

The check scripts never write, never use sudo or an elevated prompt, and never
start, stop or load a service. They save the raw JSON next to their output
(default `/tmp/twinnel-e2e`, `$XDG_RUNTIME_DIR/twinnel-e2e`,
`%TEMP%\twinnel-e2e`). That JSON names the tailnet, devices and nodes of the
machine it ran on: keep it out of the repo, and redact it before pasting it
into an issue.

Use a **test** subscription and a **test** tailnet (or an ephemeral,
pre-approved auth key) in every VM. Never paste a personal subscription URL
into a VM you will hand to anyone, and never commit anything a VM printed.

Each table row is PASS, FAIL, WARN or SKIP. A run passes when nothing FAILs.
WARN rows need a look but can be fine (for example, `system DNS` on Linux when
mihomo's dns-hijack already catches port 53).

---

## macOS: fresh machine in a tart VM

What it proves: a Mac that has never seen mihomo, Tailscale or Twinnel ends up,
after the first-run flow, with the helper running, mihomo running as a
LaunchDaemon with TUN up, system DNS on 127.0.0.1, tailscaled running and
logged in, the tailnet routed around mihomo's TUN, and every health check ok.

### Once: install tart

Apple silicon only. The image is about 25 GB; leave 60 GB free.

```sh
brew install cirruslabs/cli/tart
brew install hudochenkov/sshpass/sshpass   # optional: no password prompts for ssh/scp
```

The default image is `ghcr.io/cirruslabs/macos-tahoe-vanilla:latest` (macOS
26, only the `admin` user and Remote Login; login `admin` / `admin`). The
`-base` image adds Homebrew, which a fresh Mac does not have; use it only to
test the "Homebrew already there" path (`TART_IMAGE=ghcr.io/cirruslabs/macos-tahoe-base:latest`).

### Build what you test

Either a local build:

```sh
scripts/build-release.sh            # dist/Twinnel_<version>_macos_<arch>.zip and .dmg
```

or a published release (needs `gh auth login`):

```sh
scripts/e2e/macos-tart.sh fetch v0.3.0   # downloads, checks SHA256SUMS, copies into the VM
```

### Run

`tart run` opens the VM in a window, which takes the focus once while it
boots. Start it when you are at the machine. `up --headless` runs without a
window, which is enough for `check` but not for the GUI steps.

```sh
S=scripts/e2e/macos-tart.sh
$S create                            # clone the image into VM "twinnel-e2e"
$S up                                # boot, wait for ssh, print sw_vers
$S copy dist/Twinnel_<version>_macos_universal.zip   # or a Twinnel.app / .dmg
$S manual                            # prints the GUI checklist below
#   … do the GUI steps in the VM window …
$S check                             # read-only checklist in the VM; exit 1 on any FAIL
$S collect                           # optional: raw JSON to $WORK/out (keep it out of the repo)
$S destroy                           # stop and delete the VM and its scratch dir
```

`$S all <zip>` does create, up, copy and manual, waits for Enter, then runs
`check`. `VM=name` runs a second VM side by side.

### Manual GUI steps (in the VM window)

1. Open `/Applications/Twinnel.app`. The menu bar icon appears and the main
   window shows the first-run page.
2. Click 安装后台助手 / Install Helper and enter the admin password once.
3. Paste the test subscription URL. Twinnel downloads mihomo, composes the
   config and starts mihomo.
4. Accept the system DNS step (DNS → 127.0.0.1).
5. Let it install Tailscale, then log in to the test tailnet in the browser.
6. The flow ends on the status page; the menu bar icon shows the connected
   state by its shape, in both light and dark menu bars (System Settings →
   Appearance).
7. Open the flyout from the menu bar icon: mode, node and Tailscale rows show
   and answer.
8. Optional: turn on 登录时打开; System Settings → General → Login Items lists
   Twinnel.

The Gatekeeper path ("Open Anyway"): `copy` goes over ssh, so the app has no
quarantine flag. To test what a downloaded copy does, open Safari in the VM,
download the release `.dmg` from GitHub, and follow `docs/install.md` step 2.

### What `check` verifies

| Row | Reads |
|---|---|
| app installed, CLI runs, app running | `/Applications/Twinnel.app`, `twinnel version`, `pgrep Twinnel` |
| helper socket, LaunchDaemon, answers | `/var/run/twinnel/helper.sock`, `launchctl print system/com.l3ony2k.twinnel.helper`, `twinnel helper status --json` |
| mihomo LaunchDaemon plist and state | `/Library/LaunchDaemons/<label>.plist` and `launchctl print` (label from the helper, default `com.l3ony2k.twinnel.mihomo`) |
| mihomo controller | `twinnel status --json` → `Mihomo.Reachable` |
| TUN enabled, interface up, internet routed into TUN | `Mihomo.Config.tun`, `ifconfig <device>`, `route -n get 1.1.1.1` |
| system DNS, name resolution | `scutil --dns` resolver #1, `dscacheutil -q host` |
| tailscaled, logged in, tailnet bypasses TUN | `pgrep tailscaled` + launchd label, `Tailscale.State.BackendState`, `route -n get 100.100.100.100` |
| status level, health | `twinnel status --json` → `Level`; `twinnel doctor --json`, one row per check |
| setup status | `twinnel setup status --json`, when the build has it |

`macos-checklist.sh` refuses to run outside a VM (`kern.hv_vmm_present`)
unless `TWINNEL_E2E_ALLOW_HOST=1`. It stays read-only on a host too, but its
output then names the real tailnet.

---

## Linux: Arch with Hyprland, and KDE Plasma

Two passes on each desktop: a hermetic GUI run that changes nothing on the
system, then a real install. Do the real install in a VM first (virt-manager
or GNOME Boxes with an Arch ISO, one VM with Hyprland and one with Plasma),
then on a spare machine.

### Prerequisites

```sh
sudo pacman -S --needed go git jq iproute2      # jq or python3 for linux-check.sh
# Hyprland: a bar with a tray (waybar with the "tray" module), or the tray icon has nowhere to go.
# KDE Plasma: the system tray is there by default.
```

### Pass 1: hermetic GUI run (no system changes)

Everything runs as your user, from a scratch dir, against a fake mihomo
controller, a scratch helper on a temp socket, and a synthetic tailnet. Nothing
is installed, no service is touched, no DNS or route changes.

From the repo root:

```sh
S=$HOME/.cache/twinnel-e2e/hermetic        # scratch dir, outside the repo
TS=$PWD/cmd/app/testdata/shots-tailscale-status.json
mkdir -p $S/home $S/state $S/empty
go build -o $S/devfake ./cmd/helper/internal/devfake
go build -o $S/twinnel-helper ./cmd/helper
go build -o $S/twinnel ./cmd/cli
go build -o $S/twinnel-app ./cmd/app      # needs the Linux GUI (feat/linux-gui) merged

# 1. fake controller (invented groups and nodes)
$S/devfake -listen 127.0.0.1:19894 &

# 2. scratch helper: temp socket, file secrets, your own group, synthetic tailnet,
#    no host health checks, no takeover probing
$S/twinnel-helper --state-dir $S/state --socket $S/twinnel.sock --foreground \
  --secrets file --group "$(id -gn)" \
  --tailnet-status-file $TS --no-health --no-controller-owner --tailnet-iface none \
  --takeover-services-dir $S/empty --takeover-home $S/home &

# 3. point the scratch helper at the fake controller: stop it (kill %2), set
#    "controller": "127.0.0.1:19894" under "mihomo" in $S/state/settings.json,
#    and start it again as in step 2.

# 4. the app, read-only first
TWINNEL_READ_ONLY=1 TWINNEL_SOCKET=$S/twinnel.sock TWINNEL_CONTROLLER=127.0.0.1:19894 \
  TWINNEL_TAILSCALE_STATUS_FILE=$TS TWINNEL_MIHOMO_CONFIG=$S/empty/none.yaml \
  HOME=$S/home $S/twinnel-app
```

Check by hand, on Hyprland and on Plasma, in light and dark:

1. The tray icon appears, monochrome, and its states differ by shape.
2. Clicking it opens the flyout; mode, node and Tailscale rows show the
   synthetic data (invented node names, the synthetic tailnet).
3. The main window opens from the flyout; every sidebar page renders.
4. Cursor: arrow on buttons, rows, toggles; pointing hand only on links;
   I-beam on text fields.
5. Quit from the tray menu; no process is left (`pgrep -a twinnel`).

Then once more with writes against the fakes (`TWINNEL_READ_ONLY=0`): switch a
node, change the mode, toggle a layer. The fake controller and the scratch
helper take the writes; the system does not change.

Clean up: `kill %1 %2`, then `rm -rf $HOME/.cache/twinnel-e2e/hermetic`.

### Pass 2: real install

In the VM first. Needs systemd.

```sh
# from a release …
tar -xzf twinnel_<version>_linux_amd64.tar.gz && cd twinnel_<version>_linux_amd64
./install.sh --dry-run          # read what it will do
sudo ./install.sh               # twinnel → /usr/local/bin, twinnel-helper service started
# … or from source: scripts/build-release.sh, then the same from dist/
```

Log out and in again if `install.sh` added you to a group, so `twinnel` can use
the socket without root. Then set the machine up, either in the GUI's
first-run page (subscription URL, DNS, Tailscale install and login) or on the
command line:

```sh
twinnel setup                   # interactive: subscription, kernel, DNS, Tailscale
sudo tailscale up               # only if setup did not log in; use the test tailnet
```

Check:

```sh
scripts/e2e/linux-check.sh            # in the desktop session (tray rows included)
scripts/e2e/linux-check.sh --no-gui   # over ssh or on a server
```

| Row | Reads |
|---|---|
| CLI runs, socket group | `twinnel version`, `id -nG` (sudo, wheel or twinnel) |
| helper unit, socket, answers | `systemctl is-active twinnel-helper`, `/var/run/twinnel/helper.sock`, `twinnel helper status --json` |
| mihomo unit, controller | `systemctl is-active twinnel-mihomo` (name from the helper), `Mihomo.Reachable` |
| TUN enabled, interface up, internet routed into TUN | `Mihomo.Config.tun`, `ip link show dev <device>`, `ip route get 1.1.1.1` |
| system DNS, name resolution | `/etc/resolv.conf`, `resolvectl dns`, `getent hosts example.com` |
| tailscaled, logged in, tailnet bypasses TUN, policy rules | `systemctl is-active tailscaled`, `BackendState`, `ip route get 100.100.100.100` (want `tailscale0`), `ip rule` table 52 |
| status level, health | `Level`, `twinnel doctor --json` one row per check |
| desktop session, tray host, app running | `XDG_CURRENT_DESKTOP`, `org.kde.StatusNotifierWatcher` on the session bus, `pgrep` |

Then repeat the hand checks from pass 1 against the real setup, and reboot
once: after login, run `linux-check.sh` again; everything should come back on
its own.

Uninstall at the end and check that the system is as before:

```sh
sudo ./install.sh --uninstall --purge
systemctl status twinnel-helper twinnel-mihomo   # both "could not be found"
cat /etc/resolv.conf                             # the DNS from before setup
```

---

## Windows

Windows 11 (or 10) in **Windows Sandbox** first: it starts clean and throws
everything away when closed. If the TUN adapter (wintun) or Tailscale does not
install in the Sandbox, use a VM instead: Hyper-V on a Windows host, or UTM
with Windows 11 on Arm on a Mac (test an `arm64` build there). Take a snapshot
before installing so you can return to a fresh state.

### Build

```sh
GOOS=windows GOARCH=amd64 go build -o dist/windows/twinnel.exe ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o dist/windows/twinnel-helper.exe ./cmd/helper
cp scripts/e2e/windows-check.ps1 dist/windows/
```

(Use the release's Windows asset instead once the release workflow builds one.)

### Sandbox config

Enable it once: Settings → System → Optional features → More Windows features →
Windows Sandbox (Windows Pro, Enterprise or Education). Save as
`twinnel.wsb` next to `dist\windows` and double-click it:

```xml
<Configuration>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>C:\path\to\twinnel\dist\windows</HostFolder>
      <SandboxFolder>C:\twinnel</SandboxFolder>
      <ReadOnly>true</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <Networking>Enable</Networking>
</Configuration>
```

### Steps (in the Sandbox or VM)

1. Open **Terminal (Admin)**.
2. Install the helper:
   `C:\twinnel\twinnel.exe helper install --helper-bin C:\twinnel\twinnel-helper.exe`
   (copies it to `C:\Program Files\Twinnel`, registers the `TwinnelHelper`
   service, opens `\\.\pipe\twinnel-helper`).
3. Set up: `& "$env:ProgramFiles\Twinnel\twinnel.exe" setup` with the test
   subscription (mihomo as the `TwinnelMihomo` service, TUN adapter `Meta`).
4. Install Tailscale from tailscale.com (or let setup do it) and log in to the
   test tailnet; turn on Use Tailscale DNS.
5. Once the GUI exists on Windows: open Twinnel, check the tray icon
   (monochrome), the flyout and the main window as in the Linux pass 1 list.
6. Run the check, from a normal (not elevated) prompt:

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\twinnel\windows-check.ps1
   ```

| Row | Reads |
|---|---|
| CLI runs | `twinnel version` |
| helper service, pipe, answers | `Get-Service TwinnelHelper`, `\\.\pipe\twinnel-helper`, `twinnel helper status --json` |
| mihomo service, controller | `Get-Service TwinnelMihomo` (name from the helper), `Mihomo.Reachable` |
| TUN enabled, adapter up, internet routed into TUN | `Mihomo.Config.tun`, `Get-NetAdapter`, `Find-NetRoute 1.1.1.1` |
| system DNS, name resolution | DNS servers of the physical default-route adapter, `Resolve-DnsName example.com` |
| Tailscale service, logged in, tailnet bypasses TUN, MagicDNS NRPT | `Get-Service Tailscale`, `BackendState`, `Find-NetRoute 100.100.100.100` (want `Tailscale`), `Get-DnsClientNrptPolicy` |
| status level, health | `Level`; `twinnel doctor --json` (SKIP while doctor is not available on Windows) |
| app running | `Get-Process Twinnel` |

7. Uninstall (`twinnel helper uninstall --purge` in the admin prompt), check
   that the services and the `Meta` adapter are gone and the adapter DNS is as
   before, then close the Sandbox (or revert the VM snapshot).

### A real Windows PC over SSH (the QA loop)

Once the Sandbox run is clean, the rounds on a real Windows 11 PC run from a
Mac over SSH. The PC needs OpenSSH Server with PowerShell 7 (`pwsh`) as the
login shell, and an administrator account (an OpenSSH login gets an elevated
token without a UAC prompt). The scripts never touch sshd, the firewall or
the remote-access path (Tailscale, when that is how you reach the PC).

| Script | Runs on | Does |
|---|---|---|
| `windows-deploy.sh HOST` | the Mac | builds the Windows app zip from main (refuses another branch), copies it to `%ProgramData%\TwinnelQA\deploy`, runs `install.ps1 -NoLaunch`, `twinnel helper upgrade`, prints `twinnel version` and `helper upgrade --check` |
| `windows-snapshot.ps1 snapshot` / `verify -Dir` | the PC (elevated) | saves the state before a test (config.export zip, settings, layers, status, doctor, DNS of every adapter, service states) into `%ProgramData%\TwinnelQA\snap-<ts>`; `verify` compares and checks HTTPS to a DIRECT and a PROXY target through the TUN |
| `windows-timebox.ps1 arm -Name N -Restore FILE -Seconds S` | the PC (elevated) | a one-shot SYSTEM scheduled task that runs the restore script after S seconds even when the SSH session dies; `run` restores early, `disarm` removes it |
| `windows-usersession.ps1 -Script FILE -Arguments 'a;b'` | the PC (elevated) | runs a script in the signed-in user's desktop session (one-shot Interactive task, hidden console) and prints its output |
| `windows-gui.ps1` | the user session (through the above) | steps: `launch[:K=V,...]`, `open:VALUE` (`Twinnel.exe --open=page:<id>`: a running copy turns to that page), `wheel:N` (scroll the window), `windows`, `shot:NAME`, `winshot:NAME`, `printshot:NAME` (the window drawn by PrintWindow: works on a locked console), `click:X,Y`, `wclick:X,Y`, `key:KEYS`, `front`, `wake`, `close` |

Rules that came out of round 1:

- `%ProgramData%\TwinnelQA` is made Administrators/SYSTEM only: the snapshot's
  export zip holds node credentials and subscription URLs. Screenshots name
  the tailnet and nodes: keep them outside the repo.
- An agent's `twinnel mcp` keeps `bin\twinnel.exe` open. Since fx-installps1,
  `install.ps1` renames running files aside itself (`*.old-<digits>`, the same
  names as `core/binswap`, removed on the next run) and swaps all or none;
  `windows-deploy.sh`'s own rename-aside is now redundant but harmless.
- `install.ps1` must run under Windows PowerShell 5.1 and PowerShell 7. CI's
  Windows job runs the whole script under both (install, upgrade while
  programs run from the install, dry runs, uninstall;
  `packaging/windows/install_windows_test.go`), and the app's 完全移除 path
  (`cmd/app/appremove_windows_test.go`), so the PC run is the last check of
  the installer, not the first. Round 2's crash (`@()` around a `New-Object`
  list) is now refused in every `.ps1` by `TestPowerShellTraps`.
- `install.ps1` stops a Twinnel running in its own session and starts it
  again. Over SSH the app runs in another session: it is left running on its
  renamed-aside old image; restart it in the user session
  (`windows-gui.ps1 launch`) to get the new version.
- Screenshots fail with "The handle is invalid" when the input desktop is not
  `Default`: a running screensaver (desktop `Screen-saver`) or a locked
  session. A non-secure screensaver can be ended (it comes back after the
  idle timeout); a locked session needs the owner. On a locked console
  `printshot:NAME` still captures the main window (the app draws it on
  request), but clicks, keys and the wheel do not reach it, and popups (menus,
  the tray flyout) cannot be captured: step with `open:page:<id>` only.
- 截图的尺寸必须等于窗口的物理像素尺寸（`windows` / `printshot` 那一行会打印
  尺寸和缩放比例）。150% 下 2575 DIP 宽的窗口是 3862 px。第 4 轮的
  `printshot` 在一个没有拿到 per-monitor DPI 感知的 PowerShell 里运行，
  位图按 2575×1417 建、PrintWindow 却按 3862×2125 画，只留下左上角三分之二，
  看起来像内容整体右移并被右边缘截断。脚本现在设置线程级 DPI 感知并核对；
  看到 `warning: not per-monitor DPI aware` 时尺寸是按窗口 DPI 换算的。
  (A capture must be the window's size in physical pixels; round 4's was
  cropped by a DPI-unaware capture, not the app.)
- `twinnel doctor` shows the helper's last report when it is under 60 s old
  and no state change came after it (the header says `后台助手 N 秒前的检查结果`).
  The helper bumps a state generation on every change it sees (mihomo
  stopped or started by anyone, the controller's owner or reachability, the
  DNS watchdog, an apply, dns point/restore) and runs the checks again; a
  report from an older generation is never reused. `doctor --fresh` still
  forces a run.
- Tests that break traffic (mixed TUN stack, stopping mihomo, a foreign
  controller on 9090) arm a time box first. The LAN and the tailnet are
  excluded from the TUN, so SSH normally survives; plan for it not to.
- Write the restore scripts so they are idempotent: the time box may run
  after the test already restored.
- Twinnel's files are UTF-8. Read them over SSH as bytes or set
  `[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)` first:
  `Get-Content` printed through the SSH console's code page shows Chinese as
  `?` and full-width punctuation as ASCII (`（`→`(`), which looks like a
  broken file but is the console (round 1, `health-egress.json`).
- Deploy main exactly from a work branch: build in a clean worktree of main
  (`git worktree add --detach <dir> main`, then `scripts/build-release.sh
  --no-app --targets "" --windows-app windows/amd64` there) and pass the zip
  with `windows-deploy.sh --zip`. Building in a branch with uncommitted
  changes gives a `-dirty` version.
- Step through pages with `open:page:<id>` (ids in `cmd/app/window.go`
  allPages) instead of clicking by coordinates. A dev launch against a scratch
  `TWINNEL_SOCKET` opens no window by itself; add `TWINNEL_OPEN=1` to see the
  first-run wizard.
- A process started from the SSH session (`Start-Process`) dies when that
  session ends. Something that must outlive the call (a throwaway mihomo, an
  MCP client holding `twinnel.exe`) runs as a scheduled task.
- The upgrade from a helper whose service host reports revision 0 restarts
  mihomo once (a second or two offline): arm a time box that starts the
  Twinnel services before deploying.
- Verify the installer as users run it: `windows-deploy.sh` runs the zip's
  `install.ps1` under `powershell.exe` (5.1); run the same unpacked script
  under `pwsh` once more, each time with an MCP client holding
  `bin\twinnel.exe` (a user-session scheduled task that runs `twinnel mcp`
  and keeps stdin open), and check one "was in use" line per file.
