package main

import (
	"testing"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/status"
)

func TestDockPolicy(t *testing.T) {
	reg, acc := mygo.ActivationPolicyRegular, mygo.ActivationPolicyAccessory
	for _, tc := range []struct {
		setting string
		open    bool
		want    mygo.ActivationPolicy
	}{
		{"", false, acc}, // 打开窗口时: a menu bar app while no window is open
		{"", true, reg},  // and a Dock icon while it is
		{status.DockAlways, false, reg},
		{status.DockAlways, true, reg},
		{status.DockNever, false, acc},
		{status.DockNever, true, acc},
		{"unknown", true, reg}, // an unknown value reads as the default
		{"unknown", false, acc},
	} {
		if got := dockPolicy(tc.setting, tc.open); got != tc.want {
			t.Errorf("dockPolicy(%q, %v) = %s, want %s", tc.setting, tc.open, got, tc.want)
		}
	}
}

func TestDockChoices(t *testing.T) {
	labels, values := dockChoices()
	if len(labels) != 3 || len(values) != 3 || values[0] != "" {
		t.Fatalf("dockChoices = %q %q; want the default first", labels, values)
	}
	for _, l := range labels {
		if l == "" {
			t.Fatalf("empty label in %q", labels)
		}
	}
}

// Shots and other systems never change the activation policy.
func TestDockManaged(t *testing.T) {
	a := &app{shotMode: true}
	if a.dockManaged() {
		t.Fatal("shot mode must not manage the Dock icon")
	}
	if !platformFor("darwin").dock || platformFor("linux").dock || platformFor("windows").dock {
		t.Fatal("the Dock setting is macOS only")
	}
}
