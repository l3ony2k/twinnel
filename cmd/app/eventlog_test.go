package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// A check event names the check in the UI language, whatever language
// the helper wrote its title in.
func TestCheckEventTitleInUILanguage(t *testing.T) {
	defer i18n.Set("zh")
	id := health.DefaultRegistry().IDs()[0]
	ch := health.Change{To: health.StatusOK}
	ch.Check.ID, ch.Check.Title = id, health.TitleOf("zh-Hans", id)
	data, _ := json.Marshal(ch)
	e := rpc.Event{Type: rpc.EventCheckChanged, Data: data}
	i18n.Set("en")
	r, ok := describeEvent(e, nil)
	if !ok {
		t.Fatal("check event not described")
	}
	en := health.TitleOf("en", id)
	if en == ch.Check.Title || !strings.Contains(r.text, en) {
		t.Errorf("text = %q, want the English title %q", r.text, en)
	}
	if got := checkTitle("no-such-check", "原标题"); got != "原标题" {
		t.Errorf("unknown check = %q, want the event's own title", got)
	}
}

// After a helper upgrade the log keeps the old run's events (ending with
// 正在停止) and the new run's buffer starts again at 1 with 已启动: both
// are kept, in time order, so 最近活动 leads with the start.
func TestEventsAcrossHelperRestart(t *testing.T) {
	t0 := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	a := &app{}
	a.recordEvents([]rpc.Event{
		{Seq: 1, Time: t0, Type: rpc.EventHealthUpdated},
		{Seq: 2, Time: t0.Add(time.Minute), Type: rpc.EventHelperStopping},
	})
	// The new run's buffer, as backfillEvents records it.
	a.recordEvents([]rpc.Event{
		{Seq: 1, Time: t0.Add(2 * time.Minute), Type: rpc.EventHelperStarted},
		{Seq: 2, Time: t0.Add(3 * time.Minute), Type: rpc.EventHealthUpdated},
	})
	a.recordEvents([]rpc.Event{{Seq: 2, Time: t0.Add(3 * time.Minute), Type: rpc.EventHealthUpdated}}) // the stream repeats one
	var types []string
	for _, e := range a.events.evs {
		types = append(types, e.Type)
	}
	want := []string{rpc.EventHealthUpdated, rpc.EventHelperStopping, rpc.EventHelperStarted, rpc.EventHealthUpdated}
	if strings.Join(types, " ") != strings.Join(want, " ") {
		t.Fatalf("log %v, want %v", types, want)
	}
}

// An apply from `twinnel compose --apply` is worded, not its command line
// (new helpers send the code; older entries carry the text).
func TestCLIApplyWorded(t *testing.T) {
	defer i18n.Set("zh")
	for _, lang := range []string{"zh", "en"} {
		i18n.Set(lang)
		for _, res := range []rpc.ApplyResult{{Reason: "twinnel compose --apply"}, {Reason: "twinnel compose --apply", ReasonCode: store.ReasonCLI}} {
			res.Changed, res.Written = true, true
			b, _ := json.Marshal(res)
			r, _ := describeEvent(rpc.Event{Type: rpc.EventComposeApplied, Data: b}, nil)
			if strings.Contains(r.text, "compose") || !strings.Contains(r.text, i18n.T.EvViaCLI) {
				t.Errorf("%s: %q", lang, r.text)
			}
			if h := i18n.T.HistoryReasonText(res.ReasonCode, nil, res.Reason); strings.Contains(h, "compose") {
				t.Errorf("%s history reason %q", lang, h)
			}
		}
	}
}
