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
