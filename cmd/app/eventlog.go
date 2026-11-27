package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
)

// What duet did (U29, H7): the helper's events (events.wait), kept for
// 日志 → duet. The connection skips what happened before it; the tab
// fetches the helper's buffer once (backfill) so it starts with the recent
// past, then follows OnEvents.

const eventLogCap = 500

type eventLog struct {
	evs        []rpc.Event // oldest first
	backfilled bool
	filling    bool
}

// recordEvents keeps a batch from OnEvents. Main thread.
func (a *app) recordEvents(evs []rpc.Event) {
	l := &a.events
	for _, e := range evs {
		if e.Type == rpc.EventEventsTruncated {
			continue
		}
		if hasEvent(l.evs, e) {
			continue // already have it (a backfill raced the stream)
		}
		l.evs = append(l.evs, e)
	}
	if over := len(l.evs) - eventLogCap; over > 0 {
		l.evs = append(l.evs[:0:0], l.evs[over:]...)
	}
}

// backfillEvents asks the helper for its buffered events once per
// connection (a read: events.wait with after 0 and no wait).
func (a *app) backfillEvents() {
	l := &a.events
	if l.backfilled || l.filling || !a.hd.connected || a.shotMode && a.hc == nil {
		return
	}
	l.filling = true
	hc := a.hc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var r rpc.EventsWaitResult
		err := hc.Call(ctx, rpc.MethodEventsWait, rpc.EventsWaitParams{After: 0, TimeoutMS: 1}, &r)
		mygo.RunOnMain(func() {
			l.filling = false
			if err != nil {
				return
			}
			l.backfilled = true
			seen := map[uint64]bool{}
			var merged []rpc.Event
			for _, e := range r.Events {
				if e.Type != rpc.EventEventsTruncated {
					merged = append(merged, e)
					seen[e.Seq] = true
				}
			}
			for _, e := range l.evs {
				if !seen[e.Seq] {
					merged = append(merged, e)
				}
			}
			l.evs = nil
			a.recordEvents(merged)
			a.invalidate()
		})
	}()
}

// eventRow is one event in words: its kind (the chip), what happened,
// whether it is a failure, and where a click goes (page, history ID).
type eventRow struct {
	kind, text string
	failed     bool
	page, hist string
}

func describeEvent(e rpc.Event, subName func(id string) string) (eventRow, bool) {
	T := i18n.T
	kind, ok := T.EventKinds[e.Type]
	if !ok {
		return eventRow{}, false
	}
	r := eventRow{kind: kind}
	switch e.Type {
	case rpc.EventComposeApplied, rpc.EventComposeFailed:
		var res rpc.ApplyResult
		json.Unmarshal(e.Data, &res)
		reason := T.HistoryReasonText(res.ReasonCode, res.ReasonArgs, res.Reason)
		r.page, r.hist = pageHistory, res.HistoryID
		switch {
		case e.Type == rpc.EventComposeFailed:
			r.failed, r.page = true, pageConfig
			text, _ := T.MsgText(errorMsg(res.Error, res.ErrorMsg), false)
			r.text = i18n.F(T.EvApplyFailed, joinNonEmpty(reason, text))
		case !res.Changed && !res.Written:
			r.text = i18n.F(T.EvApplyUnchanged, reason)
		default:
			r.text = i18n.F(T.EvApplied, reason)
		}
		r.text = strings.TrimSuffix(r.text, " · ")
	case rpc.EventStateChanged:
		r.text, r.page = T.EvState, pageConfig
	case rpc.EventSubUpdated, rpc.EventSubFailed:
		var d struct {
			ID      string `json:"id"`
			Changed bool   `json:"changed"`
		}
		json.Unmarshal(e.Data, &d)
		name := subName(d.ID)
		r.page = pageConfig
		switch {
		case e.Type == rpc.EventSubFailed:
			r.failed, r.text = true, i18n.F(T.EvSubFailed, name)
		case d.Changed:
			r.text = i18n.F(T.EvSubUpdated, name)
		default:
			r.text = i18n.F(T.EvSubSame, name)
		}
	case rpc.EventTailnetChanged:
		r.text, r.page = T.EvTailnet, pageTailscale
	case rpc.EventKernelChanged:
		r.text, r.page = T.EvKernel, pageKernel
	case rpc.EventAccessoryChanged:
		r.text, r.page = T.EvAccessory, pageKernel
	case rpc.EventServiceChanged:
		r.text, r.page = T.EvService, pageKernel
	case rpc.EventTakeoverChanged:
		r.text, r.page = T.EvTakeover, pageTakeover
	case rpc.EventCheckChanged:
		var ch health.Change
		json.Unmarshal(e.Data, &ch)
		st := healthStatusName(ch.To)
		r.text, r.page = i18n.F(T.EvCheck, ch.Check.Title, st), pageHealth
		r.failed = ch.To == health.StatusFail
	case rpc.EventHealthUpdated:
		var hs rpc.HealthSummary
		json.Unmarshal(e.Data, &hs)
		r.text, r.page = i18n.F(T.EvHealth, hs.Counts[string(health.StatusFail)], hs.Counts[string(health.StatusWarn)]), pageHealth
	case rpc.EventHelperStarted:
		r.text = T.EvHelperStarted
	case rpc.EventHelperStopping:
		r.text = T.EvHelperStop
	}
	return r, true
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// hasEvent reports whether evs ends with e among its recent entries (a
// helper restart starts its numbers again, so the time counts too).
func hasEvent(evs []rpc.Event, e rpc.Event) bool {
	for i := len(evs) - 1; i >= 0 && i >= len(evs)-64; i-- {
		if evs[i].Seq == e.Seq && evs[i].Time.Equal(e.Time) {
			return true
		}
	}
	return false
}

func healthStatusName(s health.Status) string {
	T := i18n.T
	switch s {
	case health.StatusOK:
		return T.HealthOK
	case health.StatusWarn:
		return T.HealthWarn
	case health.StatusFail:
		return T.HealthFail
	}
	return T.HealthSkip
}
