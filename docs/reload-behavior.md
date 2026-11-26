# What a mihomo hot reload does to traffic

Scope: mihomo v1.19.34 (`MetaCubeX/mihomo` tag `v1.19.34`, commit `0455d0d`), macOS, TUN in `mixed` stack, fake-ip DNS. This is the version the owner runs. The engine's `ReloadImpact` (`core/engine/reloadimpact.go`) mirrors the comparisons below. Re-check them when the kernel is upgraded.

Short answer: a reload of a config whose `tun` section, `dns.listen` and ports are unchanged does **not** interrupt the network. Open connections, DNS, fake-ip mappings and selected nodes survive. The only effect is a pause of a few milliseconds. A connection that is **opened** during the pause is closed at once, and UDP packets that arrive during it are dropped.

## The call path

`PUT /configs?force=…` (`hub/route/configs.go:updateConfigs`):

1. It parses the file (`executor.ParseWithPath`). This is the slow part: 15–200 ms for 9 000–69 000 rules. Traffic is not affected, because nothing has been swapped yet. A parse error returns 400, and the running config is left alone.
2. `executor.ApplyConfig(cfg, force)` runs under a global mutex:

```
tunnel.OnSuspend()                 ← pause starts
  ca, experimental, users
  updateProxies / updateRules      (map swap)
  updateSniffer, updateHosts, updateGeneral
  updateDNS                        (new resolver; fake-ip pool copied)
  updateNTP
  updateListeners(force)
  updateTun                        (re-created only if changed)
  updateIPTables (linux), updateTunnels
tunnel.OnInnerLoading()
  loadProvider(proxy providers)    (non-inline ones close their connections)
  updateProfile                    (store-selected: re-select saved nodes)
  loadProvider(rule providers)
  runtime.GC()
tunnel.OnRunning()                 ← pause ends
resolver.ResetConnection()         (upstream DNS connections re-dialed lazily)
```

The external controller is **not** part of this: `hub.applyRoute` (address, secret, TLS, CORS) runs only at start.

## Subsystem by subsystem

| Subsystem | What happens on reload | Source |
|---|---|---|
| **New TCP connections** | While the status is not `Running`, `handleTCPConn` closes every new connection at once. Through TUN, the client sees an immediate reset or EOF right after connect. Through the mixed port, it gets EOF. | `tunnel/tunnel.go:isHandle`, `handleTCPConn` |
| **UDP packets** | Dropped during the pause (`handleUDPConn` → `packet.Drop()`). This includes packets of existing UDP flows such as QUIC; their own retransmission covers it. | same |
| **Existing TCP connections** | Kept. Nothing iterates the connection table on reload. They keep their dialed outbound, even when the rule or proxy they matched has changed. Only `DELETE /connections` or a proxy provider closes them. | `statistic.DefaultManager.Range` is used only in `hub/route/connections.go` and `adapter/provider/provider.go` |
| **Proxy providers** | Every non-inline proxy provider closes all connections that go through it in `Initial()`, on **every** reload, even when nothing changed. Inline providers and the compatible providers behind groups with inline `proxies` do not. | `proxySetProvider.Initial` → `closeAllConnections` |
| **TUN** | `ReCreateTun` sorts the new tun config (`LC.Tun.Sort`: `dns-hijack`, `route-address(-set)`, `route-exclude-address(-set)`, include/exclude interface/uid/package/mac lists, `inet4/6-route(-exclude)-address`) and compares it field by field with the last one (`Equal`). If equal, it only calls `OnReload()` (re-sets dialer defaults). If different, it closes the TUN device (routes removed, utun destroyed, every flow through it cut) and creates a new one. **List order never counts.** The TUN address is `dns.fake-ip-range`'s address /30, so a change of that address re-creates the TUN too. `force` is ignored here. | `listener/listener.go:ReCreateTun`, `listener/config/tun.go`, `config/config.go:parseTun` |
| **DNS server listener** | Same `dns.listen` → `server.SetService(new)`: no re-bind and no gap. A different address (or `dns.enable` toggled) shuts the server down and binds again. | `dns/server.go:ReCreateServer` |
| **DNS hijack in TUN** | Answered in the TUN listener through `resolver.DefaultService`, not through the tunnel. It is not paused. | `listener/sing_tun/dns.go` |
| **DNS cache** | The new resolver starts with an empty cache, so the first lookups after a reload go upstream once. | `dns.NewResolver` |
| **Fake-ip pool** | The mapping and the allocation offset are copied into the new pool (`PatchFrom` → `CloneFrom`, when the range is unchanged). Existing fake IPs keep working. A name that newly matches `fake-ip-filter` gets a real IP from now on, and its old fake IP still maps back. | `hub/executor.updateDNS`, `dns/enhancer.go`, `component/fakeip/pool.go` |
| **Inbound listeners** (port, socks-port, mixed-port, redir/tproxy, allow-lan, bind-address, lan-allowed/disallowed-ips, skip-auth-prefixes, ss/vmess/tuic) | Applied only with `force=true`. Even then, a listener is closed and re-opened only if its address changed. `force=false` keeps the old ports when they changed. | `hub/executor.updateListeners`, `listener.ReCreate*` |
| **`listeners:`** | Patched on every reload: only changed entries are re-created. | `listener.PatchInboundListeners` |
| **Selected nodes** | With `profile.store-selected: true` (the default), the saved selection is applied to the new groups. With `false`, select groups go back to their first entry. | `hub/executor.updateProfile` |
| **Health checks** | `baseProvider.Initial` starts the health-check loop of url-test/fallback groups again (asynchronously). | `adapter/provider/provider.go` |
| **Rule providers** | `Initial()` reads the cached file or downloads it. Downloads happen during the `Inner` status, when only inner connections are served. | `loadProvider` |
| **Geodata** | Not reloaded. Only the URLs and mode setters are updated. | `updateGeneral` |
| **Controller** | Not touched: address, secret and CORS keep their values from start. | `hub/hub.go:applyRoute` |

