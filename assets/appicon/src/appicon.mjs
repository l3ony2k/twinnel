// Writes the app icon masters (SVG) from code, so every platform's icon comes from one drawing.
//   node assets/appicon/src/appicon.mjs        → assets/appicon/src/*.svg
//   go run ./assets/appicon/src/raster.go       → .icns, .ico, hicolor PNGs, tray PNGs
//
// The mark is the twinnel rings (../../icons/src/twinnel.mjs): two rings that link like chain links,
// mihomo on the left, Tailscale on the right; the left ring is in front at the top crossing, the
// right one at the bottom. Geometry is the same as the line icon (24 grid, outer radius 6.75,
// centres 8.75 / 15.25), drawn heavier (stroke 2.5) for an app icon.
//
// Platforms:
//   mac.svg        1024 canvas, macOS grid: an 824 continuous-corner square (superellipse) at 100,
//                  with the grid's drop shadow; dark plate, light rings.
//   win.svg        256 canvas, Windows: rounded square plate filling the tile, no shadow.
//   win-small.svg  the ≤ 32 px tiles: plate to the edge, rings larger and heavier so they survive.
//   linux.svg      128 canvas, GNOME/hicolor style: 104 rounded square with a darker bottom edge.
import fs from 'node:fs';
import path from 'node:path';

const here = path.dirname(new URL(import.meta.url).pathname);
const f = (v) => +(+v).toFixed(3);

const COLORS = {
  top: '#3B4A78', bottom: '#171D33', // plate gradient: deep indigo
  left: '#FFFFFF', right: '#A9C6FF', // mihomo ring white, Tailscale ring pale blue
  edge: '#0E1222'                    // GNOME bottom edge
};

// The linked rings in a 24 grid, placed with scale k at (ox, oy) (top-left of the grid), stroke s
// in grid units. Each ring is cut by a band round the other ring's front arc (as kit cuts do).
function rings(id, k, ox, oy, s = 2.5, { left = COLORS.left, right = COLORS.right } = {}) {
  const R = 6.75, LX = 2 + R, RX = 22 - R, CY = 12, r = R - s / 2;
  const gap = 0.5 * s + 0.75, w = s + gap;
  const dy = Math.sqrt(r * r - (12 - LX) ** 2);
  const deg = (a) => (a * 180) / Math.PI;
  const aL = deg(Math.atan2(-dy, 12 - LX)), aR = deg(Math.atan2(dy, 12 - RX)), span = 28;
  const P = (x, y) => `${f(ox + x * k)} ${f(oy + y * k)}`;
  const at = (cx, cy, a) => P(cx + r * Math.cos((a * Math.PI) / 180), cy + r * Math.sin((a * Math.PI) / 180));
  const arc = (cx, cy, a0, a1) => `M${at(cx, cy, a0)}A${f(r * k)} ${f(r * k)} 0 0 1 ${at(cx, cy, a1)}`;
  const circle = (cx, cy, col, mask) => `<circle cx="${f(ox + cx * k)}" cy="${f(oy + cy * k)}" r="${f(r * k)}" fill="none" stroke="${col}" stroke-width="${f(s * k)}"${mask ? ` mask="url(#${mask})"` : ''}/>`;
  const big = `x="-2000" y="-2000" width="5000" height="5000"`;
  const mask = (mid, d) => `<mask id="${mid}" maskUnits="userSpaceOnUse" ${big}><rect ${big} fill="#fff"/><path d="${d}" fill="none" stroke="#000" stroke-width="${f(w * k)}" stroke-linecap="round"/></mask>`;
  const leftFront = arc(LX, CY, aL - span, aL + span), rightFront = arc(RX, CY, aR - span, aR + span);
  return {
    defs: mask(`${id}-r`, leftFront) + mask(`${id}-l`, rightFront),
    body: circle(RX, CY, right, `${id}-r`) + circle(LX, CY, left, `${id}-l`) +
      `<path d="${rightFront}" fill="none" stroke="${right}" stroke-width="${f(s * k)}" stroke-linecap="round"/>`
  };
}

