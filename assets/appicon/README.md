# App icon

One drawing, the duet rings (mihomo white on the left, Tailscale pale blue on the right, linked
like chain links) on a deep indigo plate, adapted to each platform's convention. Sources in
`src/`; everything else is generated (`node src/appicon.mjs`, then `go run ./assets/appicon/src/raster.go`
from the repo root; the rasteriser is MyGo's own SVG renderer, so no browser is needed).

| File | Platform | Notes |
|---|---|---|
| `macos/duet.icns` | macOS | 16–1024 incl. @2x, PNG entries. macOS grid: 824 continuous-corner (superellipse) body at 100 on a 1024 canvas, the grid's drop shadow, a faint top sheen and rim |
| `macos/duet-1024.png` | macOS | the master, for docs / the DMG background |
| `duet-1024.png` | mygo.json `icon` | flat plate without the macOS shadow, for platforms other than macOS |
| `windows/duet.ico` | Windows | 16, 20, 24, 32 (simplified: plate to the edge, heavier rings), 40–256 (full) |
| `linux/hicolor/<n>x<n>/apps/duet.png` | Linux | 16–512, GNOME style: rounded square with a darker bottom edge |
| `linux/hicolor/scalable/apps/duet.svg` | Linux | the same, scalable |
| `linux/hicolor/symbolic/apps/duet-symbolic.svg` | Linux | monochrome, for symbolic contexts |

Wiring (owned by the app/release scripts, not done here): in `cmd/app/mygo.json` set
`"icon": "../../assets/appicon/duet-1024.png"` and `"macos": { "icon": "../../assets/appicon/macos/duet.icns" }`.

The drawing is ours (MIT, with the repo).
