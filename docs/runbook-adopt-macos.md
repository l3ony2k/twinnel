# Runbook: adopt the existing mihomo + Tailscale setup (macOS)

This is the real cut-over. The helper takes over the hand-made mihomo in `~/mihomo-migration` (LaunchDaemon `com.mihomo.proxy`, controller `127.0.0.1:9090`). The only behavior change it makes is the Tailscale coexist layer: MagicDNS names resolve through `100.100.100.100#utunN` instead of looping through the system resolver. Plists are not replaced. The controller secret stays empty.

Time needed: about 10 minutes. You type the administrator password once (step 2).

Keep `~/mihomo-migration/emergency-restore.sh` at hand. It is unchanged and still works.

## 0. Build

```sh
cd <repo>
go build -o bin/twinnel ./cmd/cli && go build -o bin/twinnel-helper ./cmd/helper
export PATH="$PWD/bin:$PATH"
```

`twinnel` and `twinnel-helper` must be in the same directory. `twinnel helper install` looks for the helper binary next to itself.

## 1. Rehearse (optional, read-only, no helper)

```sh
twinnel takeover plan
```

This prints the plan and writes files to a temp dir. Check three lines under `verification`: `mihomo -t: ok`, `proxies … same; groups … same`, and `result: composed config behaves like the live one`. Every entry under `changes` must say `[deliberate]`.

## 2. Install the helper

```sh
twinnel helper install
```

- It prints the install plan, including the LaunchDaemon plist `com.l3ony2k.twinnel.helper`, then asks `Proceed? [y/N]`. Answer `y`.
- macOS shows a password dialog (osascript). Cancelling exits with "did not complete".
- A fresh helper has no subscription or layers, so it applies nothing. It only reads (controller GETs, health checks).

Verify:

```sh
twinnel helper status          # socket /var/run/twinnel/helper.sock (the default)
```

Expect `helper … uid 0`. The `mihomo` line still shows the helper's defaults; that is fine before the adopt.

## 3. Make the plan inside the helper

```sh
twinnel takeover plan --via-helper
```

You get the same summary as in step 1, plus `plan id: plan-YYYYMMDD-HHMMSS-xxxxxxxx`. The plan is stored under `/Library/Application Support/twinnel/takeover/<plan id>/` (root only, 0700). To list or view plans:

```sh
twinnel helper plans
twinnel helper plans <plan id>
```

## 4. Adopt

```sh
twinnel helper adopt <plan id>
```

It prints the plan, the deliberate changes and the six steps it will run, then asks `Proceed? [y/N]`. The run takes 10–30 s:

1. **verify**: `~/mihomo-migration/conf/config.yaml` must be byte-identical to the plan. If you edited it since, it refuses with "re-plan": run step 3 again.
2. **backup**: an exact copy goes to `/Library/Application Support/twinnel/takeover/<plan id>/live-backup.yaml`.
3. **import**: the subscription `adopted` (base `backup/raw-sub-original.yaml`, its URL from `backup/index.json` into the System keychain, manual refresh only), and the layers `base`, `user-1` and `user-2`. Settings point at `~/mihomo-migration/bin/mihomo`, the conf dir, `127.0.0.1:9090`, and the labels `com.mihomo.proxy` / `sh.brew.tailscale`, marked adopted. Coexist is on; the leak guard is off.
4. **health before**: `magicdns`, `tailnet-resolver-binding`, `tailscale-direct-rules`, `tailnet-route` and `system-dns`.
5. **apply**: `mihomo -t`, a history backup, the write (owner and mode kept), a hot reload `PUT /configs` (with `force=false`, because the ports are unchanged), and a wait for `/version`. The plan head shows a `reload` line with what the reload does to traffic; for this setup it must say "no network interruption". See "Network impact" below and `docs/reload-behavior.md`.
6. **health after**: if any of the five checks is worse than before (ok → skip/warn/fail, and so on), it is re-checked once after 3 s. If it is still worse, the backup is written back byte for byte, mihomo reloads it, and the import is undone. The output then says `result: ROLLED BACK: …` and the exit status is 1.

Expected result: `result: adopted`. `tailnet-resolver-binding` should go from skip to ok and `magicdns` from warn to ok. On a copy of this setup, skip → ok was seen; warn → ok can only show on the real mihomo, which is the one that answers on :53.

