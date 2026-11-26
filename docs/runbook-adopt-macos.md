# Runbook: adopt the existing mihomo + Tailscale setup (macOS)

This is the real cut-over. The helper takes over the hand-made mihomo in `~/mihomo-migration` (LaunchDaemon `com.mihomo.proxy`, controller `127.0.0.1:9090`). The only behavior change it makes is the Tailscale coexist layer: MagicDNS names resolve through `100.100.100.100#utunN` instead of looping through the system resolver. Plists are not replaced. The controller secret stays empty.

Time needed: about 10 minutes. You type the administrator password once (step 2).

Keep `~/mihomo-migration/emergency-restore.sh` at hand. It is unchanged and still works.

## 0. Build

```sh
cd <repo>
go build -o bin/duet ./cmd/cli && go build -o bin/duet-helper ./cmd/helper
export PATH="$PWD/bin:$PATH"
```

`duet` and `duet-helper` must be in the same directory. `duet helper install` looks for the helper binary next to itself.

## 1. Rehearse (optional, read-only, no helper)

```sh
duet takeover plan
```

This prints the plan and writes files to a temp dir. Check three lines under `verification`: `mihomo -t: ok`, `proxies … same; groups … same`, and `result: composed config behaves like the live one`. Every entry under `changes` must say `[deliberate]`.

## 2. Install the helper

```sh
duet helper install
```

- It prints the install plan, including the LaunchDaemon plist `com.l3ony2k.duet.helper`, then asks `Proceed? [y/N]`. Answer `y`.
- macOS shows a password dialog (osascript). Cancelling exits with "did not complete".
- A fresh helper has no subscription or layers, so it applies nothing. It only reads (controller GETs, health checks).

Verify:

```sh
duet helper status          # socket /var/run/duet/helper.sock (the default)
```

Expect `helper … uid 0`. The `mihomo` line still shows the helper's defaults; that is fine before the adopt.

## 3. Make the plan inside the helper

```sh
duet takeover plan --via-helper
```

You get the same summary as in step 1, plus `plan id: plan-YYYYMMDD-HHMMSS-xxxxxxxx`. The plan is stored under `/Library/Application Support/duet/takeover/<plan id>/` (root only, 0700). To list or view plans:

```sh
duet helper plans
duet helper plans <plan id>
```

## 4. Adopt

```sh
duet helper adopt <plan id>
```

It prints the plan, the deliberate changes and the six steps it will run, then asks `Proceed? [y/N]`. The run takes 10–30 s:

1. **verify**: `~/mihomo-migration/conf/config.yaml` must be byte-identical to the plan. If you edited it since, it refuses with "re-plan": run step 3 again.
2. **backup**: an exact copy goes to `/Library/Application Support/duet/takeover/<plan id>/live-backup.yaml`.
3. **import**: the subscription `adopted` (base `backup/raw-sub-original.yaml`, its URL from `backup/index.json` into the System keychain, manual refresh only), and the layers `base`, `user-1` and `user-2`. Settings point at `~/mihomo-migration/bin/mihomo`, the conf dir, `127.0.0.1:9090`, and the labels `com.mihomo.proxy` / `sh.brew.tailscale`, marked adopted. Coexist is on; the leak guard is off.
4. **health before**: `magicdns`, `tailnet-resolver-binding`, `tailscale-direct-rules`, `tailnet-route` and `system-dns`.
5. **apply**: `mihomo -t`, a history backup, the write (owner and mode kept), `PUT /configs?force=true`, and a wait for `/version`.
6. **health after**: if any of the five checks is worse than before (ok → skip/warn/fail, and so on), it is re-checked once after 3 s. If it is still worse, the backup is written back byte for byte, mihomo reloads it, and the import is undone. The output then says `result: ROLLED BACK: …` and the exit status is 1.

Expected result: `result: adopted`. `tailnet-resolver-binding` should go from skip to ok and `magicdns` from warn to ok. On a copy of this setup, skip → ok was seen; warn → ok can only show on the real mihomo, which is the one that answers on :53.

## 5. Check by hand

```sh
duet helper health --run
dig +short @127.0.0.1 <peer>.<tailnet>.ts.net   # expect a 100.x address, not 198.18.x (fake-ip)
ping -c1 <peer>                                  # short name
duet history list                                # "applied" on top, "backup" below it
curl -s 127.0.0.1:9090/version                   # still no secret needed
```

Also open zashboard, check the proxy groups and their selected nodes, and load a few sites.

## 6. Roll back by hand (if needed later)

**A. Through the helper** (it keeps managing mihomo):

```sh
duet history list
duet history rollback <id of the "backup" entry>   # the pre-adopt file, through mihomo -t and a reload
```

**B. Completely back to the hand-made setup.** Stop the helper first, so it doesn't re-apply on a tailnet change:

```sh
duet helper uninstall        # asks y/N + password; the state dir (plans, backup) is kept
sudo cp "/Library/Application Support/duet/takeover/<plan id>/live-backup.yaml" ~/mihomo-migration/conf/config.yaml
curl -s -X PUT 'http://127.0.0.1:9090/configs?force=true' -H 'Content-Type: application/json' \
     -d "{\"path\":\"$HOME/mihomo-migration/conf/config.yaml\"}"
# or: sudo launchctl kickstart -k system/com.mihomo.proxy
shasum -a 256 ~/mihomo-migration/conf/config.yaml  # equals live_sha256 in `duet helper plans <plan id>` (or plan.json)
```

`cp` onto the existing file keeps its owner (you). If the network is down, run `bash ~/mihomo-migration/emergency-restore.sh` first.

The keychain items stay until you delete them: `sudo security delete-generic-password -s duet.subscription-url/adopted /Library/Keychains/System.keychain` (and `duet.controller-secret` after step 7).

## 7. Later: secure the controller (separate step)

Adopt keeps the empty secret on purpose. When you are ready:

```sh
duet helper secure-controller
```

It asks y/N, generates a secret, stores it in the System keychain (`duet.controller-secret`), applies it and reloads. Afterwards zashboard and any scripts need the secret:

```sh
sudo security find-generic-password -s duet.controller-secret -w /Library/Keychains/System.keychain
```

To undo it, roll back to the history entry before it (`duet history list`, then `duet history rollback <id>`). Then delete the keychain item; otherwise the next apply puts the secret back.

## Notes

- The helper never replaces or removes `com.mihomo.proxy`: `duet service install|uninstall mihomo` is refused for an adopted service. start/stop/restart work.
- The adopted subscription has no refresh interval. To have the helper fetch it, use `duet sub put adopted … --interval 720`. Be aware that a fetched subscription can differ from `raw-sub-original.yaml`.
- The leak guard is off after the adopt. Turn it on with `duet settings --leak-guard Google,Meta` (or whichever groups you want).
- `duet helper discard <plan id>` refuses the adopted plan, because it holds the exact backup (`--force` deletes it anyway).
