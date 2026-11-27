package scm

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeSC answers sc.exe steps: stop and query say STOPPED.
type fakeSC struct{ calls []string }

func (f *fakeSC) Run(_ context.Context, argv []string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(argv[1:], " "))
	return []byte(QueryText(argv[2], 1, 0)), nil
}

// keeperFixture is a host at revision rev with file content "old",
// running TwinnelMihomo, and the helper "new".
func keeperFixture(rev int) (*HostKeeper, *memFiles, *fakeSC) {
	files := &memFiles{data: map[string]string{tHost: "old", tHelper: "new"}, locked: map[string]bool{tHost: true}}
	revs := map[string]int{}
	running := true
	r := HostReader{
		Exists: func(p string) bool { _, ok := files.data[p]; return ok },
		Version: func(_ context.Context, bin string) (string, error) {
			return map[string]string{"old": "0.1.0", "new": "0.2.0"}[files.data[bin]], nil
		},
		Revision: func(_ context.Context, bin string) int {
			if v, ok := revs[bin]; ok {
				return v
			}
			return map[string]int{"old": rev, "new": 1}[files.data[bin]]
		},
		Same: func(a, b string) bool { return files.data[a] == files.data[b] },
		ReadDir: func(string) ([]string, error) {
			var names []string
			for p := range files.data {
				names = append(names, p[strings.LastIndex(p, `\`)+1:])
			}
			return names, nil
		},
		Service: func(context.Context, string) (string, bool, int, error) {
			return `"` + tHost + `" svchost --name TwinnelMihomo -- m.exe`, running, 9, nil
		},
		// The running process is whichever file holds the lock.
		Image: func(int) string {
			for p, l := range files.locked {
				if l {
					return p
				}
			}
			return ""
		},
	}
	ex := &fakeSC{}
	k := &HostKeeper{Src: tHelper, Reader: r, Revision: 1, Exec: ex, Files: files,
		Labels: func() []string { return []string{"TwinnelMihomo"} }, Now: func() time.Time { return time.Unix(0, 7) }}
	return k, files, ex
}

func TestHostKeeperSameRevision(t *testing.T) {
	k, files, ex := keeperFixture(1)
	if st := k.State(context.Background()); st == nil || !st.Stale || st.Version != "0.1.0" {
		t.Fatalf("state = %+v", st)
	}
	r, err := k.Refresh(context.Background())
	if err != nil || r == nil || !r.Replaced || len(r.Restarted) != 0 || r.From != "0.1.0" || r.To != "0.2.0" {
		t.Fatalf("refresh = %+v, %v", r, err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("service touched: %v", ex.calls)
	}
	if files.data[tHost] != "new" {
		t.Errorf("host not replaced: %v", files.data)
	}
	// mihomo still runs the old copy (kept aside, locked): not stale, as
	// its revision is current; it picks up the file at its next restart.
	if st := k.State(context.Background()); st == nil || st.Stale || st.Version != "0.2.0" {
		t.Fatalf("after: %+v", st)
	}
	if r, err := k.Refresh(context.Background()); r != nil || err != nil {
		t.Errorf("second refresh = %+v, %v", r, err)
	}
}

func TestHostKeeperOlderRevisionRestarts(t *testing.T) {
	k, files, ex := keeperFixture(0)
	files.locked = map[string]bool{} // the stop releases it (the fake SC does not)
	r, err := k.Refresh(context.Background())
	if err != nil || r == nil || len(r.Restarted) != 1 || r.Restarted[0] != "TwinnelMihomo" {
		t.Fatalf("refresh = %+v, %v", r, err)
	}
	if got := strings.Join(ex.calls, "; "); got != "stop TwinnelMihomo; query TwinnelMihomo; start TwinnelMihomo" {
		t.Errorf("sc calls: %s", got)
	}
	if files.data[tHost] != "new" || len(files.data) != 2 {
		t.Errorf("files = %v (the old copy is deleted once stopped)", files.data)
	}
}

// A development helper reports the host and never writes.
func TestHostKeeperReadOnly(t *testing.T) {
	k, files, ex := keeperFixture(0)
	k.Files, k.Exec = nil, nil
	if r, err := k.Refresh(context.Background()); r != nil || err != nil {
		t.Fatalf("refresh = %+v, %v", r, err)
	}
	if files.data[tHost] != "old" || len(ex.calls) != 0 {
		t.Error("read-only keeper wrote")
	}
	if st := k.State(context.Background()); st == nil || !st.Stale {
		t.Errorf("state = %+v", st)
	}
}

// The copy a refresh moved aside waits while mihomo runs it, and goes as
// soon as mihomo restarts on the current file (Tidy after a service
// operation, or State's next read after the SCM restarted it).
func TestHostKeeperTidyAfterRestart(t *testing.T) {
	for _, via := range []string{"tidy", "state"} {
		t.Run(via, func(t *testing.T) {
			k, files, _ := keeperFixture(1)
			k.TTL = time.Minute
			if _, err := k.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			clock := time.Unix(0, 7)
			k.Now = func() time.Time { clock = clock.Add(time.Hour); return clock } // every State reads again
			aside := `C:\Program Files\Twinnel\twinnel-svchost.old-7.exe`
			if st := k.State(context.Background()); st == nil || st.Aside != 1 || files.data[aside] != "old" {
				t.Fatalf("while running the old copy: %+v, files %v", st, files.data)
			}
			// Tidy leaves a copy in use alone.
			if err := k.Tidy(context.Background()); err != nil || files.data[aside] != "old" {
				t.Fatalf("tidy while in use: %v, files %v", err, files.data)
			}
			// mihomo restarts: it runs the host's own file now.
			files.locked = map[string]bool{tHost: true}
			if via == "tidy" {
				if err := k.Tidy(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if st := k.State(context.Background()); st == nil || st.Aside != 0 {
				t.Fatalf("after restart: %+v", st)
			}
			if _, ok := files.data[aside]; ok || len(files.data) != 2 {
				t.Errorf("files = %v", files.data)
			}
		})
	}
}

// A development helper reports the copies left aside and never deletes
// them.
func TestHostKeeperTidyReadOnly(t *testing.T) {
	k, files, _ := keeperFixture(1)
	files.data[tOld] = "older"
	k.Files, k.Exec = nil, nil
	if err := k.Tidy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := k.State(context.Background()); st == nil || st.Aside != 1 || files.data[tOld] != "older" {
		t.Fatalf("state %+v, files %v", st, files.data)
	}
}
