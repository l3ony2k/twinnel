package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
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
