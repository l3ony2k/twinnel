package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Busy feedback (Vibe Design Tips 017, kubai R-11): work shows on the
// control that started it, only once it took longer than busyDelay, and
// then for at least busyMin, so a quick answer never flashes a mark.
//
// One timer, busySpan, does that timing everywhere: an operation's
// button (a.hd.span, pending.go), any control's own work (showBusy), the
// unreachable states' 重试 (a.retry) and the placeholders that stand in
// for loading content (loadingBox, states.go).
//
// What shows while busy: a control whose icon already means the action
// (refresh: 检查更新, 重试, 刷新) turns that icon (turningIcon), whole turns
// only; any other icon becomes a spinner (spinnerIcon).
const (
	busyDelay = 300 * time.Millisecond
	busyMin   = 500 * time.Millisecond
)

// busySpan is one run of work: end is zero while it runs.
type busySpan struct {
	start, end time.Time
}

// visible reports whether the run's busy mark shows at now: from
// busyDelay after the start; a run that ended after that keeps it until
// busyMin after it appeared, and one that ended before never shows it.
func (b busySpan) visible(now time.Time) bool {
	if b.start.IsZero() {
		return false
	}
	on := b.start.Add(busyDelay)
	if b.end.IsZero() {
		return !now.Before(on)
	}
	return !b.end.Before(on) && now.Before(on.Add(busyMin))
}

// next is when visible may change after now, zero when it will not.
func (b busySpan) next(now time.Time) time.Time {
	if b.start.IsZero() {
		return time.Time{}
	}
	on := b.start.Add(busyDelay)
	switch {
	case now.Before(on) && (b.end.IsZero() || !b.end.Before(on)):
		return on
	case !b.end.IsZero() && !b.end.Before(on) && now.Before(on.Add(busyMin)):
		return on.Add(busyMin)
	}
	return time.Time{}
}

// track starts the run when busy turns true and ends it when it turns
// false; a new run starts once the last one is over.
func (b *busySpan) track(busy bool, now time.Time) {
	switch {
	case busy && (b.start.IsZero() || !b.end.IsZero() && !b.visible(now)):
		*b = busySpan{start: now}
	case busy:
		b.end = time.Time{} // busy again while the last mark still shows: one run
	case !b.start.IsZero() && b.end.IsZero():
		b.end = now
	}
}

// showBusy says whether e should show its busy mark now for work that is
// running while busy is true; e keeps the timing (ui.Local), so each
// control has its own, and frames are scheduled for the changes.
func showBusy(c *ui.Context, e ui.Element, busy bool) bool {
	st := ui.Local(e, "twinnel.busy", func() busySpan { return busySpan{} })
	if st == nil {
		return false
	}
	now := c.Now()
	st.track(busy, now)
	if at := st.next(now); !at.IsZero() {
		c.After(at.Sub(now))
	}
	return st.visible(now)
}

// busyFace dresses b, a push or primary button that shows a busy mark
// (a spinner, or its turning icon) while spin, and colors mark, drawn in
// its label, in the ink of that face. Every button that shows a busy
// mark goes through it (TestBusyMarksReadOnTheirFace).
//
// A busy button keeps its working face (the accent of a primary), not
// dimmed even when it is disabled meanwhile: a disabled face's ink is
// too faint for a spinner (WinUI's TextFillColorDisabled is 2.5:1 on its
// light fill), and the work shows where it was started. Otherwise a
// disabled primary takes disabledPrimaryFace. On a primary the mark takes
// the label's color (AccentText); on a push face it keeps the platform's
// own (the accent ring on Windows, gray spokes on macOS), which reads
// there. (Windows QA round 5 N1: a busy, disabled 保存 painted its ring
// AccentText, black, on MyGo's grey disabled face.)
func busyFace(c *ui.Context, b ui.Element, primary, disabled, spin bool) ui.Element {
	b.Disabled(disabled)
	switch {
	case spin:
		b.DimDisabled(false)
	case primary && disabled:
		disabledPrimaryFace(c, b)
	}
	return b
}

