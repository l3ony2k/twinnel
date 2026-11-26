//go:build linux

package main

import (
	"errors"
	"os/user"
	"testing"
)

func TestLinuxGroup(t *testing.T) {
	old := lookupGroup
	defer func() { lookupGroup = old }()
	have := map[string]bool{}
	lookupGroup = func(n string) (*user.Group, error) {
		if have[n] {
			return &user.Group{Name: n, Gid: "10"}, nil
		}
		return nil, errors.New("unknown")
	}
	have["wheel"] = true
	if g := linuxGroup("admin"); g != "wheel" {
		t.Fatalf("admin missing: %s", g)
	}
	if g := linuxGroup("staff"); g != "staff" {
		t.Fatalf("explicit: %s", g)
	}
	have["admin"] = true
	if g := linuxGroup("admin"); g != "admin" {
		t.Fatalf("admin exists: %s", g)
	}
}
