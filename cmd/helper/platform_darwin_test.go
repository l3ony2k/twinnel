//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3ony2k/duet/core/engine"
	"github.com/l3ony2k/duet/core/store"
	"github.com/l3ony2k/duet/platform/darwin"
)

// With --tailnet-status-file the health checks read the file, never the
// live tailnet.
func TestHealthOptionsTailnetFile(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(st, nil)
	file := filepath.Join("..", "..", "core", "engine", "testdata", "tailscale-status.json")
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	o := &options{}
	if healthOptions(e, o).TailscaleStatus != nil {
		t.Fatal("status override without --tailnet-status-file")
	}

	o.tailnetStatus = file
	e.Tailnet = fileTailnet(file)
	opt := healthOptions(e, o)
	if opt.TailscaleStatus == nil {
		t.Fatal("health checks would read the live tailnet")
	}
	opt.TailscaleBin = "/nonexistent/tailscale"
	ts, err := darwin.New(opt).Tailscale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := opt.TailscaleStatus(context.Background()); string(got) != string(want) || ts.BackendState == "" {
		t.Fatalf("%+v", ts)
	}
}
