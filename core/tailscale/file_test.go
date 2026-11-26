package tailscale

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFileAPI(t *testing.T) {
	t.Setenv(StatusFileEnv, "../../cmd/app/testdata/shots-tailscale-status.json")
	api := DefaultAPI()
	if _, ok := api.(fileAPI); !ok {
		t.Fatalf("DefaultAPI with %s set: %T", StatusFileEnv, api)
	}
	c := New(api, true)
	st, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running() || st.Self.Name == "" || len(st.Peers) != 7 || st.ExitNode == nil || st.ExitNode.Name != "exit-sg" ||
		st.User != "user@example.test" || !st.Prefs.MagicDNS || st.Prefs.ExitNodeID == "" {
		t.Fatalf("state: %+v", st)
	}
	if err := c.Down(context.Background()); !errors.Is(err, ErrStatusFile) {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w, _ := api.WatchIPNBus(ctx, 0)
	if _, err := w.Next(); err == nil {
		t.Fatal("the idle watcher returned a notification")
	}
	t.Setenv(StatusFileEnv, "")
	if _, ok := DefaultAPI().(localAPI); !ok {
		t.Fatal("DefaultAPI without the variable is not the LocalAPI")
	}
}
