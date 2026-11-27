package dnswatch

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/sysdns"
)

// fakeDNS is the system DNS of a few network services.
type fakeDNS struct {
	svcs map[string][]string
	sets []string
	fail error
}

func (f *fakeDNS) Services(context.Context) ([]sysdns.Service, error) {
	var out []sysdns.Service
	for _, n := range []string{"Wi-Fi", "Ethernet"} {
		if s, ok := f.svcs[n]; ok {
			out = append(out, sysdns.Service{Name: n, Servers: slices.Clone(s)})
		}
	}
	return out, nil
}

func (f *fakeDNS) Set(_ context.Context, name string, servers []string) error {
	if f.fail != nil {
		return f.fail
	}
	f.svcs[name] = slices.Clone(servers)
	f.sets = append(f.sets, name)
	return nil
}

type rig struct {
	t       *testing.T
	now     time.Time
	dns     *fakeDNS
	backup  *sysdns.Backup
	answers bool
	svc     Service
	busy    bool
	changes []State
	w       *Watchdog
}

var target = []string{"127.0.0.1"}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, now: time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC), answers: true}
	r.dns = &fakeDNS{svcs: map[string][]string{"Wi-Fi": {"127.0.0.1"}, "Ethernet": {"127.0.0.1"}}}
	r.backup = &sysdns.Backup{At: r.now, Services: []sysdns.Service{
		{Name: "Wi-Fi", Servers: nil},                      // automatic (DHCP)
		{Name: "Ethernet", Servers: []string{"192.0.2.53"}}, // manual
	}}
	r.svc = Service{Known: true, Running: true, PID: 100, Runs: 1}
	r.w = r.watchdog(filepath.Join(t.TempDir(), "dns-watchdog.json"))
	return r
}

func (r *rig) watchdog(path string) *Watchdog {
	return &Watchdog{
		DNS:    r.dns,
		Backup: func() (*sysdns.Backup, error) { return r.backup, nil },
		Target: func() []string { return target },
		Probe: func(context.Context) error {
			if r.answers {
				return nil
			}
			return errors.New("i/o timeout")
		},
		Service:  func(context.Context) (Service, error) { return r.svc, nil },
		Busy:     func() bool { return r.busy },
		Path:     path,
		Now:      func() time.Time { return r.now },
		OnChange: func(s State) { r.changes = append(r.changes, s) },
	}
}

// run ticks every 5s for d.
func (r *rig) run(d time.Duration) {
	for end := r.now.Add(d); !r.now.After(end); r.now = r.now.Add(5 * time.Second) {
		r.w.Tick(context.Background())
	}
}

func (r *rig) phase() string { return r.w.State().Phase }

func (r *rig) servers(name string) []string { return r.dns.svcs[name] }

func TestHealthyNeverTouches(t *testing.T) {
	r := newRig(t)
	r.run(10 * time.Minute)
	if r.phase() != PhaseWatching || len(r.dns.sets) != 0 || len(r.changes) != 0 {
		t.Fatalf("phase %s sets %v changes %v", r.phase(), r.dns.sets, r.changes)
	}
}

func TestNoBackupIsOff(t *testing.T) {
	r := newRig(t)
	r.backup = nil
	r.answers = false
	r.svc.Running = false
	r.run(5 * time.Minute)
	if r.phase() != PhaseOff || len(r.dns.sets) != 0 {
		t.Fatalf("phase %s sets %v", r.phase(), r.dns.sets)
	}
}

