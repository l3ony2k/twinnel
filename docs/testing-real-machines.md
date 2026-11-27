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
  --tailnet-status-file $TS --no-health --tailnet-iface none \
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