## Measurements (TUN off, synthetic config, same binary)

Setup (`../../scratch/gentle/`): a second mihomo v1.19.34 run as the user, from `~/mihomo-migration/bin/mihomo`, with `-d scratch/gentle/run`. It has its own ports (mixed 17899, DNS 127.0.0.1:10053, controller 19490), TUN off, fake-ip DNS, DIRECT/REJECT only, and 9 000 or 69 000 synthetic rules. A local echo server is the target. During each reload:

- one long-lived TCP connection through the mixed port sent an echo every 20 ms;
- a DNS query loop hit :10053 every 50 ms;
- 3 workers opened new connections back to back (about 1 000 in 2.5 s).

The fake-ip of one name was read before and after each reload. 53 reloads in all. The tool is `scratch/gentle/measure` and the raw output is in `scratch/gentle/results-*.txt`.

| Scenario | Reloads | Long connection | DNS failures | New connections refused | Fake-ip kept |
|---|---|---|---|---|---|
| identical config, force=true | 5 | survived 5/5 | 0 | 0–12, window ≤ 15 ms | yes |
| identical config, force=false | 5 | survived 5/5 | 0 | 0–3, ≤ 2 ms | yes |
| adopt-like change (60 nameserver-policy keys, fake-ip-filter +1, hosts, head rules reordered), force=true / false | 10 | survived | 0 | 0–4, ≤ 10 ms | yes |
| back to base | 10 | survived | 0 | 0–3 | yes |
| tun.route-exclude-address reordered (TUN off: parse only) | 5 | survived | 0 | 0–3 | yes |
| 69 000 rules (RSS 53–73 MB, more than the live process's 36 MB), identical / adopt-like / back | 18 | survived | 0 | 0–4, ≤ 10 ms | yes |

Other checks:

- A secret added in the file and hot-reloaded → `GET /version` without a secret still returns 200. The secret is not applied.
- `mixed-port` changed with `force=false` → the old port keeps listening. With `force=true` → it moves.
- A node selected by hand (`PUT /proxies/PROXY`) is still selected after the reload (store-selected on).
- `force=true` and `force=false` behave the same when the ports are unchanged.
- PUT took 16–200 ms (mostly parsing). The refused connections always fall in the last few ms of it, which is the `OnSuspend…OnRunning` window.
- First run: a too aggressive connection loop (8 workers, no linger) ran out of loopback ports for 127.0.0.1:17899. That is an artefact of the test and only affected that port. The results above come from the throttled runs.

Not measured (TUN must not be touched on this Mac): the TUN path itself. The inference that it behaves the same rests on the source. TUN connections go through the same `handleTCPConn`/`handleUDPConn` gate. DNS hijack doesn't go through the gate. An unchanged tun config is not re-created.

## The owner's adopt

`ReloadImpact(live config.yaml, scratch/takeover/composed.yaml)` (`DUET_IMPACT_A/B … go test -run RealReloadImpact ./core/engine/`):

```
disruptive=false force=false tun_rebuild=false dns_listener=false listeners=[] providers=[] selection_reset=false restart_needed=[]
```

- The tun sections differ only in the order of `route-exclude-address`, which mihomo sorts away.
- `dns.listen` (127.0.0.1:53) and the ports are the same.
- There are no proxy providers. `store-selected` is on.
- `secret: ""` vs no secret counts as the same.

## What duet does with this

- `ReloadImpact(old, new)` is computed on every apply (`ApplyResult.impact`) and stored with every takeover plan (`TakeoverPlan.impact`). `duet helper adopt` prints it as the `reload` line.
- A disruptive impact (TUN rebuild, DNS re-bind, changed ports, provider connections) adds a warning. The app shows "会短暂中断网络" from `impact.disruptive`.
- The reload is sent with `force=false` unless an inbound setting changed, or there was no previous file. With unchanged ports this makes no measurable difference. The reload just does strictly less.
- After a reload, the engine verifies through the **same** controller address and secret, because a hot reload never moves the controller. Before, it built the controller from the new file, which fails when the address changes.
- The engine tries the file's secret, the last secret that worked, the stored secret and none, and moves on after a 401. So a secret written by a reload but not yet active (it needs a restart) no longer makes the helper think mihomo is down.
- Compose does not keep the live order of `route-exclude-address`/`fake-ip-filter`. Mihomo sorts the tun lists before comparing, and DNS lists never re-create anything, so the order has no effect on traffic. `ReloadImpact` does the same sort, which proves the TUN is unchanged.
