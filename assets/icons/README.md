# Icons

Everything here is generated; edit the sources, never the SVG/PNG outputs.

| What | Where | Made with |
|---|---|---|
| In-app line icons | `*.svg` (embedded by `icons.go`) | Kubai line library (stock) and the draw-icons skill (product concepts) |
| App mark | `duet.svg` | `src/duet.mjs`, kubai-line kit format |
| Menu bar / tray marks | `tray/` | `src/duet.mjs` → `tray/svg/` → `../appicon/src/raster.go` |
| App icon (macOS, Windows, Linux) | `../appicon/` | `../appicon/src/appicon.mjs` → `raster.go` (see its README) |

## Icon language (the rules)

One style for the whole in-app set, the Kubai **line** style, because the icons sit next to
text in the sidebar, buttons and list rows:

- 24 grid, **stroke 2**, round caps and joins, outline only, one colour. The SVGs are black or
  `currentColor`; `ui.Icon` recolours them with the text colour, so state colour comes from the
  text colour of the row, never from the drawing.
- Keylines on the **outer ink edge**: circle Ø20, square 18, landscape 20×16, portrait 16×20,
  2 units of padding. Straight edges on whole units (crisp at 24 px), optical centring.
- Corners: smooth (squircle) corners, about 0.24 of the short side; concave corners square.
- Parts: separate parts at least 1.5–1.75 apart (never "almost touching"); at most three detail
  strokes on a face; solid shapes only for dots that would close up as outlines.
- Metaphors: show the object, not the action; use the known convention where there is one
  (gear, history clock, cpu chip); a product concept is a known base plus one modifier
  (shield + droplet = leak guard). No letters or numbers inside icons.
- Sizes: drawn for 14, 16, 20 and 24 px (the sizes `cmd/app` uses). Every icon is checked at
  those sizes before it ships.
- Product concepts not in the library are drawn with the **draw-icons** skill. Its project file
  is `icons.yaml` (DNA matched to the Kubai line style: centreline keylines one stroke smaller,
  squircle corners), sources in `drawn/source/`, built files in `drawn/` and copied here.
- Menu bar marks follow the menu bar rules below, not the in-app ones.

## In-app icons

`cmd/app/icons.go` loads `<name>.svg`. File names say what the icon shows; the table says
what it stands for in duet. **New** = added in the design pass and not yet wired in `cmd/app`.

| File | Stands for | Source |
|---|---|---|
| `layout-dashboard.svg` | 总览 | stock |
| `split.svg` | 策略组 (rule routing) | stock |
| `monitor-smartphone.svg` | Tailscale 设备 | stock |
| `heart-pulse.svg` | 健康检查 | stock |
| `layers.svg` **new** | 订阅与覆写 (sidebar: the config is built from stacked layers) | stock |
| `history.svg` **new** | 历史 (sidebar) | stock |
| `cpu.svg` **new** | 内核 (sidebar; the mihomo core) | stock |
| `list.svg` | 日志 | stock |
| `settings.svg` | 设置 | stock |
| `info.svg` | 关于 (sidebar, last row) | drawn |
| `rss.svg` **new** | 订阅 (a subscription: a remote feed fetched on a schedule) | stock |
| `square-pen.svg` **new** | 覆写层 (an override layer the user edits) | stock |
| `file-lock.svg` **new** | 托管层 (the managed layer duet owns; not hand-edited) | stock |
| `undo-2.svg` **new** | 回滚 (roll back to a history entry) | stock |
| `server.svg` **new** | 服务 (mihomo / tailscaled services) | stock |
| `map.svg` **new** | 地理数据库 (GeoIP / GeoSite) | stock |
| `app-window.svg` **new** | 外部面板 (external UI) | stock |
| `import.svg` **new** | 接管现有配置 (takeover / adopt) | stock |
| `key-round.svg` **new** | 加固控制接口 (controller secret) | stock |
| `file-diff.svg` | 预览 (what an apply would change: the composed config against the running one) | stock |
| `eye.svg` | 查看 (show a read-only view; kept for a future 查看 action) | stock |
| `shield-droplet.svg` **new** | 防泄漏 (leak guard) | drawn |
| `tunnel.svg` **new** | TUN 模式 (use instead of `globe.svg` for TUN) | drawn |
| `nodes-direct.svg` **new** | 直连 (Tailscale peer reached directly) | drawn |
| `nodes-relay.svg` **new** | DERP 中继 (peer reached through a relay box) | drawn |
| `log-out.svg` | 出口节点 | stock |
| `rotate-cw.svg` | 刷新 / 重新检查 / 重载配置 | stock |
| `copy.svg` | 复制 | stock |
| `gauge.svg` | 测速 | stock |
| `power.svg` | Tailscale 启停 | stock |
| `globe.svg` | global mode / internet (no longer the TUN icon) | stock |
| `eraser.svg` | 清空缓存 | stock |
| `triangle-alert.svg`, `circle-check.svg`, `circle-x.svg`, `circle-minus.svg` | 警告 / 正常 / 错误 / 跳过 | stock |
| `chevron-right.svg`, `chevron-down.svg` | 折叠 / 展开 | stock |
| `duet.svg` | app mark (sidebar, onboarding) | `src/duet.mjs` |

