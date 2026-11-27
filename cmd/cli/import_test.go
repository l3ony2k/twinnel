package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
)

// importHelper serves a real engine on a fresh state dir and a temp socket.
func importHelper(t *testing.T) (string, *engine.Engine) {
	t.Helper()
	d, err := os.MkdirTemp("", "tw-imp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	s, err := store.Open(filepath.Join(d, "state"))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(s, &store.MemSecrets{})
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := rpc.NewUnixTransport()
	srv := rpc.NewServer(tr, rpc.AllowAll{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Register(srv)
	sock := filepath.Join(d, "h.sock")
	l, err := tr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { l.Close(); srv.Close() })
	return sock, e
}

func withIO(t *testing.T, in string) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	oldIn, oldOut := stdin, stdout
	stdin, stdout = strings.NewReader(in), &out
	t.Cleanup(func() { stdin, stdout = oldIn, oldOut })
	return &out
}

func TestImportCommand(t *testing.T) {
	sock, e := importHelper(t)
	dir := t.TempDir()
	const token = "synthetic-cli-token"
	list := filepath.Join(dir, "subs.txt")
	os.WriteFile(list, []byte("Home https://sub.example.invalid/s?token="+token+"\n"), 0o600)
	layer := filepath.Join(dir, "Verge Merge.yaml")
	os.WriteFile(layer, []byte("prepend-rules:\n  - DOMAIN,a.example.invalid,DIRECT\n"), 0o600)

	// Dry run: a plan, nothing stored, no URL printed.
	out := withIO(t, "")
	if err := runImport([]string{list, "--dry-run", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "subscription") || !strings.Contains(s, "add") || strings.Contains(s, token) ||
		!strings.Contains(s, "becomes active: home") {
		t.Fatalf("dry run output:\n%s", s)
	}
	if subs, _ := e.Subscriptions(); len(subs) != 0 {
		t.Fatal("dry run stored")
	}

	// Declined.
	withIO(t, "n\n")
	if err := runImport([]string{list, "--socket", sock}); err == nil {
		t.Fatal("declined import went ahead")
	}
	// Confirmed.
	out = withIO(t, "y\n")
	if err := runImport([]string{list, "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if subs, _ := e.Subscriptions(); len(subs) != 1 || !subs[0].Active || subs[0].ID != "home" {
		t.Fatalf("subs: %+v", subs)
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("printed the URL")
	}
	if err := runImport([]string{layer, "--yes", "--socket", sock}); err != nil {
		t.Fatal(err)
	}

	// A changed layer here: a conflict, answered on the "terminal".
	os.WriteFile(layer, []byte("prepend-rules:\n  - DOMAIN,b.example.invalid,DIRECT\n"), 0o600)
	out = withIO(t, "x\nn\ny\n")
	if err := runImport([]string{layer, "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	ls, _ := e.Layers()
	if len(ls) < 2 || ls[0].ID != "verge-merge" || ls[1].ID != "verge-merge-2" {
		t.Fatalf("layers: %+v\n%s", ls, out)
	}
	// --json needs --yes; conflicts with --json need --on-conflict.
	withIO(t, "")
	if err := runImport([]string{layer, "--json", "--socket", sock}); err == nil {
		t.Fatal("--json without --yes")
	}
	if err := runImport([]string{layer, "--json", "--yes", "--socket", sock}); err == nil || !strings.Contains(err.Error(), "on-conflict") {
		t.Fatalf("unresolved conflict with --json: %v", err)
	}
	if err := runImport([]string{layer, "--json", "--yes", "--on-conflict", "skip", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if err := runImport([]string{layer, "--on-conflict", "merge", "--socket", sock}); err == nil {
		t.Fatal("bad --on-conflict accepted")
	}
}
