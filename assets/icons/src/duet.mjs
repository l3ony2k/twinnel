// duet: the app mark, drawn by the rules in GUIDE.md (kubai line style).
// Two rings that cross: mihomo and Tailscale, the two cores running side by side. The right
// ring is in front and cuts the left one where they cross. Status variants put a mark in a
// round notch at the bottom right (a "!" for a warning, an "x" for an error), as kit.badge does.
// The menu bar icon is a monochrome template: every state is told apart by shape alone.
//   duet            all fine
//   duet-warn       "!" in the notch
//   duet-error      "x" in the notch
//   duet-unknown    two rings side by side, not linked: still connecting / not known yet
//   duet-tsdown     the linked pair with a slash over the right (Tailscale) ring: Tailscale is down
//   duet-mode       "→" in the notch: mihomo is not in rule mode (direct or global)
//   duet-nohelper   "–" in the notch: the helper is not installed / not connected
import { n, P, shell, line, solid } from '../parts.mjs';
import { gapOf } from '../kit.mjs';
import { circ } from '../smooth.mjs';

const R = 6.75; // outer radius of each ring
const LX = 2 + R, RX = 22 - R, CY = 12;

const duet = {
  label: '双核', key: 'landscape', fit: 'x',
  light: [], // one mark: both rings keep the dark tone
  allow: { sliver: '只交付线性变体（菜单栏模板图标和界面图标）；面性中两个圆盘交叠，扣环处的切口必然留下小块，面性不使用' },
  plan: '主体：两个扣在一起的圆环（左 mihomo，右 Tailscale），像两节链环：上面的交叉处左环在前，下面的交叉处右环在前',
  weights: { [P]: 1 },
  draw(g) {
    // Crossings of the two centre lines: x = 12, y = CY ± dy. Each ring goes in front at one of
    // them: a short arc of it over the crossing cuts a clear band out of the other ring (cut), and
    // the arc that redraws the right ring shares its part, so it does not cut the ring it lies on.
    const r = R - g.h, dy = Math.sqrt(r * r - (12 - LX) ** 2), w = g.s + g.g;
    const aL = (Math.atan2(-dy, 12 - LX) * 180) / Math.PI, aR = (Math.atan2(dy, 12 - RX) * 180) / Math.PI, span = 28;
    return [
      shell(g.ring(RX, CY, R), { n: 'right', role: P, part: 'right' }),
      shell(g.ring(LX, CY, R), { n: 'left', role: P, part: 'left', cut: { d: g.arc(LX, CY, r, aL - span, aL + span), w } }),
      line(g.arc(RX, CY, r, aR - span, aR + span), { n: 'right-front', role: P, part: 'right', cut: { d: g.arc(RX, CY, r, aR - span, aR + span), w } })
    ];
  },
  // the two cores move apart and come back together
  fx: [{ t: 'keys', on: ['left'], x: [-1, 0.2] }, { t: 'keys', on: ['right', 'right-front'], x: [1, -0.2] }]
};

const MARK = 3.25; // smaller than kit.badge's 4: the rings must stay readable at 16 pt
const BX = 19, BY = 20 - MARK + 0.75; // the mark's centre, tucked into the corner
const MARKS = {
  // a bar and a dot with a clear gap between them, taller than the x so it reads at 16 pt
  warn: (g, x, y) => [
    line(g.seg(x, y - 4 + g.h, x, y + 0.6 - g.h)),
    solid(circ(x, y + 2.9, 1.3 * g.s / 2))
  ],
  error: (g, x, y) => { const k = MARK * 0.78 - g.h * 0.7; return [line(`${g.seg(x - k, y - k, x + k, y + k)}${g.seg(x + k, y - k, x - k, y + k)}`)]; }
};

function marked(mark, label) {
  return {
    ...duet, label, name: `duet-${mark}`, base: 'duet', kind: 'badge', weights: null,
    allow: { ...duet.allow, keyline: '角标在右下角的缺口里，伸出圆环的外框是有意的' },
    draw(g, ctx) {
      const x = BX, y = BY;
      const parts = MARKS[mark](g, x, y);
      return [...duet.draw(g, ctx), ...parts.map((b, i) => ({ ...b, n: i ? 'badge-dot' : 'badge', role: P, part: 'badge', ...(i ? {} : { cutD: circ(x, y, MARK + 0.25 + gapOf(g.s)), at: [x, y] }) }))];
    },
    fx: { t: 'keys', on: ['badge', 'badge-dot'], s: [1.2], o: [BX, BY], ms: 260 }
  };
}

// Tailscale down: the pair stays linked, a slash lies over the right (Tailscale) ring only.
const slashed = (label, k = 4.6) => ({
  ...duet, label, name: 'duet-tsdown', base: 'duet', weights: null,
  allow: { ...duet.allow, keyline: '斜线只盖住右环' },
  plan: '主体：两个扣在一起的圆环；右环（Tailscale）上压一条斜线，表示它关着',
  draw(g, ctx) {
    const d = g.seg(RX + k, CY - k, RX - k, CY + k);
    return [...duet.draw(g, ctx), line(d, { n: 'slash', role: P, part: 'slash', cut: { d, w: g.s + 2 * gapOf(g.s) } })];
  },
  fx: { t: 'draw', on: ['slash'] }
});
// Two whole rings side by side, not linked: nothing is connected yet.
const apart = (label) => ({
  ...duet, label, name: 'duet-unknown', base: 'duet', weights: null,
  allow: { ...duet.allow },
  plan: '主体：两个分开、没有扣上的圆环：还没连上',
  draw(g) {
    const r = 4.5;
    return [shell(g.ring(2 + r, CY, r), { n: 'left', role: P, part: 'left' }), shell(g.ring(22 - r, CY, r), { n: 'right', role: P, part: 'right' })];
  },
  fx: [{ t: 'keys', on: ['left'], x: [1, 0] }, { t: 'keys', on: ['right'], x: [-1, 0] }]
});
// Extra notch marks (same notch as warn/error).
// horizontal marks sit on a whole unit (17, half a unit above the notch centre) so their edges are crisp
MARKS.minus = (g, x, y) => { const k = MARK * 0.95 - g.h * 0.5; y = Math.floor(y); return [line(g.seg(x - k, y, x + k, y))]; };
MARKS.mode = (g, x, y) => {
  y = Math.floor(y);
  const k = MARK * 0.95 - g.h * 0.5, a = MARK * 0.7;
  return [line(`${g.seg(x - k, y, x + k, y)}M${n(x + k - a)} ${n(y - a)}L${n(x + k)} ${n(y)}L${n(x + k - a)} ${n(y + a)}`)];
};

export default {
  duet,
  'duet-warn': marked('warn', '双核（警告）'),
  'duet-error': marked('error', '双核（错误）'),
  'duet-unknown': apart('双核（连接中）'),
  'duet-tsdown': slashed('双核（Tailscale 断开）'),

  'duet-mode': marked('mode', '双核（非规则模式）'),
  'duet-nohelper': marked('minus', '双核（后台助手未连接）')
};
