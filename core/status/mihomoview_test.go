package status

import (
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func withOwner(s Snapshot, o *foreign.Owner) Snapshot {
	s.Helper = Helper{State: HelperConnected, Status: &rpc.Status{Mihomo: rpc.MihomoStatus{Owner: o}}}
	return s
}

func TestMihomoViewPhases(t *testing.T) {
	i18n.Set("zh-Hans")
	answering := Snapshot{Loaded: true, Mihomo: Mihomo{Reachable: true, Version: "v1.19.0",
		Config: mihomo.Config{MixedPort: 17890, Mode: mihomo.ModeGlobal}}}
	cases := []struct {
		name string
		s    Snapshot
		want MihomoPhase
	}{
		{"running", answering, MihomoRunning},
		{"managed owner", withOwner(answering, &foreign.Owner{Verdict: foreign.Managed}), MihomoRunning},
		{"unknown owner", withOwner(answering, &foreign.Owner{Verdict: foreign.Unknown}), MihomoRunning},
		{"foreign wins over answering", withOwner(answering, &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign}), MihomoForeign},
		{"down", Snapshot{Mihomo: Mihomo{Err: "connection refused"}}, MihomoDown},
		{"not loaded", Snapshot{}, MihomoDown},
		{"auth", Snapshot{Mihomo: Mihomo{Unauthorized: true, Err: "401"}}, MihomoAuth},
	}
	for _, c := range cases {
		v := c.s.MihomoView()
		if v.Phase != c.want {
			t.Errorf("%s: phase %v, want %v", c.name, v.Phase, c.want)
		}
		if live := c.s.MihomoLive(); live != (c.want == MihomoRunning) {
			t.Errorf("%s: live %v", c.name, live)
		}
		if c.s.Can().Mihomo && c.want != MihomoRunning {
			t.Errorf("%s: mihomo controls on", c.name)
		}
	}
}

func TestMihomoViewForeignTexts(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		i18n.Set(lang)
		anon := withOwner(Snapshot{Mihomo: Mihomo{Reachable: true}}, &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign, PID: 4242, Exe: `C:\synthetic\mihomo.exe`}).MihomoView()
		app := withOwner(Snapshot{Mihomo: Mihomo{Reachable: true}}, &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign, Client: foreign.ClientVerge}).MihomoView()
		if anon.Text() == "" || anon.Text() == i18n.Cur().Running {
			t.Errorf("%s: foreign text %q", lang, anon.Text())
		}
		if !strings.Contains(app.Text(), "Clash Verge Rev") {
			t.Errorf("%s: client not named: %q", lang, app.Text())
		}
		if got, want := anon.Detail(), `pid 4242 · C:\synthetic\mihomo.exe`; got != want {
			t.Errorf("%s: detail %q, want %q", lang, got, want)
		}
		if app.Detail() != "" {
			t.Errorf("%s: detail of an owner without pid/exe: %q", lang, app.Detail())
		}
	}
	i18n.Set("zh-Hans")
}

// The menu bar icon of a foreign controller is the error shape, before
// the Tailscale and mode shapes that read the (foreign) mihomo.
func TestTrayIconForeign(t *testing.T) {
	s := withOwner(Snapshot{Loaded: true, Mihomo: Mihomo{Reachable: true, Config: mihomo.Config{Mode: mihomo.ModeGlobal}},
		Tailscale: Tailscale{Reachable: true}}, &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign})
	Evaluate(&s)
	if got := TrayIconFor(s); got != TrayError {
		t.Fatalf("tray %v, want %v", got, TrayError)
	}
}

// Facts are the rows every surface lists under the state (the app's card,
// the CLI's status): the address, the process and what started it, as
// much as is known, never the unknown name again.
func TestMihomoViewFacts(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		i18n.Set(lang)
		T := i18n.Cur()
		o := &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign, PID: 4242, Exe: "/synthetic/mihomo", Parent: "/synthetic/launcher"}
		v := withOwner(Snapshot{Mihomo: Mihomo{Reachable: true}}, o).MihomoView()
		want := []ViewFact{
			{FactController, T.Controller, "127.0.0.1:19090"},
			{FactProcess, T.ForeignProcess, "pid 4242 · /synthetic/mihomo"},
			{FactParent, T.ForeignParent, "/synthetic/launcher"},
		}
		if got := v.Facts(); !slices.Equal(got, want) {
			t.Errorf("%s: facts %+v", lang, got)
		}
		for _, f := range v.Facts() {
			if strings.Contains(f.Value, "another") || strings.Contains(f.Value, "另一个") {
				t.Errorf("%s: %s repeats the unknown name: %q", lang, f.Key, f.Value)
			}
		}
		bare := withOwner(Snapshot{}, &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign}).MihomoView()
		if got := bare.Facts(); len(got) != 1 || got[0].Key != FactController {
			t.Errorf("%s: facts of an owner known only by address: %+v", lang, got)
		}
		if got := (Snapshot{Mihomo: Mihomo{Err: "refused"}}).MihomoView().Facts(); got != nil {
			t.Errorf("%s: facts while down: %+v", lang, got)
		}
	}
	i18n.Set("zh-Hans")
}

// The helper's own read (twinnel helper status) is derived the same way:
// another program's mihomo answering is not Twinnel's running.
func TestHelperMihomoView(t *testing.T) {
	cases := []struct {
		m    rpc.MihomoStatus
		want MihomoPhase
	}{
		{rpc.MihomoStatus{Reachable: true}, MihomoRunning},
		{rpc.MihomoStatus{Reachable: true, Owner: &foreign.Owner{Verdict: foreign.Managed}}, MihomoRunning},
		{rpc.MihomoStatus{Reachable: true, Owner: &foreign.Owner{Addr: "a:1", Verdict: foreign.Foreign}}, MihomoForeign},
		{rpc.MihomoStatus{Error: "refused"}, MihomoDown},
	}
	for _, c := range cases {
		if got := HelperMihomoView(c.m).Phase; got != c.want {
			t.Errorf("%+v: %v, want %v", c.m, got, c.want)
		}
	}
}
