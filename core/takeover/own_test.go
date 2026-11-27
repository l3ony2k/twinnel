package takeover

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/store"
)

func TestOwnHas(t *testing.T) {
	state := filepath.Join(t.TempDir(), "Twinnel")
	adopted := filepath.Join(t.TempDir(), "mihomo", "config.yaml")
	own := Own{Dirs: []string{state}, Configs: []string{adopted}, Labels: []string{"TwinnelMihomo"}}
	svc := func(label string) *ServiceDef { return &ServiceDef{Label: label} }
	cases := []struct {
		name string
		m    *MihomoSetup
		want bool
	}{
		{"nil", nil, false},
		// Windows QA round 1: Twinnel's own service and config.
		{"own service", &MihomoSetup{Service: svc("TwinnelMihomo"), Config: "/elsewhere/config.yaml"}, true},
		{"config in the state dir", &MihomoSetup{Config: filepath.Join(state, "mihomo", "config.yaml")}, true},
		{"the adopted config", &MihomoSetup{Service: svc("com.example.mihomo"), Config: adopted}, true},
		{"a hand-made setup", &MihomoSetup{Service: svc("com.example.mihomo"), Config: filepath.Join(filepath.Dir(adopted), "other.yaml")}, false},
		{"a sibling of the state dir", &MihomoSetup{Config: state + "-old/config.yaml"}, false},
		{"the state dir itself", &MihomoSetup{Config: state}, false},
	}
	for _, c := range cases {
		if got := own.Has(c.m); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if !foldPaths {
		return
	}
	if !own.Has(&MihomoSetup{Config: strings.ToUpper(filepath.Join(state, "mihomo", "config.yaml"))}) {
		t.Error("paths ignore case on this platform")
	}
}

func TestDefaultOwnLabels(t *testing.T) {
	for _, l := range []string{store.DefaultMihomoLabel, legacy.MihomoLabel} {
		if !DefaultOwn().Has(&MihomoSetup{Service: &ServiceDef{Label: l}, Config: "/x/config.yaml"}) {
			t.Errorf("%s is Twinnel's", l)
		}
	}
	if !DefaultOwn().Has(&MihomoSetup{Config: filepath.Join(store.DefaultDir, "mihomo", "config.yaml")}) {
		t.Error("a config in the default state dir is Twinnel's")
	}
}

// Discovery marks Twinnel's own setup and no plan is made against it; the
// same setup is a plan when it is someone else's.
func TestBuildPlanRefusesOwnSetup(t *testing.T) {
	live := liveHeader + subBody[:strings.Index(subBody, "rules:")] + liveRules
	dir := setup(t, live)
	opt := Options{NoValidate: true}
	opt.Own = Own{Labels: []string{"x.mihomo"}}
	d, err := Discover(context.Background(), fakeProbe{dir}, opt.DiscoverOptions)
	if err != nil || !d.Mihomo.Twinnel {
		t.Fatalf("discover: %+v %v", d, err)
	}
	if _, err := BuildPlan(context.Background(), fakeProbe{dir}, opt); !errors.Is(err, ErrOwnSetup) {
		t.Fatalf("plan against Twinnel's own service: %v", err)
	}
	opt.Own = Own{Dirs: []string{filepath.Join(dir, "conf")}}
	if _, err := BuildPlan(context.Background(), fakeProbe{dir}, opt); !errors.Is(err, ErrOwnSetup) {
		t.Fatalf("plan against a config in Twinnel's state dir: %v", err)
	}
	opt.Own = Own{}
	if _, err := BuildPlan(context.Background(), fakeProbe{dir}, opt); err != nil {
		t.Fatalf("a hand-made setup: %v", err)
	}
}
