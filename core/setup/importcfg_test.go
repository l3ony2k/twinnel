package setup_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
	"github.com/l3ony2k/twinnel/core/store"
)

// Synthetic subscription; no real provider or node.
const subYAML = "proxies:\n  - {name: n1, type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: synthetic}\nproxy-groups:\n  - {name: PROXY, type: select, proxies: [n1]}\nrules:\n  - MATCH,PROXY\n"

func serve(t *testing.T) (*rpc.Client, *engine.Engine) {
	t.Helper()
	d, err := os.MkdirTemp("", "tw-setimp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	s, err := store.Open(filepath.Join(d, "state"))
	if err != nil {
		t.Fatal(err)
	}
	// Never the live mihomo's controller (a fresh store defaults to 9090).
	if _, err := s.UpdateSettings(func(st *store.Settings) error { st.Mihomo.Controller = "127.0.0.1:1"; return nil }); err != nil {
		t.Fatal(err)
	}
	e := engine.New(s, &store.MemSecrets{})
	e.NewController = func(addr, secret string) engine.Controller {
		return &engine.HTTPController{Addr: "127.0.0.1:1", Secret: secret}
	}
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := rpc.NewUnixTransport()
	srv := rpc.NewServer(tr, rpc.AllowAll{}, e.Log)
	e.Register(srv)
	l, err := tr.Listen(filepath.Join(d, "h.sock"))
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { l.Close(); srv.Close() })
	c, err := rpc.Dial(context.Background(), nil, filepath.Join(d, "h.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, e
}

func zipOf(t *testing.T, b *bundle.Bundle) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := bundle.Write(&buf, b, time.Now(), "test"); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImportConfigHook(t *testing.T) {
	// A zip with the download: active at once, no fetch.
	c, e := serve(t)
	z := zipOf(t, &bundle.Bundle{
		Layers:          []bundle.Layer{{ID: "mine", Name: "Mine", Stage: store.StageUser, Format: store.FormatMerge, Enabled: true, Content: "mode: rule\n"}},
		Subscriptions:   []bundle.Subscription{{ID: "home", Name: "Home", Active: true, IntervalMinutes: 60, Content: subYAML}},
		LeakGuardGroups: []string{"PROXY"},
	})
	r, err := setup.ImportConfig(context.Background(), c, z)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Subscription || len(r.LeakGuardGroups) != 1 || r.LeakGuardGroups[0] != "PROXY" {
		t.Fatalf("%+v", r)
	}
	if ls, _ := e.Layers(); ls[0].ID != "mine" {
		t.Fatalf("layers %+v", ls)
	}

	// A zip with a URL only: the hook downloads it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, subYAML) }))
	defer srv.Close()
	c2, e2 := serve(t)
	z = zipOf(t, &bundle.Bundle{Subscriptions: []bundle.Subscription{{ID: "remote", Name: "Remote", Active: true, URL: srv.URL + "/s?token=synthetic"}}})
	r, err = setup.ImportConfig(context.Background(), c2, z)
	if err != nil {
		t.Fatal(err)
	}
	subs, _ := e2.Subscriptions()
	if !r.Subscription || len(subs) != 1 || subs[0].Size == 0 {
		t.Fatalf("%+v %+v", r, subs)
	}

	// No subscription in the zip.
	c3, _ := serve(t)
	r, err = setup.ImportConfig(context.Background(), c3, zipOf(t, &bundle.Bundle{Layers: []bundle.Layer{{ID: "x", Stage: store.StageUser, Format: store.FormatMerge, Content: "a: 1\n"}}}))
	if err != nil || r.Subscription {
		t.Fatalf("%v %+v", err, r)
	}
}