// onBusyFace colors mark, the busy mark or icon in the label of a button
// dressed by busyFace, for that face: on a working primary, in its
// label's color. It returns mark.
func onBusyFace(c *ui.Context, mark ui.Element, primary, spin bool) ui.Element {
	// A primary's face is the accent only while it shows one: MyGo
	// draws a primary in a window that isn't active as a plain button
	// (AppKit), unless the theme keeps the accent there (Fluent's
	// InactiveAccent). On a plain face the mark keeps the text color.
	if primary && spin && (c.WindowActive() || c.Theme().InactiveAccent != ui.Transparent) {
		mark.TextColor(c.Theme().AccentText)
	}
	return mark
}

// spinnerIcon is a spinner the size of an inline icon, to stand in for
// one while its control works.
func spinnerIcon(c *ui.Context) ui.Element {
	if plat.fluent {
		return progressRing(c, iconInline)
	}
	return ui.Spinner(c).Size(iconInline, iconInline)
}

// spinner is the platform's indicator of work of unknown length: AppKit's
// spokes (ui.Spinner) on macOS and Linux, WinUI's ProgressRing on
// Windows. It is as large as ui.Spinner (1.25 × the font size). Use it
// for every spinner (TestSpinnersArePlatform).
func spinner(c *ui.Context) ui.Element {
	if plat.fluent {
		return progressRing(c, c.Theme().FontSize*1.25)
	}
	return ui.Spinner(c)
}

// iconProgressArc is the ProgressRing's arc: a third of a circle from
// twelve o'clock, on the icons' 24 grid.
var iconProgressArc = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" ><path d="M12 3a9 9 0 0 1 7.79 13.5" fill="none" stroke="#000000" stroke-width="2.5" stroke-linecap="round"/></svg>`))

// ringPeriod is one turn of the ProgressRing.
const ringPeriod = time.Second

// progressRing is WinUI's indeterminate ProgressRing at size: an arc in
// the accent turning clockwise (TextColor changes it, as the text on the
// accent of a primary button). It turns under Reduce Motion too, as a
// sign that work goes on (MyGo's Loop does the same). Assistive
// technology sees a progress indicator; name it with Label.
func progressRing(c *ui.Context, size float32) ui.Element {
	e := ui.Icon(c, iconProgressArc).FontSize(size).Size(size, size).Shrink(0).
		TextColor(c.Theme().Accent).Role(ui.RoleProgress).AccessHidden(false)
	frac := float32(c.Now().UnixMilli()%ringPeriod.Milliseconds()) / float32(ringPeriod.Milliseconds())
	c.AnimationFrame()
	return e.Rotate(360 * frac)
}

// turnPeriod is one whole turn of a turning icon.
const turnPeriod = 640 * time.Millisecond

// turnState is a turning icon's motion: when it started, and the whole
// turn it stops on once the work is over.
type turnState struct {
	start, stop time.Time
}

// turningIcon is icon (one that means the action, as refresh) at the
// inline size, turning while turning is true: one turn per turnPeriod,
// clockwise, and once turning goes false it finishes the turn it is in,
// so it always comes to rest upright. Under Reduce Motion it does not
// turn but dims while busy.
func turningIcon(c *ui.Context, icon *ui.SVG, turning bool) ui.Element {
	e := ui.Icon(c, icon).FontSize(iconInline)
	if c.Preferences().ReduceMotion {
		if turning {
			e.Opacity(0.45)
		}
		return e
	}
	st := ui.Local(e, "twinnel.turn", func() turnState { return turnState{} })
	if st == nil {
		return e
	}
	now := c.Now()
	switch {
	case turning && st.start.IsZero():
		st.start, st.stop = now, time.Time{}
	case turning:
		st.stop = time.Time{} // turning again before it came to rest
	case !st.start.IsZero() && st.stop.IsZero():
		n := now.Sub(st.start)/turnPeriod + 1
		st.stop = st.start.Add(n * turnPeriod)
	}
	if st.start.IsZero() {
		return e
	}
	if !st.stop.IsZero() && !now.Before(st.stop) {
		*st = turnState{}
		return e
	}
	frac := float32(now.Sub(st.start)%turnPeriod) / float32(turnPeriod)
	c.AnimationFrame()
	return e.Rotate(360 * frac)
}

// busyIcon is the busy form of a control's icon: icon turning when it is
// the refresh icon, else the icon, replaced by a spinner while busy.
func busyIcon(c *ui.Context, icon *ui.SVG, busy bool) ui.Element {
	if icon == iconRefresh {
		return turningIcon(c, icon, busy)
	}
	if busy {
		return spinnerIcon(c)
	}
	return ui.Icon(c, icon).FontSize(iconInline)
}
