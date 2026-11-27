// Regenerates every SVG in assets/icons from its source. Run from the repo root:
//   node assets/icons/src/export.mjs --kit <kubai-line kit>
// then the rasters:   go run ./assets/appicon/src/raster.go
//
// Three sources (see ../README.md for which icon comes from where):
//   STOCK    icons from the Kubai line library, exported by name with the kit (stroke 2, outline)
//   duet.mjs the app mark and the menu bar states, drawn in the kit's format (copied into
//            <kit>/set/duet.mjs and registered in <kit>/set/index.mjs before exporting)
//   drawn/   product icons drawn with the draw-icons skill: sources in drawn/source/, built by
//            `node <draw-icons>/scripts/icons.js build` (run in assets/icons) into drawn/*.svg
// The kit and the draw-icons skill stay outside the repo (their licences keep the tooling out).
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const args = process.argv.slice(2);
const opt = (k, d) => { const i = args.indexOf(k); return i >= 0 ? args[i + 1] : d; };
const kit = path.resolve(opt('--kit', '../icons-kit'));
const here = path.dirname(new URL(import.meta.url).pathname);
const icons = path.resolve(here, '..');

// file name in assets/icons ← stock icon name in the kit (same name unless noted)
export const STOCK = [
  'layout-dashboard', 'split', 'monitor-smartphone', 'heart-pulse', 'list', 'settings', 'rotate-cw',
  'copy', 'gauge', 'log-out', 'triangle-alert', 'circle-check', 'circle-x', 'circle-minus', 'power',
  'globe', 'eraser', 'chevron-right', 'chevron-down',
  // added in the design pass
  'layers', 'history', 'undo-2', 'cpu', 'rss', 'square-pen', 'file-lock', 'server', 'map',
  'app-window', 'import', 'key-round',
  // added by ui-final
  'file-diff', 'eye',
  // added by pages-rest (日志 toolbar)
  'pause', 'play'
];
const DUET = ['duet'];
const TRAY = ['duet', 'duet-warn', 'duet-error', 'duet-unknown', 'duet-tsdown', 'duet-mode', 'duet-nohelper'];

// 1. keep the kit's copy of duet.mjs in step with ours
const kitDuet = path.join(kit, 'set', 'duet.mjs');
fs.copyFileSync(path.join(here, 'duet.mjs'), kitDuet);
const idx = path.join(kit, 'set', 'index.mjs');
if (!fs.readFileSync(idx, 'utf8').includes('duet.mjs')) throw new Error(`register ./duet.mjs in ${idx} first`);

// 2. stock + duet, outline at stroke 2 in black (ui.Icon recolours them with the text colour)
execFileSync('node', ['export.mjs', '--only', [...STOCK, ...DUET].join(','), '--stroke', '2', '--variant', 'outline', '--color', '#000000', '--out', icons], { cwd: kit, stdio: 'inherit' });

// 3. product icons built by draw-icons (currentColor), copied next to the rest
const drawn = path.join(icons, 'drawn');
for (const f of fs.readdirSync(drawn).filter((f) => f.endsWith('.svg'))) fs.copyFileSync(path.join(drawn, f), path.join(icons, f));

// 4. menu bar / tray marks as SVG sources for the rasters (raster.go): stroke 1.5 (the kit's
//    light weight: SF Symbols' regular weight in the menu bar is about 1.4 pt), two crops:
//    tray/svg/<name>.svg       wide crop, 19.5 units tall around the rings (macOS menu bar)
//    tray/svg/<name>-sq.svg    the 24 grid (Windows tray, Linux symbolic)
const { render } = await import(pathToFileURL(path.join(kit, 'kit.mjs')));
const { ALL } = await import(pathToFileURL(path.join(kit, 'icons.mjs')) + `?t=${Date.now()}`);
const svgDir = path.join(icons, 'tray', 'svg');
fs.mkdirSync(svgDir, { recursive: true });
for (const name of TRAY) {
  const svg = render(ALL[name], { s: 1.5, variant: 'outline', color: '#000000', tones: { dark: '#000000' }, size: 24, id: name });
  fs.writeFileSync(path.join(svgDir, `${name}-sq.svg`), svg + '\n');
  // y 2.25..21.75 keeps the rings centred and leaves room for the notch marks ("!" reaches 21.7)
  fs.writeFileSync(path.join(svgDir, `${name}.svg`), svg.replace(/viewBox="[^"]*"/, 'viewBox="1 2.25 22 19.5"').replace(/width="24" height="24"/, 'width="22" height="19.5"') + '\n');
}
console.log(`stock ${STOCK.length} + duet + drawn ${fs.readdirSync(drawn).filter((f) => f.endsWith('.svg')).length} → ${icons}; tray ${TRAY.length} → ${svgDir}`);
