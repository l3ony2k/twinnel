package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

// twinnel export → twinnel import on another (scratch) helper brings the
// subscriptions and overrides over; the URL is never printed.
func TestExportImportRoundTrip(t *testing.T) {
	sock, _ := importHelper(t)
	dir := t.TempDir()
	const token = "synthetic-export-token"
	list := filepath.Join(dir, "subs.txt")
	os.WriteFile(list, []byte("Home https://sub.example.invalid/s?token="+token+"\n"), 0o600)
	layer := filepath.Join(dir, "Mine.yaml")
	os.WriteFile(layer, []byte("prepend-rules:\n  - DOMAIN,a.example.invalid,DIRECT\n"), 0o600)
	withIO(t, "")
	for _, f := range []string{list, layer} {
		if err := runImport([]string{f, "--yes", "--socket", sock}); err != nil {
			t.Fatal(err)
		}
	}

	var errBuf bytes.Buffer
	oldErr := stderr
	stderr = &errBuf
	t.Cleanup(func() { stderr = oldErr })

	zip := filepath.Join(dir, "out.zip")
	out := withIO(t, "")
	if err := runExport([]string{"--out", zip, "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if s := out.String() + errBuf.String(); strings.Contains(s, token) || !strings.Contains(s, "1 override(s) and 1 subscription(s)") ||
		!strings.Contains(errBuf.String(), "keep it private") || !strings.Contains(s, "twinnel import "+zip) {
		t.Fatalf("output:\n%s", s)
	}
	if fi, err := os.Stat(zip); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("zip: %v %v", fi, err)
	}

	// An existing file needs --force; --json reports without the zip.
	if err := runExport([]string{"--out", zip, "--socket", sock}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrote: %v", err)
	}
	out = withIO(t, "")
	if err := runExport([]string{"--out", zip, "--force", "--json", "--no-subscriptions", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	var res ExportResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || res.Path != zip || res.Subscriptions != 0 || res.Layers != 1 ||
		len(res.Warnings) != 1 || res.Warnings[0] != rpc.ImportWarnSecrets {
		t.Fatalf("json: %+v %v\n%s", res, err, out)
	}
	if err := runExport([]string{"--out", "-", "--json", "--socket", sock}); err == nil {
		t.Fatal("--out - with --json")
	}
	if err := runExport([]string{"extra", "--socket", sock}); err == nil {
		t.Fatal("positional argument accepted")
	}

	// stdout: the zip itself.
	out = withIO(t, "")
	if err := runExport([]string{"--out", "-", "--socket", sock}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("PK")) {
		t.Fatalf("stdout is not a zip: %q", out.Bytes()[:min(8, out.Len())])
	}

	// The full export imports on a fresh machine.
	os.WriteFile(zip, out.Bytes(), 0o600)
	sock2, e2 := importHelper(t)
	withIO(t, "")
	if err := runImport([]string{zip, "--yes", "--socket", sock2}); err != nil {
		t.Fatal(err)
	}
	subs, _ := e2.Subscriptions()
	ls, _ := e2.Layers()
	var mine bool
	for _, l := range ls {
		mine = mine || l.ID == "mine"
	}
	if len(subs) != 1 || subs[0].ID != "home" || !mine {
		t.Fatalf("imported subs %+v layers %+v", subs, ls)
	}
}

func TestExportNeedsHelper(t *testing.T) {
	withIO(t, "")
	err := runExport([]string{"--out", filepath.Join(t.TempDir(), "x.zip"), "--socket", filepath.Join(t.TempDir(), "none.sock")})
	if err == nil || !strings.Contains(err.Error(), "cannot reach the helper") {
		t.Fatalf("got %v", err)
	}
}
