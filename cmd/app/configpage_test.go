package main

import (
	"slices"
	"testing"
)

func TestReorderIDs(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	for _, tc := range []struct {
		rows []int
		to   int
		want []string
	}{
		{[]int{0}, 2, []string{"b", "a", "c", "d"}},
		{[]int{3}, 0, []string{"d", "a", "b", "c"}},
		{[]int{1}, 4, []string{"a", "c", "d", "b"}},
		{[]int{0, 2}, 4, []string{"b", "d", "a", "c"}},
		{[]int{1}, 1, []string{"a", "b", "c", "d"}},
		{[]int{1}, 2, []string{"a", "b", "c", "d"}},
		{[]int{9}, 0, []string{"a", "b", "c", "d"}},
	} {
		if got := reorderIDs(ids, tc.rows, tc.to); !slices.Equal(got, tc.want) {
			t.Errorf("reorderIDs(%v, %d) = %v, want %v", tc.rows, tc.to, got, tc.want)
		}
	}
	if !slices.Equal(ids, []string{"a", "b", "c", "d"}) {
		t.Errorf("ids changed: %v", ids)
	}
}

// A layer dropped on a row takes the row's place, from either side; the
// accent line (above from below, under from above) shows where it lands.
func TestDropLayer(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	for _, tc := range []struct {
		dragged string
		at      int
		want    []string // nil: nothing sent
	}{
		{"a", 2, []string{"b", "c", "a", "d"}}, // down: under c
		{"a", 3, []string{"b", "c", "d", "a"}}, // to the end
		{"d", 1, []string{"a", "d", "b", "c"}}, // up: above b
		{"d", 0, []string{"d", "a", "b", "c"}}, // to the top
		{"b", 2, []string{"a", "c", "b", "d"}}, // one down
		{"c", 1, []string{"a", "c", "b", "d"}}, // one up
		{"b", 1, nil},                          // on itself
		{"x", 1, nil},                          // not a stored layer
		{"a", 4, nil},                          // past the list (a managed row)
		{"a", -1, nil},
	} {
		got, ok := dropLayer(ids, tc.dragged, tc.at)
		if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
			t.Errorf("dropLayer(%s → %d) = %v %v, want %v", tc.dragged, tc.at, got, ok, tc.want)
		}
	}
	if !slices.Equal(ids, []string{"a", "b", "c", "d"}) {
		t.Errorf("ids changed: %v", ids)
	}
}

// ⌥⌘↑ / ⌥⌘↓ swap the open layer with its neighbour, never past the ends.
func TestStepID(t *testing.T) {
	ids := []string{"a", "b", "c"}
	for _, tc := range []struct {
		i, delta int
		want     []string
	}{
		{1, -1, []string{"b", "a", "c"}},
		{1, 1, []string{"a", "c", "b"}},
		{0, -1, nil},
		{2, 1, nil},
		{-1, 1, nil},
		{1, 0, nil},
	} {
		got, ok := stepID(ids, tc.i, tc.delta)
		if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
			t.Errorf("stepID(%d, %d) = %v %v, want %v", tc.i, tc.delta, got, ok, tc.want)
		}
	}
	if !slices.Equal(ids, []string{"a", "b", "c"}) {
		t.Errorf("ids changed: %v", ids)
	}
}