## 5. Check by hand

```sh
twinnel helper health --run
dig +short @127.0.0.1 <peer>.<tailnet>.ts.net   # expect a 100.x address, not 198.18.x (fake-ip)
ping -c1 <peer>                                  # short name
twinnel history list                                # "applied" on top, "backup" below it
curl -s 127.0.0.1:9090/version                   # still no secret needed
```

Also open zashboard, check the proxy groups and their selected nodes, and load a few sites.

## 6. Roll back by hand (if needed later)

**A. Through the helper** (it keeps managing mihomo):

```sh
twinnel history list
twinnel history rollback <id of the "backup" entry>   # the pre-adopt file, through mihomo -t and a reload
```

History entries never hold the `secret:` line: the helper writes the stored secret back in, and with an empty secret it leaves the line out. So the result is not byte-identical to `live-backup.yaml` (it lacks `secret: ""`), but mihomo reads both the same way. For the exact bytes, use B.

**B. Completely back to the hand-made setup.** Stop the helper first, so it doesn't re-apply on a tailnet change:

```sh
twinnel helper uninstall        # lists what goes and what stays, asks y/N + password; the state dir (plans, backup) is kept
sudo cp "/Library/Application Support/twinnel/takeover/<plan id>/live-backup.yaml" ~/mihomo-migration/conf/config.yaml
curl -s -X PUT 'http://127.0.0.1:9090/configs?force=true' -H 'Content-Type: application/json' \
     -d "{\"path\":\"$HOME/mihomo-migration/conf/config.yaml\"}"
# or: sudo launchctl kickstart -k system/com.mihomo.proxy
shasum -a 256 ~/mihomo-migration/conf/config.yaml  # starts with the sha256 on the `live config` line of `twinnel helper plans <plan id>`; full value: live_sha256 in plan.json
```

`cp` onto the existing file keeps its owner (you). If the network is down, run `bash ~/mihomo-migration/emergency-restore.sh` first.

The keychain items stay until you delete them: `sudo security delete-generic-password -s twinnel.subscription-url/adopted /Library/Keychains/System.keychain` (and `twinnel.controller-secret` after step 7).

## 7. Later: secure the controller (separate step)

Adopt keeps the empty secret on purpose. When you are ready:

```sh
twinnel helper secure-controller
```

It asks y/N, generates a secret, stores it in the System keychain (`twinnel.controller-secret`), writes it into the config and reloads. A hot reload does **not** change the controller's secret: mihomo reads it only when it starts. The secret takes effect at the next start of `com.mihomo.proxy` (reboot, or `sudo launchctl kickstart -k system/com.mihomo.proxy`, which restarts the TUN and cuts the network for a few seconds, so do it when nothing important runs). Until then the controller still answers without a secret. The helper copes with both states. Afterwards zashboard and any scripts need the secret:

```sh
sudo security find-generic-password -s twinnel.controller-secret -w /Library/Keychains/System.keychain
```

To undo it, roll back to the history entry before it (`twinnel history list`, then `twinnel history rollback <id>`). Then delete the keychain item; otherwise the next apply puts the secret back.

## 8. Move mihomo to a root-owned kernel and config (one restart, a few seconds offline)

After the adopt, `com.mihomo.proxy` still runs `~/mihomo-migration/bin/mihomo` **as root**, and that file and the `-d` dir `~/mihomo-migration/conf` belong to you. Any program running as you could replace the binary and get root at the next restart (security review, finding 5). This step moves mihomo to:

- the managed kernel `/Library/Application Support/twinnel/kernels/mihomo/current/mihomo` (root-owned, same version as now unless you pass `--kernel`);
- a root-owned config dir `/Library/Application Support/twinnel/mihomo` (0700): the composed config, the geo databases, `cache.db` (selected nodes, fake-ip pool) and the dashboard are copied there. Absolute paths into the old dir (`external-ui: ~/mihomo-migration/conf/ui`) become relative (`ui`);
- Twinnel's LaunchDaemon `com.l3ony2k.twinnel.mihomo`.

It needs **one restart** of mihomo, which rebuilds the TUN: the network is down for a few seconds (estimate 2–8 s; nothing was measured on this Mac). Do it when no agent or download is running. Put every other restart-only change into the same restart:

- the controller secret: with `--new-secret` the new mihomo starts with a new one. Without it, a secret that step 7 stored but mihomo has not loaded yet becomes active now;
- a kernel update: `--kernel vX.Y.Z`.

### 8a. Plan (changes nothing)

```sh
twinnel helper harden plan [--new-secret] [--kernel vX.Y.Z]
```

It shows both sides (label, plist, binary, config, owner), the kernel download if the version is not installed yet, what is copied (and what is not, e.g. symlinks), the config changes, the secret, the expected downtime and every step. Steps marked `!` run while mihomo is down. Check:

- `from` is `com.mihomo.proxy`, `to` is `com.l3ony2k.twinnel.mihomo`;
- `copy` lists the geo files, `cache.db` and `ui/`;
- each `change` line is expected. "the composed config differs from the running file in more than paths" means the subscription, a layer or the tailnet changed since the last apply; `twinnel compose --preview` shows it, and you may want to apply that first, on its own.

### 8b. Apply

```sh
twinnel helper harden apply            # shows the newest ready plan again, then y/N
```

Before the cut (network unaffected): installs or selects the kernel, copies into a staging dir, writes the config, runs `mihomo -t` with the new kernel, renames the staging dir into place, saves a copy of the old plist in `/Library/Application Support/twinnel/harden/<plan id>/old-service.plist`, writes `/Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist`, and runs the health checks.

The cut: `launchctl bootout system/com.mihomo.proxy`, wait until its controller stops answering (at most 15 s), `launchctl bootstrap system /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist` and `launchctl kickstart system/com.l3ony2k.twinnel.mihomo`, wait for `GET /version` (at most 45 s), health checks again.

- **Success:** `launchctl disable system/com.mihomo.proxy` (the plist stays, it just does not load at boot any more) and settings switch to the new kernel, dir and label. The result shows the measured time offline.
- **Failure** (the new one does not answer, or health is worse): the new label is booted out and its plist removed, `com.mihomo.proxy` is bootstrapped again, the previous secret is restored, and the new dir is moved to `…/harden/<plan id>/failed-config-dir` for inspection. Settings are not changed. Offline time is then about twice as long.
- If the helper itself dies during the cut, it boots `com.mihomo.proxy` back in when it starts again (if neither service runs).

Afterwards check:

```sh
sudo launchctl print system/com.l3ony2k.twinnel.mihomo | grep -E 'state|program'
sudo launchctl print-disabled system | grep -E 'mihomo'   # com.mihomo.proxy => disabled
twinnel status
twinnel helper health --run
```

Zashboard keeps working at the same address. With `--new-secret` it needs the new secret (see step 7 for how to read it).

### 8c. Roll back by hand

The old binary, `~/mihomo-migration/conf` and `/Library/LaunchDaemons/com.mihomo.proxy.plist` are untouched. To go back (a few seconds offline again):

```sh
sudo launchctl bootout system/com.l3ony2k.twinnel.mihomo
sudo launchctl enable system/com.mihomo.proxy
sudo launchctl bootstrap system /Library/LaunchDaemons/com.mihomo.proxy.plist
curl -s http://127.0.0.1:9090/version          # add -H "Authorization: Bearer <secret>" if it has one
sudo mv /Library/LaunchDaemons/com.l3ony2k.twinnel.mihomo.plist "/Library/Application Support/twinnel/harden/"
```

If the network is down at any point, `bash ~/mihomo-migration/emergency-restore.sh` still works (it uses the old setup).

**Or let uninstall do it.** `twinnel helper uninstall` refuses while mihomo runs from Twinnel's LaunchDaemon. `twinnel helper uninstall --restore-mihomo` runs the steps above as root before it removes the helper: `bootout system/com.l3ony2k.twinnel.mihomo`, wait until the controller stops answering (at most 15 s), `enable` and `bootstrap` `com.mihomo.proxy`, wait until the controller answers again (at most 45 s; any HTTP answer counts, also a 401). Then Twinnel's plist is removed (a copy goes to `…/twinnel/harden/`, unless `--purge`). If the original does not answer, it starts Twinnel's mihomo again and stops: nothing else is removed. `--keep-mihomo` instead leaves Twinnel's mihomo running from the state dir, unmanaged (not with `--purge`). The app's 卸载… and 完全移除 Twinnel… always use `--restore-mihomo`.

