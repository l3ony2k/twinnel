package scm

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/service"
)

const (
	tHost   = `C:\Program Files\Twinnel\twinnel-svchost.exe`
	tHelper = `C:\Program Files\Twinnel\twinnel-helper.exe`
	tOld    = `C:\Program Files\Twinnel\twinnel-svchost.old-5.exe`
)

func planText(steps []service.Step) string {
	var b strings.Builder
	Dialect.FormatPlan(&b, steps)
	return b.String()
}

func TestHostRefreshPlan(t *testing.T) {
	now := time.Unix(0, 7)
	newAside := `C:\Program Files\Twinnel\twinnel-svchost.old-7.exe`
	for _, tc := range []struct {
		name  string
		h     Host
		want  string
		stale bool
	}{
		{name: "not installed", h: Host{Path: tHost}, want: ""},
		{name: "current", h: Host{Path: tHost, Present: true, Same: true, Services: []HostedService{{Label: "TwinnelMihomo", Running: true, Revision: 1}}}, want: ""},
		{name: "current, old copy left", h: Host{Path: tHost, Present: true, Same: true, Aside: []string{tOld}},
			want: "1. del /f \"" + tOld + "\"  (errors ignored)\n"},
		{name: "new file, same revision: no restart", stale: true,
			h: Host{Path: tHost, Present: true, Services: []HostedService{{Label: "TwinnelMihomo", Running: true, Revision: 1}}},
			want: "1. move /y \"" + tHost + "\" \"" + newAside + "\"\n" +
				"2. copy /y \"" + tHelper + "\" \"" + tHost + "\"\n" +
				"3. del /f \"" + newAside + "\"  (errors ignored)\n"},
		{name: "older revision running: restart", stale: true,
			h: Host{Path: tHost, Present: true, Services: []HostedService{{Label: "TwinnelMihomo", Running: true, Revision: 0}}},
			want: "1. sc.exe stop TwinnelMihomo\n" +
				"2. wait until `sc.exe query TwinnelMihomo` says \"STOPPED\" or \"1060\" (up to 30s)\n" +
				"3. move /y \"" + tHost + "\" \"" + newAside + "\"\n" +
				"4. copy /y \"" + tHelper + "\" \"" + tHost + "\"\n" +
				"5. sc.exe start TwinnelMihomo  (also when a step before failed)\n" +
				"6. del /f \"" + newAside + "\"  (errors ignored)\n"},
		{name: "file refreshed, service still on the old one: restart only", stale: true,
			h: Host{Path: tHost, Present: true, Same: true, Services: []HostedService{{Label: "TwinnelMihomo", Running: true, Image: tOld}}, Aside: []string{tOld}},
			want: "1. sc.exe stop TwinnelMihomo\n" +
				"2. wait until `sc.exe query TwinnelMihomo` says \"STOPPED\" or \"1060\" (up to 30s)\n" +
				"3. sc.exe start TwinnelMihomo  (also when a step before failed)\n" +
				"4. del /f \"" + tOld + "\"  (errors ignored)\n"},
		{name: "stopped service: never started", stale: true,
			h:    Host{Path: tHost, Present: true, Services: []HostedService{{Label: "TwinnelMihomo"}}},
			want: "1. move /y \"" + tHost + "\" \"" + newAside + "\"\n" + "2. copy /y \"" + tHelper + "\" \"" + tHost + "\"\n" + "3. del /f \"" + newAside + "\"  (errors ignored)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := planText(tc.h.RefreshPlan(tHelper, 1, now)); got != tc.want {
				t.Errorf("plan:\n%s\nwant:\n%s", got, tc.want)
			}
			if tc.h.Stale(1) != tc.stale {
				t.Errorf("Stale = %v", !tc.stale)
			}
		})
	}
}

// The plan runs: the running file is moved, never overwritten.
func TestHostRefreshPlanApplies(t *testing.T) {
	h := Host{Path: tHost, Present: true, Services: []HostedService{{Label: "TwinnelMihomo", Running: true, Revision: 1}}}
	files := &memFiles{data: map[string]string{tHost: "old", tHelper: "new"}, locked: map[string]bool{tHost: true}}
	if err := service.Apply(context.Background(), h.RefreshPlan(tHelper, 1, time.Unix(0, 7)), nil, files); err != nil {
		t.Fatal(err)
	}
	aside := `C:\Program Files\Twinnel\twinnel-svchost.old-7.exe`
	if files.data[tHost] != "new" || files.data[aside] != "old" {
		t.Errorf("files = %v", files.data)
	}
}

// memFiles is a filesystem whose locked files (a running program) can be
// renamed but not overwritten or deleted, as on Windows.
type memFiles struct {
	data   map[string]string
	locked map[string]bool
}

func (f *memFiles) MkdirAll(string, fs.FileMode) error { return nil }
func (f *memFiles) WriteFile(p string, b []byte, _ fs.FileMode) error {
	if f.locked[p] {
		return errors.New("in use")
	}
	f.data[p] = string(b)
	return nil
}
func (f *memFiles) Copy(src, dst string, m fs.FileMode) error {
	return f.WriteFile(dst, []byte(f.data[src]), m)
}
func (f *memFiles) Remove(p string) error {
	if f.locked[p] {
		return errors.New("in use")
	}
	delete(f.data, p)
	return nil
}
func (f *memFiles) Rename(src, dst string) error {
	f.data[dst], f.locked[dst] = f.data[src], f.locked[src]
	delete(f.data, src)
	delete(f.locked, src)
	return nil
}

func TestHostRead(t *testing.T) {
	r := HostReader{
		Exists:   func(p string) bool { return p == tHost },
		Version:  func(_ context.Context, bin string) (string, error) { return "0.1.0", nil },
		Revision: func(_ context.Context, bin string) int { return map[string]int{tHost: 1, tOld: 0}[bin] },
		Same:     func(a, b string) bool { return false },
		ReadDir: func(string) ([]string, error) {
			return []string{"twinnel-helper.exe", "twinnel-svchost.exe", "twinnel-svchost.old-5.exe", "twinnel-svchost.old-5.exe.txt"}, nil
		},
		Service: func(_ context.Context, l string) (string, bool, int, error) {
			switch l {
			case "TwinnelMihomo":
				return `"` + tHost + `" svchost --name TwinnelMihomo -- C:\m\mihomo.exe`, true, 42, nil
			case "Other":
				return `C:\other.exe`, true, 43, nil
			}
			return "", false, 0, errors.New("1060")
		},
		Image: func(pid int) string { return map[int]string{42: tOld}[pid] },
	}
	h := r.Read(context.Background(), tHost, tHelper, []string{"TwinnelMihomo", "twinnelmihomo", "Other", "Missing"})
	if !h.Present || h.Same || h.Version != "0.1.0" || len(h.Aside) != 1 || h.Aside[0] != tOld {
		t.Fatalf("host = %+v", h)
	}
	if len(h.Services) != 1 || h.Services[0] != (HostedService{Label: "TwinnelMihomo", Running: true, Image: tOld, Revision: 0}) {
		t.Fatalf("services = %+v", h.Services)
	}
	if got := h.Restarts(1); len(got) != 1 {
		t.Errorf("restarts = %v", got)
	}
}
