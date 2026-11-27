package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/healthfix"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// noHealth answers health.run with an empty report (the harness has no
// checks); every other call goes to the engine over its socket.
type noHealth struct{ c *rpc.Client }

func (n noHealth) Call(ctx context.Context, method string, params, result any) error {
	if method == rpc.MethodHealthRun {
		return nil
	}
	return n.c.Call(ctx, method, params, result)
}

// The config fixes against the real engine: after fix + undo the stored
// overrides, the settings and the config on disk are the pre-fix ones,
// and the store composes to what is on disk (compose.preview: unchanged),
// so the next apply keeps the undo. Overrides of the fixes' ids exist
// before (B4: the tun-stack fix over a hand-written override).
func TestHealthFixUndoRealEngine(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, l := range []rpc.LayerPutParams{
		{ID: health.TUNStackLayerID, Name: "My TUN", Content: "tun:\n  stack: mixed\n"},
		{ID: healthfix.LoopbackLayerID, Name: "Controller", Content: "external-controller: 0.0.0.0:9090\n"},
	} {
		if _, err := h.e.PutLayer(l); err != nil {
			t.Fatal(err)
		}
	}
	h.apply("seed")

	d, _ := os.MkdirTemp("", "twinnelf")
	t.Cleanup(func() { os.RemoveAll(d) })
	sock := filepath.Join(d, "h.sock")
	tr := rpc.NewUnixTransport()
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(tr, allowAll{}, h.e.Log)
	h.e.Register(srv)
	go srv.Serve(l)
	t.Cleanup(func() { l.Close(); srv.Close() })
	c, err := rpc.Dial(ctx, nil, sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &healthfix.Fixer{Helper: noHealth{c}, Lang: "en", Platform: health.Darwin}

	state := func() (string, []byte) {
		ls, err := h.e.Layers()
		if err != nil {
			t.Fatal(err)
		}
		st, _ := h.e.Settings()
		var b bytes.Buffer
		for _, l := range ls {
			lc, _ := h.e.Layer(ctx, l.ID)
			b.WriteString(l.ID + "|" + l.Name + "|" + l.Stage + "|" + l.Format + "|" + lc.Content)
			if l.Enabled {
				b.WriteString("|on")
			}
			b.WriteString("\n")
		}
		b.WriteString(strings.Join(st.LeakGuardGroups, ",") + fmt.Sprint(st.TailscaleCoexist))
		return b.String(), h.disk()
	}
	for _, tc := range []struct{ reason, param string }{
		{health.ReasonDataplaneTCP, ""},
		{health.ReasonCtlExposed, "9090"},
		{health.ReasonLeakMayGo, "PROXY"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			preStore, preDisk := state()
			cr := health.CheckReport{ID: "the-check", Result: health.Result{Status: health.StatusFail, Reasons: []string{tc.reason},
				Fix: &health.FixHint{Param: tc.param}}}
			p, err := f.Plan(ctx, cr, tc.reason)
			if err != nil || p.Blocked != "" || !p.Undo {
				t.Fatalf("plan %+v %v", p, err)
			}
			out, err := f.Apply(ctx, p)
			if err != nil || out.Undo == nil {
				t.Fatalf("apply %+v %v", out, err)
			}
			if s, disk := state(); s == preStore || bytes.Equal(disk, preDisk) {
				t.Fatalf("%s changed nothing", p.Action)
			}
			if _, err := f.Undo(ctx, out.Undo); err != nil {
				t.Fatal(err)
			}
			s, disk := state()
			if s != preStore {
				t.Errorf("store after undo:\n%s\nwant\n%s", s, preStore)
			}
			if !bytes.Equal(disk, preDisk) {
				t.Errorf("running config after undo differs from before the fix")
			}
			if pv, err := h.e.Preview(ctx); err != nil || pv.Changed {
				t.Errorf("the store does not compose to the running config after undo (changed %v, %v)", pv.Changed, err)
			}
		})
	}
}
