package main

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// shotDigests catches a shot run that saved one picture under several
// names: a capture that returned a stale frame (a hidden window on Windows
// draws no frames after its first, so every capture repeated it) or a step
// that changed nothing. Two names with the same pixels fail the run.
type shotDigests struct {
	mu     sync.Mutex
	byHash map[[32]byte]string // digest → first name saved with it
	byName map[string][32]byte
	dups   map[string]string // name → the earlier name with the same picture
}

// add records a saved shot. A name saved again replaces its own entry.
func (d *shotDigests) add(name string, data []byte) (dupOf string) {
	sum := sha256.Sum256(data)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byHash == nil {
		d.byHash, d.byName, d.dups = map[[32]byte]string{}, map[string][32]byte{}, map[string]string{}
	}
	if old, ok := d.byName[name]; ok && d.byHash[old] == name {
		delete(d.byHash, old)
	}
	delete(d.dups, name)
	d.byName[name] = sum
	if first, ok := d.byHash[sum]; ok && first != name {
		d.dups[name] = first
		return first
	}
	d.byHash[sum] = name
	return ""
}

// err lists the names that repeated an earlier shot, or nil.
func (d *shotDigests) err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.dups) == 0 {
		return nil
	}
	var lines []string
	for n, first := range d.dups {
		lines = append(lines, n+" = "+first)
	}
	sort.Strings(lines)
	return fmt.Errorf("%d shot(s) repeat an earlier one (stale capture?): %s", len(lines), strings.Join(lines, ", "))
}
