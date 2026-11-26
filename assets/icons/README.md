# Icons

Line icons in the Kubai line style (24 grid, stroke 2, outline variant, black;
`ui.Icon` recolors them with the text color). Exported with the kubai-line kit
(`export.mjs --stroke 2 --variant outline --color '#000000'`).

| File | Used for |
|---|---|
| `layout-dashboard.svg` | 总览 |
| `split.svg` | 策略组 |
| `monitor-smartphone.svg` | Tailscale 设备 |
| `heart-pulse.svg` | 健康检查 |
| `list.svg` | 日志 |
| `settings.svg` | 设置 |
| `rotate-cw.svg` | 刷新 / 重新检查 / 重载配置 |
| `copy.svg` | 复制 |
| `gauge.svg` | 测速 |
| `log-out.svg` | 出口节点 |
| `triangle-alert.svg`, `circle-check.svg`, `circle-x.svg`, `circle-minus.svg` | 警告 / 正常 / 错误 / 跳过 |
| `power.svg` | Tailscale 启停 |
| `globe.svg` | TUN |
| `eraser.svg` | 清空缓存 |
| `chevron-right.svg`, `chevron-down.svg` | 折叠 / 展开 |
| `duet.svg` | app 标志 |
| `tray/duet*.png` | menu bar template icon, 32×32 (16 pt @2x), one per state (below) |

`duet` is new: two interlocked rings (mihomo and Tailscale), drawn with the kit
in `src/duet.mjs` (needs the kit next to it to run; kept here as the source).
The tray PNGs are rendered from it with headless Chrome (masks included).

## Menu bar states (shape only)

The menu bar icon is a monochrome template image: macOS tints it for the
light or dark bar, so a state may never depend on color. Each state has its
own shape; `status.TrayIconFor(snapshot)` picks one (in that order of
priority):

| File | Shape | State |
|---|---|---|
| `duet-unknown.png` | both rings dashed | starting, nothing answered yet |
| `duet-error.png` | "x" in the notch | mihomo unreachable / wrong secret, or another red reason |
| `duet-tsdown.png` | right (Tailscale) ring broken, left whole | mihomo fine, Tailscale down or stopped |
| `duet-warn.png` | "!" in the notch | a yellow reason |
| `duet-mode.png` | "→" in the notch | mihomo in direct or global mode (not rule) |
| `duet-nohelper.png` | hollow ring in the notch | helper not installed / not running / refused |
| `duet.png` | two whole interlocked rings | all fine |

Render (kit next to the repo): copy `src/duet.mjs` to the kit's `set/duet.mjs`, then
`node raster.mjs --only duet,duet-warn,duet-error,duet-unknown,duet-tsdown,duet-mode,duet-nohelper --size 32 --stroke 2 --color '#000' --out <dir>`
with an absolute `<dir>`.
