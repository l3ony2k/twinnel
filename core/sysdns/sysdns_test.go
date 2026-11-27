package sysdns

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

type fakePlatform struct {
	svcs []Service
	sets []string
	fail string
}

func (f *fakePlatform) Services(context.Context) ([]Service, error) {
	return slices.Clone(f.svcs), nil
}

func (f *fakePlatform) Set(_ context.Context, name string, servers []string) error {
	if name == f.fail {
		return errors.New("refused")
	}
	for i := range f.svcs {
		if f.svcs[i].Name == name {
			f.svcs[i].Servers = slices.Clone(servers)
		}
	}
	f.sets = append(f.sets, name)
	return nil
}

var mihomo = []string{"127.0.0.1"}

func TestPointRestore(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi"}, {Name: "USB LAN", Servers: []string{"192.0.2.53"}}}}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json")}
	st, err := m.Point(context.Background(), mihomo)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Pointed || len(st.Changed) != 2 || st.Backup == nil || len(st.Backup.Services) != 2 {
		t.Fatalf("status %+v", st)
	}
	// Pointing again changes nothing and keeps the original backup.
	st, err = m.Point(context.Background(), mihomo)
	if err != nil || len(st.Changed) != 0 {
		t.Fatalf("second point: %+v %v", st, err)
	}
	if bk, _ := m.ReadBackup(); !slices.Equal(bk.Services[1].Servers, []string{"192.0.2.53"}) {
		t.Fatalf("backup overwritten: %+v", bk)
	}
	// A service that appears later is added to the backup.
	p.svcs = append(p.svcs, Service{Name: "Ethernet", Servers: []string{"198.51.100.1"}})
	if _, err := m.Point(context.Background(), mihomo); err != nil {
		t.Fatal(err)
	}
	st, err = m.Restore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Backup != nil || len(st.Changed) != 3 {
		t.Fatalf("restore %+v", st)
	}
	want := []Service{{Name: "Wi-Fi", Servers: nil}, {Name: "USB LAN", Servers: []string{"192.0.2.53"}}, {Name: "Ethernet", Servers: []string{"198.51.100.1"}}}
	for i, s := range p.svcs {
		if s.Name != want[i].Name || !slices.Equal(s.Servers, want[i].Servers) {
			t.Fatalf("after restore %+v", p.svcs)
		}
	}
	if _, err := m.Restore(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("second restore: %v", err)
	}
}

func TestPointFailureKeepsBackup(t *testing.T) {
	p := &fakePlatform{svcs: []Service{{Name: "Wi-Fi", Servers: []string{"192.0.2.1"}}, {Name: "Ethernet"}}, fail: "Ethernet"}
	m := &Manager{P: p, Path: filepath.Join(t.TempDir(), "dns-backup.json")}
	if _, err := m.Point(context.Background(), mihomo); err == nil {
		t.Fatal("no error")
	}
	p.fail = ""
	if _, err := m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.svcs[0].Servers, []string{"192.0.2.1"}) {
		t.Fatalf("not restored: %+v", p.svcs)
	}
}

func TestPointNeedsServices(t *testing.T) {
	m := &Manager{P: &fakePlatform{}, Path: filepath.Join(t.TempDir(), "b.json")}
	if _, err := m.Point(context.Background(), mihomo); err == nil {
		t.Fatal("pointed with no services")
	}
	if _, err := m.Point(context.Background(), nil); err == nil {
		t.Fatal("pointed at nothing")
	}
}