func TestStoppedRestoresThenPointsBack(t *testing.T) {
	r := newRig(t)
	r.run(time.Minute)
	r.answers, r.svc.Running, r.svc.PID = false, false, 0
	r.run(15 * time.Second) // a restart's worth: nothing yet
	if r.phase() != PhaseWatching || len(r.dns.sets) != 0 {
		t.Fatalf("acted too early: %s %v", r.phase(), r.dns.sets)
	}
	r.run(10 * time.Second)
	st := r.w.State()
	if st.Phase != PhaseRestored || st.Cause != CauseStopped {
		t.Fatalf("state %+v", st)
	}
	if r.servers("Wi-Fi") != nil || !slices.Equal(r.servers("Ethernet"), []string{"192.0.2.53"}) {
		t.Fatalf("not restored: %v", r.dns.svcs)
	}
	if len(r.changes) != 1 || r.changes[0].Phase != PhaseRestored {
		t.Fatalf("changes %+v", r.changes)
	}
	// Still down: stays restored.
	r.run(5 * time.Minute)
	if r.phase() != PhaseRestored {
		t.Fatal("left restored while mihomo is down")
	}
	// Back: points back only after the hysteresis.
	r.answers, r.svc.Running, r.svc.PID, r.svc.Runs = true, true, 200, 2
	r.run(25 * time.Second)
	if r.phase() != PhaseRestored {
		t.Fatal("pointed back before the hysteresis")
	}
	r.run(10 * time.Second)
	if r.phase() != PhaseWatching {
		t.Fatalf("not pointed back: %+v", r.w.State())
	}
	if !slices.Equal(r.servers("Wi-Fi"), target) || !slices.Equal(r.servers("Ethernet"), target) {
		t.Fatalf("not pointed: %v", r.dns.svcs)
	}
	if len(r.changes) != 2 || r.changes[1].Phase != PhaseWatching {
		t.Fatalf("changes %+v", r.changes)
	}
}

func TestFlappingAnswerResetsHysteresis(t *testing.T) {
	r := newRig(t)
	r.answers, r.svc.Running = false, false
	r.run(30 * time.Second)
	if r.phase() != PhaseRestored {
		t.Fatal("not restored")
	}
	r.svc.Running = true
	for range 5 { // answers 20s, then a miss
		r.answers = true
		r.run(20 * time.Second)
		r.answers = false
		r.run(0)
		r.now = r.now.Add(5 * time.Second)
	}
	if r.phase() != PhaseRestored {
		t.Fatal("pointed back while flapping")
	}
}

func TestCrashLoopRestoresEarly(t *testing.T) {
	r := newRig(t)
	r.run(10 * time.Second)
	// Fails, and each sample sees a new start (launchd runs grows).
	r.answers = false
	for i := range 4 {
		r.svc.Runs, r.svc.PID = 2+i, 300+i
		r.run(0)
		r.now = r.now.Add(5 * time.Second)
	}
	st := r.w.State()
	if st.Phase != PhaseRestored || st.Cause != CauseCrashLoop {
		t.Fatalf("state %+v", st)
	}
	// Answers again but still restarting: no point back.
	r.answers = true
	for i := range 8 {
		r.svc.Runs, r.svc.PID = 10+i, 400+i
		r.run(0)
		r.now = r.now.Add(5 * time.Second)
	}
	if r.phase() != PhaseRestored {
		t.Fatal("pointed back during a crash loop")
	}
}

func TestCrashLoopByPID(t *testing.T) {
	r := newRig(t)
	r.svc.Runs = 0 // a platform without a start count
	r.answers = false
	for i := range 4 {
		r.svc.PID = 500 + i
		r.run(0)
		r.now = r.now.Add(5 * time.Second)
	}
	if st := r.w.State(); st.Cause != CauseCrashLoop {
		t.Fatalf("state %+v", st)
	}
}

func TestHungService(t *testing.T) {
	r := newRig(t)
	r.answers = false // running, same PID, no answer
	r.run(80 * time.Second)
	if r.phase() != PhaseWatching {
		t.Fatal("restored a running service too early")
	}
	r.run(15 * time.Second)
	if st := r.w.State(); st.Cause != CauseNoAnswer {
		t.Fatalf("state %+v", st)
	}
}

func TestBusySuppresses(t *testing.T) {
	r := newRig(t)
	r.busy = true
	r.answers, r.svc.Running = false, false
	r.run(2 * time.Minute)
	if r.phase() != PhaseWatching || len(r.dns.sets) != 0 {
		t.Fatal("acted during a deliberate restart")
	}
	r.busy = false
	r.run(15 * time.Second)
	if r.phase() != PhaseWatching {
		t.Fatal("busy time counted as downtime")
	}
}