The helper's settings still point at the new dir after a manual rollback. Either uninstall the helper (step 6 B), or edit `/Library/Application Support/twinnel/settings.json` as root: put back the `mihomo` values recorded under `hardened` (`old_binary`, `old_config_dir`, `old_config_file`, `old_config_dir_uid`, `old_label` as `service_label`, `service_adopted: true`), remove `hardened`, and restart the helper (`sudo launchctl kickstart -k system/com.l3ony2k.twinnel.helper`). Note that the old config file is the one from before step 8; changes applied after it are only in the new dir.

## Network impact (when to do what)

Measured and read from the mihomo v1.19.34 source; details in `docs/reload-behavior.md`.

| Step | Touches the network path? |
|---|---|
| 1 rehearse, 3 plan | No. Reads files, `tailscale status`, `route get`, controller `GET /version`, `mihomo -t` on temp files. |
| 2 install the helper | No. It starts a LaunchDaemon that only reads (controller GETs, health checks: DNS queries to 127.0.0.1:53, `scutil`, `netstat`, `route get`). With no subscription it applies nothing. |
| 4 adopt | One hot reload (two if it rolls back). TUN, DNS server and ports stay as they are; open connections, fake-ip mappings and the selected nodes are kept. Connections **opened** during a pause of about 1–15 ms are closed at once, and UDP packets in that pause are dropped. A program that opens a connection at that instant gets one connection error; most retry. |
| 7 secure the controller | One more hot reload (same as above); the secret needs a restart of mihomo, which does cut the network. Step 8 is that restart. |
| 8a harden plan | No. Reads, `launchctl print`, controller `GET /version`, a GitHub release lookup, `mihomo -t` on temp files. |
| 8b harden apply | **Yes: one restart of mihomo**, a few seconds without network (TUN rebuilt, open connections cut). The kernel download and the copy happen before it, while the network is up. |

Safest moment for step 4: when no agent or download is starting new connections (e.g. between tasks). Steps 1–3 and 8a can be done any time. Step 8b only when nothing is running.

After the adopt, the helper reloads mihomo again only when the composed config changes: a Tailscale peer joins or leaves the tailnet, its MagicDNS name or IP changes, or tailscaled restarts on a new utun. Each such reload is the same short pause.

## Updating, restarting and removing the helper

- `twinnel helper upgrade --check` compares the helper next to `twinnel` (in the app: `Twinnel.app/Contents/Resources/bin/twinnel-helper`) with the installed one and the running one. Reads only.
- `twinnel helper upgrade` installs the bundled helper when it differs: the install plan (copy the binary, reload `com.l3ony2k.twinnel.helper`), one password prompt, then it waits until the helper answers with the new version. It never touches `com.mihomo.proxy`, `com.l3ony2k.twinnel.mihomo` or tailscaled. An older bundled helper is refused unless `--force`.
- `twinnel helper restart` asks the running helper to restart itself (`helper.restart`: it exits and launchd starts it again; no password). If it does not answer, it runs `launchctl kickstart -k system/com.l3ony2k.twinnel.helper` as root.
- `twinnel helper uninstall [--purge]` lists what goes and what stays first. `--purge` also deletes `/Library/Application Support/twinnel` (settings, subscriptions, layers, history, logs, takeover and harden backups, kernels) and the keychain items `twinnel.controller-secret` and `twinnel.subscription-url/*`. It refuses when a mihomo that Twinnel installed itself (not adopted, not hardened) still runs from the state dir: purging would cut the network. Remove that service first (`twinnel service uninstall mihomo`).
- `scripts/install-local.sh` does the daily-use update in one go: build, quit the app, back up `/Applications/Twinnel.app`, install, `twinnel helper upgrade` only if the helper changed.

## Notes

- The helper never replaces or removes `com.mihomo.proxy`: `twinnel service install|uninstall mihomo` is refused for an adopted service. start/stop/restart work. Step 8 only boots it out and disables it; the plist stays.
- The adopted subscription has no refresh interval. To have the helper fetch it, use `twinnel sub put adopted … --interval 720`. Be aware that a fetched subscription can differ from `raw-sub-original.yaml`.
- The leak guard is off after the adopt. Turn it on with `twinnel settings --leak-guard Google,Meta` (or whichever groups you want).
- `twinnel helper discard <plan id>` refuses the adopted plan, because it holds the exact backup (`--force` deletes it anyway).