// A superellipse |x|^n + |y|^n = 1 (Apple's continuous-corner icon shape is close to n = 5).
function squircle(cx, cy, half, n = 5, steps = 360) {
  let d = '';
  for (let i = 0; i < steps; i++) {
    const t = (i / steps) * 2 * Math.PI, c = Math.cos(t), s = Math.sin(t);
    const x = cx + half * Math.sign(c) * Math.abs(c) ** (2 / n), y = cy + half * Math.sign(s) * Math.abs(s) ** (2 / n);
    d += `${i ? 'L' : 'M'}${f(x)} ${f(y)}`;
  }
  return d + 'Z';
}
const rrect = (x, y, w, h, r) => `M${x + r} ${y}H${x + w - r}A${r} ${r} 0 0 1 ${x + w} ${y + r}V${y + h - r}A${r} ${r} 0 0 1 ${x + w - r} ${y + h}H${x + r}A${r} ${r} 0 0 1 ${x} ${y + h - r}V${y + r}A${r} ${r} 0 0 1 ${x + r} ${y}Z`;
const grad = (id, x1, y1, x2, y2, a = COLORS.top, b = COLORS.bottom) =>
  `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}"><stop offset="0" stop-color="${a}"/><stop offset="1" stop-color="${b}"/></linearGradient>`;
const sheen = (id, y1, y2) => `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="0" y1="${y1}" x2="0" y2="${y2}"><stop offset="0" stop-color="#fff" stop-opacity="0.16"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></linearGradient>`;
const svg = (size, defs, body) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}"><defs>${defs}</defs>${body}</svg>\n`;

