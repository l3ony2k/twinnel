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
| `tray/duet*.png` | menu bar template icon, 32×32 (16 pt @2x), ok / warn / error |

`duet` is new: two interlocked rings (mihomo and Tailscale), drawn with the kit
in `src/duet.mjs` (needs the kit next to it to run; kept here as the source).
The tray PNGs are rendered from it with headless Chrome (masks included).
