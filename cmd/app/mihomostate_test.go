package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/foreign"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// The window reads Twinnel's mihomo through status.MihomoView only: a raw
// Mihomo.Reachable / Unauthorized says the controller answers, not that
// it answers for Twinnel (Windows QA round 2, E: the foreign process was
// shown as Twinnel's mihomo). Shots may set the fields; they are fixtures.
func TestNoRawMihomoReachable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "shots") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			// Names bound to a snapshot's Mihomo in this function (m := s.Mihomo).
			alias := map[string]bool{}
			ast.Inspect(fn, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != len(as.Rhs) {
					return true
				}
				for i, r := range as.Rhs {
					if sel, ok := r.(*ast.SelectorExpr); ok && sel.Sel.Name == "Mihomo" {
						if id, ok := as.Lhs[i].(*ast.Ident); ok {
							alias[id.Name] = true
						}
					}
				}
				return true
			})
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Reachable" && sel.Sel.Name != "Unauthorized") {
					return true
				}
				raw := false
				switch x := sel.X.(type) {
				case *ast.SelectorExpr:
					raw = x.Sel.Name == "Mihomo"
				case *ast.Ident:
					raw = alias[x.Name]
				}
				if raw {
					t.Errorf("%s: %s reads Mihomo.%s; use s.MihomoView() / s.MihomoLive() (another program's mihomo may answer)",
						fset.Position(sel.Pos()), fn.Name.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// foreignSnap is a snapshot whose controller answers for another program's
// mihomo, with values that must not show as Twinnel's.
func foreignSnap() status.Snapshot {
	s := status.Snapshot{Time: time.Now(), Loaded: true, WritesAllowed: true}
	s.Mihomo = status.Mihomo{Reachable: true, Version: "v9.9.9-foreign",
		Config:  mihomo.Config{MixedPort: 17890, Mode: mihomo.ModeRule},
		Traffic: status.TrafficSample{Traffic: mihomo.Traffic{Up: 1000, Down: 2000}}}
	s.Helper = status.Helper{State: status.HelperConnected, Status: &rpc.Status{HelperVersion: "0.1.0",
		Mihomo: rpc.MihomoStatus{Owner: &foreign.Owner{Addr: "127.0.0.1:19090", Verdict: foreign.Foreign,
			Client: foreign.ClientClashParty, PID: 4242, Exe: "/Applications/Clash Party.app/Contents/Resources/sidecar/mihomo"}}}}
	status.Evaluate(&s)
	return s
}

func TestMihomoMarkForeign(t *testing.T) {
	i18n.Set("zh-Hans")
	defer i18n.Set("zh-Hans")
	th := &ui.Theme{}
	s := foreignSnap()
	if s.MihomoLive() || s.Can().Mihomo {
		t.Fatal("a foreign controller is live for Twinnel")
	}
	m, _, text := mihomoMark(th, s.MihomoView())
	if m != markForeign {
		t.Fatalf("mark %v, want markForeign", m)
	}
	if text != "Clash Party 占用控制端口" {
		t.Fatalf("text %q", text)
	}
	if strings.Contains(text, i18n.T.Running) {
		t.Fatal("the foreign state reads as running")
	}
	if got := mihomoLockTip(s); got == "" {
		t.Fatal("no reason for the disabled mihomo controls")
	}
	if l := trayLookFor(s, status.Config{}); l.shape != status.TrayError || l.up != "" || l.down != "" {
		t.Fatalf("tray %+v: want the error shape and no rates", l)
	}
	if v := exitTileView(s, false, nil, time.Time{}, false, false, ""); v.node != text {
		t.Fatalf("exit tile node %q, want %q", v.node, text)
	}
	if len(selectorGroups(s)) != 0 {
		t.Fatal("the foreign groups are offered")
	}
}

func TestMihomoMarkShapesDiffer(t *testing.T) {
	th := &ui.Theme{}
	seen := map[stateMark]status.MihomoPhase{}
	for _, v := range []status.MihomoView{{Phase: status.MihomoRunning}, {Phase: status.MihomoDown},
		{Phase: status.MihomoForeign, Owner: &foreign.Owner{Verdict: foreign.Foreign}}} {
		m, _, _ := mihomoMark(th, v)
		if p, dup := seen[m]; dup {
			t.Fatalf("%v and %v share a shape", p, v.Phase)
		}
		seen[m] = v.Phase
	}
}

func TestControllerOwnerCheckFound(t *testing.T) {
	s := foreignSnap()
	if _, ok := controllerOwnerCheck(s); ok {
		t.Fatal("found a row without a report")
	}
	s.Health.Report = &health.Report{Checks: []health.CheckReport{{ID: "controller-owner",
		Result: health.Result{Status: health.StatusFail, Reasons: []string{health.ReasonOwnerOtherClient, health.ReasonOwnerPortShared}}}}}
	cr, ok := controllerOwnerCheck(s)
	if !ok || cr.ID != "controller-owner" {
		t.Fatal("the controller-owner row is not found")
	}
}

// While another program's mihomo holds the controller, the 总览 banner
// leads with the card's two ways out (Windows QA round 3, E), and no
// control offers to start or restart Twinnel's mihomo: it cannot help.
func TestForeignBannerOffersOwnerFixes(t *testing.T) {
	i18n.Set("zh-Hans")
	defer i18n.Set("zh-Hans")
	a := &app{healthOpen: map[string]bool{}}
	s := foreignSnap()
	// The watchdog held the DNS back too (the foreign mihomo does not
	// answer Twinnel's DNS): its 重启 mihomo must not lead either.
	s.Helper.Status.DNSWatch = &rpc.DNSWatch{Phase: rpc.DNSWatchRestored}
	s.Helper.Status.ActiveSubscription = "sub" // taken over: the fix runs, no step before it
	s.Health.Report = &health.Report{Platform: "windows", Checks: []health.CheckReport{{ID: "controller-owner",
		Result: health.Result{Status: health.StatusFail, Reasons: []string{health.ReasonOwnerOtherClient, health.ReasonOwnerPortShared}}}}}
	status.Evaluate(&s)
	if a.canRestartMihomo(s) || mihomoRestartHelps(s) {
		t.Fatal("restarting mihomo is offered while another mihomo holds the port")
	}
	fixes := a.foreignFixes(s)
	var fixKeys []string
	for _, f := range fixes {
		fixKeys = append(fixKeys, f.key)
	}
	if strings.Join(fixKeys, ",") != "foreign-move,foreign-quit" {
		t.Fatalf("foreign fixes %v", fixKeys)
	}
	acts := a.bannerActs(s)
	if len(acts) < 2 || acts[0].key != "foreign-move" || acts[0].label != i18n.T.ForeignMovePort || acts[1].key != "foreign-quit" {
		t.Fatalf("banner acts %+v: want the owner fixes first", acts)
	}
	for _, act := range acts {
		if act.label == i18n.T.RestartMihomo || act.label == i18n.T.StartMihomo {
			t.Fatalf("banner offers %q in the foreign state", act.label)
		}
	}
	// Twinnel's own mihomo: no owner fixes.
	if f := a.foreignFixes(dnsSnap(rpc.DNSWatchWatching, true)); f != nil {
		t.Fatalf("owner fixes without a foreign owner: %+v", f)
	}
}
