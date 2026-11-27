package health

import (
	"slices"
	"strings"
	"testing"
)

func TestApplyUserChoices(t *testing.T) {
	s := DefaultSettings()
	s.ApplyUserChoices(nil, true)
	if !slices.Equal(s.SensitiveGroups, DefaultSettings().SensitiveGroups) || !s.Egress {
		t.Fatalf("no groups: %+v", s)
	}
	groups := []string{"PROXY"}
	s.ApplyUserChoices(groups, false)
	groups[0] = "changed"
	if !slices.Equal(s.SensitiveGroups, []string{"PROXY"}) || s.Egress {
		t.Fatalf("groups: %+v", s)
	}
}

func TestRegistryCheckIDs(t *testing.T) {
	r := DefaultRegistry()
	if err := r.CheckIDs(nil); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckIDs([]string{"leak-guard", "controller-owner"}); err != nil {
		t.Fatal(err)
	}
	err := r.CheckIDs([]string{"leak-guard", "nosuch", "other"})
	if err == nil || !strings.Contains(err.Error(), `"nosuch", "other"`) || !strings.Contains(err.Error(), "tailnet-route") {
		t.Fatalf("got %v", err)
	}
}
