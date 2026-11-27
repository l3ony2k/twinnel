package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
)

// The 总览 mihomo and Tailscale cards line up row for row, so a reader
// finds the same thing in the same place in both: the rows they share
// first (状态, 进程, 运行时长), then the rows that pair like with like
// (mihomo 出口 ↔ 出口节点, DNS ↔ MagicDNS, 代理端口 ↔ 本机), then each
// card's own rows, side by side by position. Side by side, every slot is
// as tall as its taller side (measured on the previous frame), so a
// control on one side (the mode switch, the exit-node picker) never
// pushes the other side out of line; a slot one side lacks stays empty
// there. Stacked, each card lists its rows in the same order, without
// the empty slots.

// ovSlot is a row's place in the grid.
type ovSlot int

const (
	slotBody    ovSlot = -1 // the whole card (down, logged out): no grid
	slotState   ovSlot = iota
	slotProcess         // memory · CPU
	slotUptime          // 运行时长
	slotExit            // mihomo 出口 ↔ Tailscale 出口节点
	slotDNS             // DNS ↔ MagicDNS
	slotAddr            // 代理端口 ↔ 本机
	slotOwnBase ovSlot = 100
)

// slotOwn is a card's i-th own row.
func slotOwn(i int) ovSlot { return slotOwnBase + ovSlot(i) }

// ovRows draws a card's rows by slot.
type ovRows map[ovSlot]func()

// ovGrid lines up two cards' rows (the overview's mihomo and Tailscale).
type ovGrid struct {
	pair bool
	rows [2]ovRows
	h    map[ovSlot]float32    // each slot's height, the taller side's
	meas [2]map[ovSlot]float32 // this frame's measurements
}

// begin takes this frame's rows of both cards; pair is whether they
// stand side by side.
func (g *ovGrid) begin(pair bool, left, right ovRows) {
	g.pair, g.rows = pair, [2]ovRows{left, right}
	g.meas = [2]map[ovSlot]float32{{}, {}}
	if g.h == nil {
		g.h = map[ovSlot]float32{}
	}
}

// aligned: both cards show rows, side by side.
func (g *ovGrid) aligned() bool {
	return g.pair && g.rows[0][slotBody] == nil && g.rows[1][slotBody] == nil
}

// slots are the slots side draws, in order: with the other side's too
// when aligned.
func (g *ovGrid) slots(side int) []ovSlot {
	var out []ovSlot
	add := func(r ovRows) {
		for k := range r {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	add(g.rows[side])
	if g.aligned() {
		add(g.rows[1-side])
	}
	slices.Sort(out)
	return out
}

// body draws side's card body (0 mihomo, 1 Tailscale).
func (g *ovGrid) body(c *ui.Context, side int) {
	rows := g.rows[side]
	if b := rows[slotBody]; b != nil {
		b()
		return
	}
	align := g.aligned()
	for _, k := range g.slots(side) {
		draw := rows[k]
		if !align {
			draw()
			continue
		}
		ui.Column(c.Key(fmt.Sprint("slot", k))).Justify(ui.Center).MinHeight(g.h[k]).Children(func() {
			inner := ui.Column(c).Gap(gapRow).Children(func() {
				if draw != nil {
					draw()
				}
			})
			g.meas[side][k] = inner.Bounds().H // its own height, not the slot's
		})
	}
}

// end keeps the slots' heights for the next frame and lays out again
// when one changed.
func (g *ovGrid) end(c *ui.Context) {
	if !g.aligned() {
		return
	}
	changed := false
	for k := range g.meas[0] {
		m := max(g.meas[0][k], g.meas[1][k])
		if m <= 0 { // not laid out yet
			continue
		}
		if d := m - g.h[k]; d > 0.5 || d < -0.5 {
			g.h[k], changed = m, true
		}
	}
	if changed {
		c.Invalidate()
	}
}