## Menu bar marks (shape only)

The menu bar icon is a monochrome **template** image: macOS tints it for the light or dark bar,
so a state may never depend on colour. Each state has its own shape (`status.TrayIconFor`
picks one, highest priority first):

| File | Shape | State |
|---|---|---|
| `duet-unknown.png` | two rings side by side, **not linked** | starting, nothing answered yet |
| `duet-error.png` | "×" in the notch | mihomo unreachable / wrong secret, or another red reason |
| `duet-tsdown.png` | linked rings, **slash over the right (Tailscale) ring** | mihomo fine, Tailscale down or stopped |
| `duet-warn.png` | "!" in the notch | a yellow reason |
| `duet-mode.png` | "→" in the notch | mihomo in direct or global mode (not rule) |
| `duet-nohelper.png` | "–" in the notch | helper not installed / not running / refused |
| `duet.png` | two linked rings | all fine |

- Size: mygo draws tray images **18 pt tall** and keeps the aspect ratio, so the marks are a wide
  crop (22×19.5 units of the 24 grid, same crop for every state so nothing jumps).
  `tray/<state>.png` is @2x (41×36 px, the file the app loads), `tray/<state>@1x.png` is 20×18.
- Stroke **1.5** (the kit's light weight; ≈1.4 pt, close to SF Symbols regular in the menu bar).
- Windows notification area: `tray/windows/<state>-dark.ico` (white, for the default dark
  taskbar) and `-light.ico` (black), 16–48 px. Linux: `tray/linux/<state>-symbolic.svg`
  (`#2e3436`, recoloured by GTK). Both use the square 24-grid crop (`tray/svg/<state>-sq.svg`).

## Re-rendering

Needs: a copy of the kubai-line kit outside the repo (`../../icons-kit` here; the kit's licence
keeps its files and rules out of this repo) with `./duet.mjs` registered in `set/index.mjs`,
the draw-icons skill, Node 18+, Chrome (the kit's checks), Go.

```sh
(cd assets/icons && node <draw-icons>/scripts/icons.js lint all && node <draw-icons>/scripts/icons.js build)
node assets/icons/src/export.mjs --kit <kit>       # all SVGs here + tray/svg
node assets/appicon/src/appicon.mjs                # app icon masters
go run ./assets/appicon/src/raster.go              # every PNG / ICO / ICNS
```

Checks: `node check.mjs --only <names>` in the kit (stock and `duet*` icons: no findings at
strokes 1.5–2.5) and `icons.js lint all` / `render` / `sheet` for the drawn ones (no errors, no
open warnings; waivers are written on the sources).
