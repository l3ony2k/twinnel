# Twinnel on Windows: design

Status: design for the first Windows port (2026-11-28). Everything here is
cross-compiled (`GOOS=windows`, amd64 and arm64) and unit-tested with fakes on
macOS/Linux; nothing has run on a real Windows machine yet. `HANDOFF.md` of
the `feat/windows-platform` branch lists what needs a real box.

Target: Windows 10 1809+ and Windows 11, amd64 and arm64. Same shape as on
macOS: the app (MyGo), a privileged helper, the CLI, and mihomo + Tailscale
side by side without either taking the other's routes or DNS.

## 1. What maps to what

| | macOS | Windows |
|---|---|---|
| Privileged process | LaunchDaemon as root | Windows service `TwinnelHelper`, LocalSystem |
| Helper binary | `/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper` | `%ProgramFiles%\Twinnel\twinnel-helper.exe` |
| State dir | `/Library/Application Support/twinnel` | `%ProgramData%\Twinnel` (DACL: SYSTEM + Administrators only) |
| Helper log | `<state>/logs/helper.log` | same (`%ProgramData%\Twinnel\logs\helper.log`) |
| IPC | unix socket `/var/run/twinnel/helper.sock`, group `admin` | named pipe `\\.\pipe\twinnel-helper`, DACL: SYSTEM, Administrators, the installing user's SID |
| Peer identity | `LOCAL_PEERCRED` (uid, groups) | `GetNamedPipeClientProcessId` → process token → user SID, elevation, Administrators membership |
| Elevation for install | `osascript … with administrator privileges` | `ShellExecuteExW` verb `runas` (UAC), argv joined with `syscall.EscapeArg`, never through `cmd.exe` |
| Secrets | System keychain | DPAPI (`CryptProtectData`, the helper's LocalSystem scope) in `<state>\secrets.dpapi` |
| mihomo service | LaunchDaemon `com.l3ony2k.twinnel.mihomo` | service `TwinnelMihomo`: `twinnel-svchost.exe` (a copy of the helper binary) hosting `mihomo.exe` |
| Tailscale | tailscaled (brew) or the app | the official client: service `Tailscale` (`tailscaled.exe`), wintun adapter `Tailscale`; detect and read only |
| App prefs | `~/Library/Application Support/twinnel/app.json` | `%AppData%\twinnel\app.json` (`os.UserConfigDir`, unchanged code) |
| Network facts | `route`, `netstat`, `scutil`, `networksetup` | `GetIpForwardTable2`, `GetAdaptersAddresses`, NRPT from the registry |

## 2. Helper as a Windows service

`cmd/helper` stays one binary. On Windows `main` asks
`svc.IsWindowsService()`; if so it runs under `svc.Run("TwinnelHelper", …)`:
the service handler reports `StartPending → Running`, accepts
`Stop | Shutdown`, cancels the same context SIGTERM cancels on Unix, reports
`StopPending` while `run` drains, then `Stopped`. Started from a console
(development) it behaves as on macOS.

The service definition, written with the Service Control Manager:

- name `TwinnelHelper`, display name `Twinnel Helper`, start type automatic,
  account LocalSystem;
- binPath `"%ProgramFiles%\Twinnel\twinnel-helper.exe" --state-dir "%ProgramData%\Twinnel" --socket \\.\pipe\twinnel-helper --allow-sid <installing user's SID>`;
- failure actions: restart after 5 s, three times, reset after a day (the
  equivalent of launchd's `KeepAlive` + `ThrottleInterval`);
- `helper.restart` ends `run`; the service exits with a non-zero
  service-specific code so the failure actions start it again.

Logging: the helper writes `<state>\logs\helper.log` as on macOS. No event
log source is registered (no installer-time registry writes beyond the
service itself); the SCM's own events (start/stop/crash) land in the System
log anyway.

### Plans and the SCM

`core/service` plans are lists of printable steps. The Windows manager
(`platform/windows/scm`) writes them as the `sc.exe` commands they are
equivalent to, so `--dry-run` reads like something an admin can run by hand:

```
sc.exe stop TwinnelHelper                                  # errors ignored
wait until `sc.exe query TwinnelHelper` says STOPPED or 1060 (up to 30s)
copy twinnel-helper.exe → %ProgramFiles%\Twinnel\twinnel-helper.exe
sc.exe create TwinnelHelper binPath= … start= auto obj= LocalSystem DisplayName= …   # errors ignored (exists)
sc.exe config TwinnelHelper binPath= … start= auto obj= LocalSystem DisplayName= …
sc.exe failure TwinnelHelper reset= 86400 actions= restart/5000/restart/5000/restart/5000
sc.exe description TwinnelHelper "…"
sc.exe start TwinnelHelper
```

They are not run through `sc.exe`: the Windows executor (`scm.Executor`)
recognises `sc.exe` steps and performs them in process with
`golang.org/x/sys/windows/svc/mgr` (create, config, failure actions,
description, start, stop, delete, query). That avoids `sc.exe`'s localised
output and its quoting rules, and gives real errors (`ERROR_SERVICE_EXISTS`,
`ERROR_SERVICE_DOES_NOT_EXIST` = 1060, `ERROR_SERVICE_MARKED_FOR_DELETE`).
`query` prints `STATE: <n> <NAME>` in English, so `StepWait` can match on it.
Anything that is not an `sc.exe` step goes to the normal `ExecExecutor`.

**Upgrade in place.** Windows locks a running `.exe`, so the copy must wait
for the old process to be gone, the same fix as on macOS (wait for launchd
to let go): stop, wait for `STOPPED` (or 1060 when it was never installed),
copy, `config` (binPath may have changed), start. `twinnel helper upgrade`
then verifies the new version over the pipe as on macOS.

**Uninstall.** stop, wait for `STOPPED`, `delete`, remove the binary. The
state dir stays (removed with `--purge`, after the same checks as on macOS:
absolute, not a drive root, named Twinnel). `ERROR_SERVICE_MARKED_FOR_DELETE`
(a services.msc window holds a handle) is reported with that explanation.

## 3. IPC: named pipe

`core/rpc.Transport` already hides the socket kind. Windows adds
`PipeTransport` (build tag `windows`) on
`github.com/tailscale/go-winio` (`ListenPipe`, `DialPipeContext`), which is
already in the module graph through `tailscale.com`; no new module.

- Address: `\\.\pipe\twinnel-helper` (`rpc.DefaultSocket` on Windows;
  `TWINNEL_SOCKET` overrides as before, e.g. `\\.\pipe\twinnel-dev-1234`).
- Protocol: unchanged, one JSON-RPC object per line, byte-mode pipe.
- Pipe DACL (SDDL), built by `rpc.PipeSDDL`:
  `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;<installing user SID>)` plus the
  helper's own SID when it is not SYSTEM (a development helper). Protected
  (`P`): no inherited entries. This is the macOS "admin group" rule:
  Administrators (an elevated admin's token) plus the person who installed it
  (whose normal, non-elevated token has Administrators as deny-only, so the
  SID entry is what lets the app in without a UAC prompt).
- Server-side check (`rpc.SIDAuthorizer`), in addition to the DACL: the peer
  is SYSTEM, the helper's own SID, an allowed SID (`--allow-sid`, repeatable),
  or a token that is elevated and in Administrators. The peer comes from
  `GetNamedPipeClientProcessId` → `OpenProcess(QUERY_LIMITED_INFORMATION)` →
  `OpenProcessToken` → `GetTokenUser`/`IsElevated`/`IsMember(Administrators)`.
- `rpc.Peer` gains `SID` and `Elevated`; `UID` stays for Unix and is `-1` on
  Windows. Logs print the SID.
- Clients pick the transport with `rpc.DefaultTransport()` (unix socket on
  macOS/Linux, pipe on Windows); `rpc.Dial(ctx, nil, addr)` uses it.

A stale pipe cannot exist (pipes vanish with their server), so Listen only
needs "another helper is already listening": go-winio's first instance uses
`FILE_FLAG_FIRST_PIPE_INSTANCE`, which fails if one is.

## 4. Elevation (UAC)

`twinnel helper install|upgrade|uninstall` as a non-elevated user:

1. print the plan (same as macOS);
2. capture the current user's SID (`--allow-sid` for the pipe; the elevated
   copy may run as another admin account after an over-the-shoulder prompt);
3. `ShellExecuteExW(lpVerb="runas", lpFile=<twinnel.exe>, lpParameters=<args>, nShow=SW_HIDE, fMask=SEE_MASK_NOCLOSEPROCESS|SEE_MASK_NO_CONSOLE)`,
   wait for the process, read its exit code;
4. the elevated copy runs with `--as-root --yes --output <temp file>` and
   writes its stdout/stderr there (its own console is hidden); the
   non-elevated copy prints the file afterwards and deletes it.

`lpParameters` is a command line, parsed by the elevated process's CRT
(`CommandLineToArgvW` rules). Each argument is quoted with
`syscall.EscapeArg`; there is no shell (`cmd.exe`/PowerShell) anywhere, and
arguments with control characters or NUL are refused as on macOS.
`ERROR_CANCELLED` (1223) from ShellExecuteEx means the person dismissed the
UAC prompt: "cancelled; nothing was changed".

"Root" on Windows (`priv.Elevated`) means: the process token is elevated or
is LocalSystem. Places that decided with `os.Geteuid() == 0` whether to run
plans for real (helper service runner, CLI install paths) use it. The Unix
ownership checks (state dir owned by root, config dir owner, binary trust in
`core/kernel/runas.go`) have no Windows counterpart in this port; the helper
instead resets the state dir's DACL to SYSTEM + Administrators at start, and
no RPC takes a path (unchanged).

## 5. mihomo as a service

`mihomo.exe` is a console program and does not speak the SCM protocol, so
it cannot be a service on its own (the SCM kills it after 30 s with error
1053). The helper binary has a second mode, the service host:

```
twinnel-svchost.exe svchost --name TwinnelMihomo --log <state>\logs\mihomo.log
    --throttle 10s -- <state>\kernels\current\mihomo.exe -d <confDir> -f <confFile>
```

- `twinnel-svchost.exe` is a copy of `twinnel-helper.exe` in
  `%ProgramFiles%\Twinnel\`, made by the mihomo service install plan.
  A separate copy, so uninstalling or upgrading the helper never breaks the
  mihomo service (the plan keeps mihomo running: never cut the network).
- The host runs `svc.Run`; it starts the child in a job object with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` (the child dies with the host, even
  on a crash), restarts it when it exits (`KeepAlive`), at most once per
  `--throttle`, and on Stop/Shutdown terminates it (mihomo removes its wintun
  adapter when the process ends) and reports Stopped. Child stdout/stderr go
  to `--log`.
- The restart/backoff logic (`platform/windows/svchost.Supervisor`) is
  platform-free and tested with a fake process starter.

`service.start/stop/restart/status` for `mihomo` and `tailscaled` go through
the same SCM manager. The Tailscale service (`Tailscale`) is only started,
stopped and queried, never installed or changed (`service.install` of
tailscaled is refused on Windows: Tailscale's own installer owns it).

## 6. Kernels

- Assets: `mihomo-windows-<arch>-<version>.zip` (`amd64` is the GOAMD64=v3
  build, `arm64`). `-compatible`, `-v1/-v2/-v3` and `-goXXX` variants are not
  picked (the existing pattern already excludes them). The zip holds one
  `mihomo-windows-<arch>.exe`; it is extracted to `<version>\mihomo.exe`.
- The digest check, probe (`mihomo.exe -v`) and pruning are unchanged.
- `current`/`previous` stay directory symlinks (the helper runs as
  LocalSystem, which may create them). Windows cannot rename a symlink over
  an existing one, so `setLink` removes the old link and renames the new one
  in (a microsecond gap; the service is restarted after a switch anyway).
  The running `mihomo.exe` lives under `<version>\`, so switching never
  touches a locked file.

## 7. Tailscale on Windows

The official client: the service `Tailscale` (`C:\Program Files\Tailscale\tailscaled.exe`),
the CLI `tailscale.exe` next to it, a wintun adapter named `Tailscale`.
It does not use a system VPN slot (no NetworkExtension equivalent), so the
macOS "two VPNs fight for the slot" problem does not exist; what can still
conflict is routes and DNS:

- **Routes.** Tailscale adds `100.64.0.0/10`-covered /32 routes (and the
  ULA /48) on its adapter with a low metric. mihomo's TUN (`auto-route`)
  adds `0.0.0.0/1` + `128.0.0.0/1` on its wintun adapter (default name
  `Meta`). The coexist layer's `route-exclude-address` keeps the tailnet
  ranges out of mihomo's TUN, exactly as on macOS. The `tailnet-route` check
  verifies with the real route table (`GetIpForwardTable2`, longest prefix,
  then route metric + interface metric, the same decision Windows makes).
- **DNS.** With "Use Tailscale DNS", Tailscale writes NRPT rules (registry,
  `…\Dnscache\Parameters\DnsPolicyConfig` and the GPO path) sending the
  MagicDNS suffix to `100.100.100.100`, or, with "override local DNS", sets
  its adapter's DNS to quad-100. mihomo's DNS hijack must not catch those:
  the coexist layer's `nameserver-policy` + `100.100.100.100#Tailscale`
  binding already does that. The `magicdns` check reads NRPT rules as
  scoped resolvers.
- Detection: `core/tailscale.DetectInstall("windows", …)` looks for the
  service binary and CLI under `%ProgramFiles%\Tailscale`. Version via
  `tailscale.exe version`. No download or install of Tailscale.

## 8. Health checks on Windows

`platform/windows.Env` implements `health.Env`. Like `platform/linux`, the
package has no build tag; the OS calls sit behind a `Sys` interface whose
real implementation is in `_windows.go` files, so the Env is tested with
fixtures on every OS.

| Fact | Source |
|---|---|
| routes | `GetIpForwardTable2(AF_UNSPEC)`; interface names from `GetAdaptersAddresses` (friendly name, as mihomo and Go's `net.Interfaces` use) |
| route lookup | computed from the table: longest prefix match, then lowest route metric + interface metric; skips adapters that are down |
| interfaces | `GetAdaptersAddresses`: friendly name, oper status, unicast addresses, if-type, description |
| resolvers | per-adapter DNS servers (scoped by interface), the default resolver (the up adapter with a default route and the lowest metric), and NRPT rules (domain → servers) |
| network services | physical adapters (not loopback, not tunnel/wintun/TAP), ordered by interface metric; DNS = the adapter's servers; source from the registry (`NameServer` set = manual, else DHCP) |
| VPNs | up adapters that are tunnels (if-type `TUNNEL`/`PPP`/`PROP_VIRTUAL`, or description matching WireGuard/Wintun/TAP-Windows/OpenVPN/…), excluding Tailscale's and mihomo's TUN |
| OS resolver lookup | Go's resolver (`net.DefaultResolver`, which on Windows uses `GetAddrInfoW`, i.e. NRPT and the DNS client) |
| mihomo / Tailscale | as on Linux: controller GETs, config file, `tailscale.exe status --json` |
| egress test | not implemented yet (skips) |

Check changes: `vpn-conflict` judges the default route on Windows like on
Linux (another tunnel holding it fails; mihomo's TUN not holding it while
enabled warns), with tunnel-ness taken from `RouteLookup.Flags` (`tunnel`)
instead of name prefixes. `system-dns` gets a Windows fix hint
(Settings → Network → DNS server assignment, or
`Set-DnsClientServerAddress`), in zh and en.

The helper's health scheduler on Windows runs on its interval and after
applies/service ops; a network-change trigger (`NotifyRouteChange2`,
`NotifyUnicastIpAddressChange`) is designed but not wired in this port.

## 9. Secrets

`platform/windows.DPAPISecrets` keeps the same key/value map as
`secrets.json`, sealed with `CryptProtectData` (no
`CRYPTPROTECT_LOCAL_MACHINE`, so only LocalSystem, the helper's account,
can unseal it) in `<state>\secrets.dpapi`; the state dir's DACL keeps
others from even reading the blob. A `secrets.json` left by a development
helper is migrated in and deleted, as with the keychain. The sealing is
behind two functions, so the store's logic is tested on every OS.
The app does not read secrets itself (it gets the controller secret over
`controller.credentials`, as on macOS).

## 10. The app

MyGo v0.10 supports Windows natively (Win32 + WebView2 through syscall/COM,
no cgo): tray icons in the notification area (`NewTray`, click and right
click), flyouts anchored to a tray icon (`FlyoutOptions.Tray`, flipped above
a bottom taskbar), native-UI windows, `Tray.Dark()` for the taskbar
appearance. The app already cross-compiles; on Windows it needs:

- the tray icon drawn white or black from `Tray.Dark()` (template images are
  a macOS concept; still monochrome, shapes carry state);
- `healthEnv` and `tailscaleIface` using `platform/windows`;
- the helper connection over the pipe (`rpc.DefaultTransport`).

App-side polish (tray icon variants, Windows-specific layout) is a follow-up;
this port wires the facts and the transport.

## 11. Uninstall / 停止使用

Same `core/uninstall` plan. On Windows: stop and delete `TwinnelHelper`,
remove `twinnel-helper.exe`; the pipe goes with the process. Twinnel's mihomo
service (`TwinnelMihomo`, with its own `twinnel-svchost.exe`) is kept unless
removed explicitly (`twinnel service uninstall mihomo`), and `--purge` is
refused while it exists (same `own-mihomo` refusal as macOS), because the
system DNS may point at mihomo and removing it would cut name resolution.
`--purge` deletes `%ProgramData%\Twinnel` (with `secrets.dpapi`). Tailscale is
never touched. There is no adopt/harden on Windows (no hand-made mihomo
service to take over), so `--restore-mihomo`/`--keep-mihomo` never apply.

## 12. CI

`.github/workflows/ci.yml` gets a `windows-latest` job running gofmt, vet and
`go test` for the packages that are meaningful there: `core/rpc`,
`core/service`, `core/kernel`, `core/store`, `core/health/...`,
`core/tailscale`, `core/uninstall`, `platform/common`, `platform/windows/...`,
`cmd/helper`, `cmd/cli`. The existing cross-build job keeps building
windows/amd64 and windows/arm64.

## 13. Not in this port

- Running on a real Windows machine (all of it; see HANDOFF's test plan).
- Event log source, network-change trigger, egress test on Windows.
- takeover/adopt/harden on Windows.
- The app's Windows-specific UI polish.
- Code signing of the binaries (SmartScreen will warn on unsigned exes).