func TestUserChangedBeforeFailureIsLeftAlone(t *testing.T) {
	r := newRig(t)
	r.dns.svcs["Wi-Fi"] = []string{"198.51.100.1"} // set by hand
	r.answers, r.svc.Running = false, false
	r.run(time.Minute)
	if !slices.Equal(r.servers("Wi-Fi"), []string{"198.51.100.1"}) {
		t.Fatalf("overwrote the user's DNS: %v", r.servers("Wi-Fi"))
	}
	if !slices.Equal(r.servers("Ethernet"), []string{"192.0.2.53"}) {
		t.Fatal("Ethernet not restored")
	}
	st := r.w.State()
	if len(st.Restored) != 1 || st.Restored[0].Name != "Ethernet" {
		t.Fatalf("restored %+v", st.Restored)
	}
	// Pointing back leaves Wi-Fi alone too.
	r.answers, r.svc.Running = true, true
	r.run(time.Minute)
	if !slices.Equal(r.servers("Wi-Fi"), []string{"198.51.100.1"}) || !slices.Equal(r.servers("Ethernet"), target) {
		t.Fatalf("after point back: %v", r.dns.svcs)
	}
}

func TestNothingPointedNothingDone(t *testing.T) {
	r := newRig(t)
	r.dns.svcs["Wi-Fi"] = nil
	r.dns.svcs["Ethernet"] = []string{"192.0.2.53"}
	r.answers, r.svc.Running = false, false
	r.run(2 * time.Minute)
	if r.phase() != PhaseWatching || len(r.dns.sets) != 0 || len(r.changes) != 0 {
		t.Fatalf("phase %s sets %v", r.phase(), r.dns.sets)
	}
}

func TestUserChangedWhileRestoredIsNotPointedBack(t *testing.T) {
	r := newRig(t)
	r.answers, r.svc.Running = false, false
	r.run(30 * time.Second)
	r.dns.svcs["Ethernet"] = []string{"203.0.113.9"} // the user picks a DNS meanwhile
	r.answers, r.svc.Running = true, true
	r.run(time.Minute)
	st := r.w.State()
	if st.Phase != PhaseWatching || !slices.Equal(st.LeftAlone, []string{"Ethernet"}) {
		t.Fatalf("state %+v", st)
	}
	if !slices.Equal(r.servers("Ethernet"), []string{"203.0.113.9"}) || !slices.Equal(r.servers("Wi-Fi"), target) {
		t.Fatalf("services %v", r.dns.svcs)
	}
}

func TestKeptRestoredEndsWithBackup(t *testing.T) {
	r := newRig(t)
	r.answers, r.svc.Running = false, false
	r.run(30 * time.Second)
	// 保持原设置: dns.restore removes the backup.
	r.backup = nil
	r.answers, r.svc.Running = true, true
	r.run(2 * time.Minute)
	if r.phase() != PhaseOff || !slices.Equal(r.servers("Ethernet"), []string{"192.0.2.53"}) {
		t.Fatalf("phase %s services %v", r.phase(), r.dns.svcs)
	}
}

func TestResetForgetsRestore(t *testing.T) {
	r := newRig(t)
	r.answers, r.svc.Running = false, false
	r.run(30 * time.Second)
	r.w.Reset()
	if r.phase() != PhaseOff {
		t.Fatal("reset kept the restore")
	}
	if n := len(r.changes); n != 2 || r.changes[1].Phase != PhaseOff {
		t.Fatalf("changes %+v", r.changes)
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	r := newRig(t)
	r.answers, r.svc.Running = false, false
	r.run(30 * time.Second)
	// A new helper process reads the state and still points back.
	r.w = r.watchdog(r.w.Path)
	if r.phase() != PhaseRestored {
		t.Fatal("state lost")
	}
	r.answers, r.svc.Running = true, true
	r.run(time.Minute)
	if r.phase() != PhaseWatching || !slices.Equal(r.servers("Wi-Fi"), target) {
		t.Fatalf("after restart: %+v %v", r.w.State(), r.dns.svcs)
	}
}

func TestRestoreFailureKeepsWatching(t *testing.T) {
	r := newRig(t)
	r.dns.fail = errors.New("permission denied")
	r.answers, r.svc.Running = false, false
	r.run(time.Minute)
	if r.phase() != PhaseWatching {
		t.Fatal("claimed a restore that failed")
	}
	r.dns.fail = nil
	r.run(5 * time.Second)
	if r.phase() != PhaseRestored {
		t.Fatal("did not retry")
	}
}

func TestUnknownServiceCountsAsDown(t *testing.T) {
	r := newRig(t)
	r.w.Service = nil
	r.answers = false
	r.run(25 * time.Second)
	if st := r.w.State(); st.Cause != CauseStopped {
		t.Fatalf("state %+v", st)
	}
}
