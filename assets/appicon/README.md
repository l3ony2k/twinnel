# App icon

One drawing, the duet rings (mihomo white on the left, Tailscale pale blue on the right, linked
like chain links) on a deep indigo plate, adapted to each platform's convention. Sources in
`src/`; everything else is generated (`node src/appicon.mjs`, then `go run ./assets/appicon/src/raster.go`
from the repo root; the rasteriser is MyGo's own SVG renderer, so no browser is needed).

| File | Platform | Notes |
|---|---|---|
| `duet.icon/` | macOS 26 | Icon Composer document, the layered icon (see below) |
| `macos/duet.icns` | macOS ≤ 15 (and the fallback) | 16–1024 incl. @2x, PNG entries. macOS grid: 824 continuous-corner (superellipse) body at 100 on a 1024 canvas, the grid's drop shadow, a faint top sheen and rim |
| `macos/duet-1024.png` | macOS | the master, for docs / the DMG background |
| `duet-1024.png` | mygo.json `icon` | flat plate without the macOS shadow, for platforms other than macOS |
| `windows/duet.ico` | Windows | 16, 20, 24, 32 (simplified: plate to the edge, heavier rings), 40–256 (full) |
| `linux/hicolor/<n>x<n>/apps/duet.png` | Linux | 16–512, GNOME style: rounded square with a darker bottom edge |
| `linux/hicolor/scalable/apps/duet.svg` | Linux | the same, scalable |
| `linux/hicolor/symbolic/apps/duet-symbolic.svg` | Linux | monochrome, for symbolic contexts |

Wiring (owned by the app/release scripts, not done here): in `cmd/app/mygo.json` set
`"icon": "../../assets/appicon/duet-1024.png"` and `"macos": { "icon": "../../assets/appicon/macos/duet.icns" }`.

The drawing is ours (MIT, with the repo).

## macOS 26: the layered icon (`duet.icon`)

macOS 26 draws app icons itself from layers: it adds the plate shape, Liquid Glass, specular
highlights and shadows, and re-colours the icon for each appearance (Default, Dark, Clear light /
dark, Tinted). `duet.icon` is that source, an Icon Composer document:

```
duet.icon/
  icon.json                  background fill, groups, per-appearance settings
  Assets/mihomo-ring.svg     the white ring (front group)
  Assets/tailscale-ring.svg  the pale-blue ring (back group)
```

- **Background**: the icon's `fill`, not a layer. Default: the indigo gradient `#3B4A78 → #171D33`
  (same as the `.icns`); Dark: a deeper `#252D4A → #0B0E1A`. Tinted and Clear: the system's.
- **Rings**: one layer per ring, each in its own group (the first group in `icon.json` is in
  front), glass on, neutral shadow, specular on. The SVGs are flat paths on a 1024 canvas with
  the ring's own colour; each ring's gap where the other passes over it is cut into its outline
  (no masks: Icon Composer only takes plain shapes).
- **Tinted / Clear** (`"appearance" : "tinted"` in `icon.json`, which Icon Composer calls Mono):
  the mihomo ring is white, the Tailscale ring 70 % grey so the two stay apart, and both groups
  turn translucency on (35 % / 50 %) so Clear shows the wallpaper through them. Default and Dark
  keep the rings opaque.

### Editing

Open `duet.icon` in Icon Composer (Xcode 26: Xcode → Open Developer Tool → Icon Composer, or the
standalone download). It previews every appearance and the macOS, light/dark and tint variants
live; save writes `icon.json` (and copies of any new layer files into `Assets/`) in place. Commit
what it writes.

The ring geometry comes from `src/appicon.mjs` like every other icon: `node src/appicon.mjs`
rewrites the two SVGs in `Assets/` (not `icon.json`). Keep the layer file names, or update the
`image-name` entries in `icon.json` to match.

### How it ships

`scripts/build-release.sh` compiles it when `actool` (Xcode 26) is available:

```sh
xcrun actool assets/appicon/duet.icon --compile <out> --platform macosx --target-device mac \
  --minimum-deployment-target 26.0 --app-icon duet --output-partial-info-plist <out>/partial.plist
```

then copies `<out>/Assets.car` to `duet.app/Contents/Resources/` and sets `CFBundleIconName` =
`duet` (the name `actool` gives the icon is the document's name, so `--app-icon` must stay `duet`).
`AppIcon.icns` (from `macos/duet.icns`) and `CFBundleIconFile` stay in the bundle for macOS 15 and
earlier. Without `actool` the build warns and the app keeps only the `.icns`;
`DUET_REQUIRE_LAYERED_ICON=1` turns that into an error for release CI. See `docs/release.md`.

`mygo.json`'s `macos.icon` stays the `.icns`: MyGo can compile a `.icon` itself, but it then
replaces our drawn `.icns` with the one `actool` renders, and without Xcode it falls back to the
flat PNG instead of the `.icns`.
