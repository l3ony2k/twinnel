package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/core/rpc"
)

type fakeAutostarts []autostart.Entry

func (f fakeAutostarts) List(context.Context) ([]autostart.Entry, error) { return f, nil }

func TestAutostartChange(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	st, _ := h.e.Store.Settings()
	h.e.Autostarts = fakeAutostarts{
		{Kind: autostart.ScheduledTask, Name: `\mihomo-party`, Program: `C:\Program Files\Clash Party\Clash Party.exe`, Enabled: true, Disable: autostart.ByHelper},
		{Kind: autostart.LoginItem, Name: "ClashX Meta", Enabled: true, Disable: autostart.Guided, Open: autostart.LoginItemsSettings},
		{Kind: autostart.LaunchDaemon, Name: st.Mihomo.ServiceLabel, Program: "/usr/local/bin/mihomo", Enabled: true, Disable: autostart.ByHelper},
	}
	l := h.e.AutostartList(ctx)
	if !l.Supported || len(l.Entries) != 2 {
		t.Fatalf("list (own service must not be listed) %+v", l)
	}
	id := `scheduled-task:\mihomo-party`

	// No helper rights: only the plan.
	r, err := h.e.AutostartChange(ctx, rpc.AutostartChangeParams{IDs: []string{id}, Op: "disable"})
	if err != nil || !r.DryRun || len(r.Steps) != 1 || !strings.Contains(r.Steps[0], `/Change /TN \mihomo-party /Disable`) {
		t.Fatalf("%+v %v", r, err)
	}
	exec := &recExec{}
	h.e.ServiceRunner = &ServiceRunner{Exec: exec, Files: &recFiles{}}
	if r, err = h.e.AutostartChange(ctx, rpc.AutostartChangeParams{IDs: []string{id}, Op: "disable", Preview: true}); err != nil || !r.DryRun || len(exec.runs) != 0 {
		t.Fatalf("preview ran: %+v %v", r, err)
	}
	r, err = h.e.AutostartChange(ctx, rpc.AutostartChangeParams{IDs: []string{id}, Op: "disable"})
	if err != nil || r.DryRun || len(exec.runs) != 1 || r.Undo == nil || r.Undo.Op != "enable" {
		t.Fatalf("%+v %v %q", r, err, exec.runs)
	}
	if _, err := h.e.AutostartChange(ctx, *r.Undo); err != nil || exec.runs[1][len(exec.runs[1])-1] != "/Enable" {
		t.Fatalf("undo: %v %q", err, exec.runs)
	}
	// A login item is not the helper's to change; an unknown id is refused.
	for _, bad := range []string{"login-item:ClashX Meta", "scheduled-task:\\nope"} {
		if _, err := h.e.AutostartChange(ctx, rpc.AutostartChangeParams{IDs: []string{bad}, Op: "disable"}); !isCode(err, rpc.CodeInvalidParams) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(exec.runs) != 2 {
		t.Fatalf("ran %q", exec.runs)
	}
}