// macOS: 824 body at 100, drop shadow (the grid's: about 10 down, 20 soft, 30% black) built from
// stacked translucent copies because the renderer has no blur filter.
{
  const plate = squircle(512, 512, 412);
  let shadow = '';
  for (let i = 8; i >= 1; i--) shadow += `<path d="${squircle(512, 512 + 10, 412 + i * 2.5)}" fill="#000" fill-opacity="0.035"/>`;
  const k = 26, ox = 512 - 12 * k, oy = 512 - 12 * k;
  const m = rings('mac', k, ox, oy);
  fs.writeFileSync(path.join(here, 'mac.svg'), svg(1024,
    grad('plate', 0, 100, 0, 924) + sheen('sheen', 100, 560) + m.defs,
    shadow + `<path d="${plate}" fill="url(#plate)"/><path d="${plate}" fill="url(#sheen)"/>` +
    `<path d="${squircle(512, 512, 410.5)}" fill="none" stroke="#fff" stroke-opacity="0.14" stroke-width="3"/>` + m.body));
}
// Windows 256: rounded square plate across the tile (Fluent-style soft radius), no shadow.
{
  const k = 8.2, ox = 128 - 12 * k, oy = 128 - 12 * k, m = rings('win', k, ox, oy);
  const plate = rrect(8, 8, 240, 240, 52);
  fs.writeFileSync(path.join(here, 'win.svg'), svg(256, grad('plate', 0, 8, 0, 248) + sheen('sheen', 8, 140) + m.defs,
    `<path d="${plate}" fill="url(#plate)"/><path d="${plate}" fill="url(#sheen)"/>` + m.body));
}
// ≤ 32 px: plate to the edge, rings wider and heavier (stroke 3 in the 24 grid).
{
  const k = 1.3, ox = 16 - 12 * k, oy = 16 - 12 * k, m = rings('sm', k, ox, oy, 3);
  fs.writeFileSync(path.join(here, 'win-small.svg'), svg(32, grad('plate', 0, 0, 0, 32) + m.defs,
    `<path d="${rrect(0, 0, 32, 32, 6)}" fill="url(#plate)"/>` + m.body));
}
// GNOME/hicolor 128: 104 square with radius 18 at 12, a 4 px darker edge at the bottom.
{
  const k = 4.1, ox = 64 - 12 * k, oy = 62 - 12 * k, m = rings('gn', k, ox, oy);
  fs.writeFileSync(path.join(here, 'linux.svg'), svg(128, grad('plate', 0, 12, 0, 112) + m.defs,
    `<path d="${rrect(12, 14, 104, 104, 18)}" fill="${COLORS.edge}"/><path d="${rrect(12, 10, 104, 104, 18)}" fill="url(#plate)"/>` + m.body));
}
// macOS 26 layered icon (Icon Composer): the rings as two flat layers on a 1024 canvas, no plate,
// no masks (Icon Composer draws the background, shape, glass and shadow itself). Each ring is
// one filled outline: the annulus with its gap cut by the other ring's band, as in rings() above.
// icon.json in ../twinnel.icon is hand-written; this only refreshes the layer artwork.
{
  const R = 6.75, s = 2.5, r = R - s / 2, w = s + 0.5 * s + 0.75, k = 26, o = 512 - 12 * k;
  const L = [o + (2 + R) * k, 512], Rc = [o + (22 - R) * k, 512];
  // the outline of the ring round c whose gap is the band (radius r ± w/2) round the other centre
  // oc, on the side ys (+1 bottom crossing, -1 top crossing)
  const cut = (c, oc, ys) => {
    const ro = (r + s / 2) * k, ri = (r - s / 2) * k, bo = (r + w / 2) * k, bi = (r - w / 2) * k;
    const d = Math.abs(oc[0] - c[0]), dir = Math.sign(oc[0] - c[0]);
    // intersection of the circle radius a round c with radius b round oc, on side ys
    const meet = (a, b) => { const x = (a * a - b * b + d * d) / (2 * d); return [c[0] + dir * x, c[1] + ys * Math.sqrt(a * a - x * x)]; };
    const ang = (p) => Math.atan2(p[1] - c[1], p[0] - c[0]);
    const P = (p) => `${f(p[0])} ${f(p[1])}`;
    const oIn = meet(ro, bi), oOut = meet(ro, bo), iIn = meet(ri, bi), iOut = meet(ri, bo);
    let gap = ang(oIn) - ang(oOut);
    gap = Math.atan2(Math.sin(gap), Math.cos(gap));
    // go the long way round, in the positive direction, from the end the gap leaves from
    const [oA, oB, iA, iB, bA, bB] = gap > 0 ? [oIn, oOut, iIn, iOut, bi, bo] : [oOut, oIn, iOut, iIn, bo, bi];
    const sw = (p, q) => ((p[0] - oc[0]) * (q[1] - oc[1]) - (p[1] - oc[1]) * (q[0] - oc[0]) > 0 ? 1 : 0);
    return `M${P(oA)}A${f(ro)} ${f(ro)} 0 1 1 ${P(oB)}A${f(bB)} ${f(bB)} 0 0 ${sw(oB, iB)} ${P(iB)}` +
      `A${f(ri)} ${f(ri)} 0 1 0 ${P(iA)}A${f(bA)} ${f(bA)} 0 0 ${sw(iA, oA)} ${P(oA)}Z`;
  };
  const dir = path.join(here, '..', 'twinnel.icon', 'Assets');
  fs.mkdirSync(dir, { recursive: true });
  const layer = (name, d, col) => fs.writeFileSync(path.join(dir, name),
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024"><path d="${d}" fill="${col}"/></svg>\n`);
  layer('mihomo-ring.svg', cut(L, Rc, 1), COLORS.left);
  layer('tailscale-ring.svg', cut(Rc, L, -1), COLORS.right);
}
console.log('mac.svg win.svg win-small.svg linux.svg twinnel.icon/Assets/*.svg →', here);
