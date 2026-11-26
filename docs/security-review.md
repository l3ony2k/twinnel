# Security review of duet-helper (2026-11-26)

Scope: the root helper (`cmd/helper`, `core/rpc`, `core/engine`, `core/store`, `core/kernel`, `core/service`, `platform/darwin/{launchd,keychain,takeoverprobe}`) and the CLI install path (`cmd/cli/helper*.go`), reviewed before the owner installs it as a LaunchDaemon on a daily-driver Mac.

## Threat model

- **Local process running as the console user.** On a single-user Mac that user is in `admin`, so such a process can reach the helper socket (`root:admin 0660`). It does not know the user's password. The bar: talking to the helper must not give it more than "change the proxy configuration". Above all, it must not get root code execution, root file writes, or root file reads.
- **Malicious subscription.** The provider writes the base of every composed config. The config sets the geo/UI download URLs and the controller settings, and mihomo runs it as root.
- **Malicious or compromised release, and the network path** for kernel, geo and UI downloads.
- Out of scope: an attacker who already has root; bugs in mihomo or Tailscale.

## Findings

Severity: **High** means a local user gets root, or arbitrary root file write or read. **Medium** is a partial bypass or one that depends on another flaw. **Low** is hardening or a narrow information leak.

| # | Severity | Finding | Status |
|---|---|---|---|
| 1 | High | Root runs user-replaceable binaries: the tailscale CLI, the adopted mihomo binary and `mihomo -t` | Fixed |
| 2 | High | Geo download staged with `O_TRUNC` through a planted symlink: arbitrary root file write | Fixed |
| 3 | High | Config dir writes (config file, geo swap, UI swap) follow symlinked directories out of the dir | Fixed |
| 4 | High | Adopt accepts any config dir found in a user-written LaunchAgent (for example `/etc/paths.d`) | Fixed |
| 5 | High (pre-existing, not duet's) | The live LaunchDaemon runs `~/mihomo-migration/bin/mihomo` (owned by the user) as root, with a user-writable `-d` dir | Fix available: `duet helper harden apply` (owner runs it; one restart) |
| 6 | Medium | `config.get live` and the controller secret lookup read the config through a symlink: arbitrary root file read | Fixed |
| 7 | Medium | `service.install` would create a root LaunchDaemon for a user-replaceable binary | Fixed |
| 8 | Medium | A subscription can open the controller to the LAN without a secret, or put an unauthenticated controller socket anywhere | Fixed (refused at apply) |
| 9 | Medium | Adopt runs whatever binary the plist names for `mihomo -t` | Fixed (must answer `-v` like mihomo; runs without root if user-owned) |
| 10 | Low | geo/UI URLs from the config allowed `http://127.0.0.1` / `localhost` (request forgery against local services) | Fixed (https only, no loopback, private or link-local literals) |
| 11 | Low | Downloads followed https→http redirects | Fixed |
| 12 | Low | State dir: the mode of existing subdirs was never tightened; the dir was not checked for ownership | Fixed |
| 13 | Low | Health checks (`platform/darwin`) and takeover discovery (`core/takeover`) read files in the user's config dir as root, following symlinks | TODO |
| 14 | Low | Plain-http subscription URLs are accepted | Fixed (warning from `subscription.put`) |
| 15 | Info | Any `admin` member has full use of the helper (by design) | Documented |
| 16 | Info | Controller on loopback without a secret is reachable from web pages (DNS rebinding / CORS) | Existing `controller.secure` and the app's warning |

### 1. Root exec of user-replaceable binaries (High, fixed)

**Exploit sketch.** Homebrew's `/opt/homebrew/Cellar/tailscale/<v>/bin/tailscale` is owned by the user, and `/opt/homebrew/Cellar` is group-`admin` writable. The helper ran `tailscale status --json` as root every 60 s (tailnet poll), and again on every health run and takeover plan. A user process replaces the file and gets root within a minute, with no password. The same holds for the adopted `~/mihomo-migration/bin/mihomo`, which the helper ran as `mihomo -t` and `-v`.

**Fix.** `core/kernel/runas.go`: `CheckExec` resolves symlinks and walks the file and every parent directory. A binary is *trusted* only if root owns all of them and no group (other than wheel) or others can write any of them (sticky dirs are allowed). Otherwise `Command` runs it as its single non-root owner, with no supplementary groups and a minimal environment (`HOME` set to that user's). If several users own parts of the path, or a world-writable or group-writable dir with no single owner is on it, it runs as `nobody`. That user could already run the binary, so swapping it gains nothing. The fix is wired into the tailscale CLI (engine poll, `kernel.TailscaleProbe`, takeover probe, health env via `darwin.Options.Run`), `mihomo -v` and `mihomo -t` (`config.WithRunAs`: the work dir goes in `/tmp` and is handed to that user). It is a no-op when the helper is not root.

**Tests.** `core/kernel/runas_test.go`, `core/config/validate_test.go:TestValidateRunAs`.

### 2. Geo staging file opened through a symlink (High, fixed)

**Exploit sketch.** The adopted config dir `~/mihomo-migration/conf` is user-writable. Plant `GeoIP.dat.duet-new -> /etc/sudoers.d/x`, set `geox-url.geoip` (a layer, or the subscription) to an https URL serving `"\n<user> ALL=(ALL) NOPASSWD: ALL\n"`, and call `geo.update`. The download was opened with `O_CREATE|O_TRUNC` and followed the link. The `.dat` format check only wants a leading `0x0a`, so a valid sudoers file is written as root.

**Fix.** All accessory I/O goes through an `os.Root` on the config dir. Staging files are removed and then created with `O_EXCL`, and swaps and rollbacks use `Root.Rename` and `Root.RemoveAll` with relative names.

**Tests.** `TestGeoUpdateIgnoresPlantedSymlink`, `TestGeoUpdateReplacesSymlinkedTarget`.

### 3. Directory redirection of config-dir writes (High, fixed)

**Exploit sketch.** Replace `conf/ui` with a symlink to `/Library` and set `external-ui: ui`, `external-ui-name: LaunchDaemons` and an `external-ui-url` serving a zip with `index.html` and `evil.plist`. `ui.update` then swapped `/Library/LaunchDaemons` for the zip contents. A similar trick works for `conf -> /etc/...` with the config file write.

**Fix.** `core/engine/confdir.go`. Every write (config file, restore, adopt rollback, geo, UI) opens the dir as an `os.Root`, so nothing can escape it through `..` or symlinks. As root, the dir must also be owned by the expected uid: root by default, or settings `mihomo.config_dir_uid`, which adopt records. Every path component must be owned by root or that uid and must not be group- or world-writable (sticky dirs allowed), so no third user can redirect it. The config is written to an `O_EXCL` temp file, `fchown`ed to the dir's owner when that is a user (previously `Lchown` after the rename), then renamed.

**Tests.** `TestUIUpdateRefusesSymlinkedDir`, `TestOpenConfDirOwner`, `TestApplyAsRootChecksOwner`, `TestApplyDoesNotWriteThroughSymlink`.

### 4. Adopt from a planted LaunchAgent (High, fixed)

**Exploit sketch.** Discovery also reads `~/Library/LaunchAgents`, which the user controls. A fake agent with `-d /etc/paths.d`, plus a fake controller on the configured port, could make adopt record a root-owned system dir as the config dir. Later geo or config writes land there (`/etc/paths.d/*` is read by `path_helper` for every login shell).

**Fix.** `core/engine/adoptguard.go:adoptDirOwner`, enforced in `takeover.adopt` and reported in `takeover.plan` warnings. It requires that:

- the config dir and the `-f` dir have one owner;
- if that owner is root, the setup comes from a root-owned plist in a root-owned LaunchDaemons dir, and the whole path is root-owned;
- if the owner is a user, it is the user whose home is probed, and no third user controls the path.

The owner's real setup passes: a LaunchDaemon, with `conf` owned by the user.

**Test.** `TestAdoptDirOwner`.

### 5. The existing setup already hands root to the user (High, pre-existing; fix available)

`/Library/LaunchDaemons/com.mihomo.proxy.plist` runs `/Users/<user>/mihomo-migration/bin/mihomo` as root, and that file is owned by the user. Any process running as the user can replace it and wait for the next restart. mihomo (root) also writes provider caches, `cache.db` and so on into the user-writable `-d` dir.

duet does not make this worse: it never runs that binary as root itself, and adopt shows a warning (`binaryWarning`). Closing the hole needs a restart of mihomo, so it is a separate step the owner runs when idle:

**`service.harden.plan` / `service.harden.apply`** (`duet helper harden plan|apply`, runbook step 8). One restart moves mihomo to:

1. the managed kernel (root-owned, under the state dir; same version as running unless asked; sha256-checked download from the GitHub release);
2. a root-owned config dir `<state>/mihomo` (0700). The composed config, geo databases, `cache.db`, the dashboard and provider files are copied through an `os.Root` on the old dir: symlinks and other non-regular entries are not copied, and the copy is capped (20 000 entries, 1 GiB). Absolute paths into the old dir are rewritten to relative ones when composing;
3. duet's LaunchDaemon `com.l3ony2k.duet.mihomo` (`RequireTrusted` on the kernel before the cut).

The cut is bootout old → bootstrap new → `/version` + health compare; on failure the old label is bootstrapped back. On success the old label is disabled in launchd (`launchctl disable`), not deleted, and the old binary and dir stay as a manual fallback. The apply also activates a pending controller secret, or a new one (`new_secret`), in the same restart.

After step 8, the old binary and dir are no longer used by anything that runs as root. The owner can delete them, or keep them for rollback: they only matter if `com.mihomo.proxy` is enabled again.

### 6. Root file read through the config path (Medium, fixed)

**Exploit sketch.** Replace `conf/config.yaml` with a symlink to `/etc/master.passwd` (or another user's file), then call `config.get {source: live}`. The helper read it as root and returned it. The text only had the `secret:` line stripped, as non-YAML. `controller.credentials`, status and apply read the same file.

**Fix.** These reads go through `readConfined` (an `os.Root` on the file's dir).

**Test.** `TestConfigGetLiveRefusesSymlinkOut`.

### 7. Root service for a user-replaceable binary (Medium, fixed)

`service.install` (mihomo, not adopted) wrote a LaunchDaemon around `settings.mihomo.binary`. As root, it now requires `kernel.RequireTrusted`.

**Test.** `TestMihomoSpecRefusesUserBinary`.

### 8. Controller exposure from the config (Medium, fixed)

A subscription is the base of the composed config and can set `external-controller: 0.0.0.0:9090` with no secret, or `external-controller-unix: /some/path`, which root mihomo creates without authentication. `exposureCheck` (in `check`, so preview and apply both see it) now refuses these:

- a non-loopback `external-controller` or `-tls` when the final config has no secret;
- a `-unix` or `-pipe` path outside the config dir.

A non-loopback controller with a secret only gives a warning. Layers can still override every key, as before.

**Tests.** `TestExposureCheck`, `TestApplyRefusesOpenController`.

### 9. Adopted binary identity (Medium, fixed)

The binary path comes from a plist, possibly a user-written agent. Adopt now requires `bin -v` to print `Mihomo Meta <version>`. The probe itself runs without root when the binary is user-owned (finding 1).

### 10–12. Hardening (Low, fixed)

- `kernel.checkURL` accepts only `https`, and refuses `localhost` and loopback, private, link-local or unspecified IP literals. Hostnames that resolve to private addresses (DNS rebinding) are not checked; the impact is a GET whose body is written only if it passes the size, digest and format checks.
- The engine's HTTP client refuses https→http redirects and stops after 10 redirects.
- `store.Open`: as root, refuses a state dir that is a symlink, not root-owned, or group/world-writable. It re-applies `0700` to `subscriptions/`, `layers/`, `history/` and `kernels/` on every start.

### 13. Reads as root in other packages (Low, TODO)

`platform/darwin` (health env) reads `ConfigPath`, and `core/takeover` reads the live config and the base candidates next to it, both with `os.ReadFile`, so they follow a planted symlink. What leaks is limited: health only reports parsed controller and DNS settings; takeover keeps a candidate only if it parses as a config with `proxies`, and plan texts are redacted. **TODO:** use `os.OpenRoot` / `O_NOFOLLOW` there too. Both packages are outside this review's edit scope.

### 14. Plain-http subscriptions (Low, fixed: warning)

`subscription.put` accepts `http://` URLs. A network attacker on that path can read the URL (often a token) and serve a config, which is still bounded by finding 8 and `mihomo -t`. `subscription.put` now returns `warnings` for a plain-http URL (`rpc.Subscription.warnings`, set only by put); `duet sub put` prints it. The app should show it too.

**Test.** `TestSubscriptionPutWarnsOnPlainHTTP`.

### 15. Who may use the helper (Info)

`root`, the helper's own uid, and members of `admin`. The check uses `LOCAL_PEERCRED` groups, with a fallback to the group database for users in more than 16 groups. There is no TOCTOU, because credentials are taken at connect time from the kernel. The socket is `root:admin 0660`, so non-admin users cannot connect at all. Every authorized peer can do everything: compose and apply, read `controller.credentials`, run kernel and accessory updates, and start or stop services. After the fixes above, that power is limited to the proxy configuration and the allowlisted operations: no paths, commands, URLs (other than the subscription) or binaries come from clients. A process running as an admin user can still redirect that user's traffic through a proxy of its choosing, which is inherent to a network manager without a password prompt per change.

## Reviewed and found sound

- **RPC server.** Fixed method allowlist and `DisallowUnknownFields`. One JSON line is capped at 16 MiB, with 8 requests in flight per connection. `events.wait` is capped at 2 min and the bus at 256 events. Handler panics are recovered.
- **IDs.** Layer, subscription and plan IDs must match `^[a-z0-9][a-z0-9_-]{0,63}$` before they become file names. History IDs are checked in `store`. Launchd labels are checked (`checkLabel`).
- **`takeover.plan` with a client-provided base.** Accepted only if it is one of the candidates the helper found itself.
- **Kernel install.** Takes a channel and an optional version (`ValidVersion`). It refuses an asset without a published sha256 digest and checks size and digest. The binary goes into the root-owned state dir, and the `current` link is switched atomically. Trust is the GitHub release of MetaCubeX/mihomo, since the digest comes from the same place as the asset.
- **UI zip.** Zip-slip and absolute or backslash paths are refused. Symlinks and other non-regular entries are refused. Limits: 5000 entries, 64 MiB zip, 256 MiB extracted; per-file reads are capped at the declared size and `archive/zip` rejects overlong entries.
- **Keychain.** Secrets are piped to `security -i` as hex, so they are never in argv, the environment or a file. Keys are checked for quote and control characters. Items live in the System keychain, readable only by root (or with admin authentication).
- **Logs.** `helper.log` (0644) records methods, uids, pids, durations and errors. Subscription URLs are redacted in fetch errors, and `ControllerCredentials` hides the secret in `String`, `GoString` and `LogValue`. No params are logged.
- **Install (`duet helper install`).** The command is quoted in two layers: shell single quotes inside an AppleScript string literal. Control characters are refused, and osascript is exec'd with an argv. The binary is copied as root to `/Library/PrivilegedHelperTools/<label>` with mode 0755. The plist is 0644 in `/Library/LaunchDaemons`. The state dir and the socket dir are root-owned 0755. Mutations happen only after the plan has been printed and confirmed.
- **Supply chain.** Direct dependencies are `golang.org/x/sys`, `gopkg.in/yaml.v3` and `tailscale.com` (LocalAPI types; pulls `x/crypto`, `x/net` and others), plus `egoist/mygo` (app only). All are pinned in `go.sum`. `govulncheck ./cmd/helper ./cmd/cli` on 2026-11-26 reported no vulnerabilities.

## Still to do

- Finding 13: confined reads in `platform/darwin` and `core/takeover`.
- Finding 5: the owner runs `duet helper harden apply` when idle (one restart).
- Finding 14 in the app: show `warnings` from `subscription.put`.
- Consider an `O_NOFOLLOW` open for `helper.log`. The logs dir is root-owned inside the root-owned state dir, so this is not exploitable today.
