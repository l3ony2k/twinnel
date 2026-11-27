package i18n

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestPlurals(t *testing.T) {
	for _, tt := range []struct {
		f    string
		args []any
		want string
	}{
		{"%d issue{s} found", []any{1}, "1 issue found"},
		{"%d issue{s} found", []any{3}, "3 issues found"},
		{"%d issue{s} found", []any{0}, "0 issues found"},
		{"%d entr{y|ies}", []any{1}, "1 entry"},
		{"%d entr{y|ies}", []any{2}, "2 entries"},
		{"%s: %d rule{s}, %d group{s}", []any{"x", 1, 2}, "x: 1 rule, 2 groups"},
		{"%[2]d of %[1]s item{s}", []any{"a", 1}, "1 of a item"},
		{"%.1f%% · %d line{s}", []any{1.5, 1}, "1.5% · 1 line"},
		{"no groups", nil, "no groups"},
	} {
		if got := F(tt.f, tt.args...); got != tt.want {
			t.Errorf("F(%q, %v) = %q, want %q", tt.f, tt.args, got, tt.want)
		}
	}
}

// A count in English is followed by a plural group, not a fixed plural
// noun ("1 coexistence issues found", R-unslop-26). Units ("%d s ago")
// are not nouns.
var fixedPlural = regexp.MustCompile(`%(\[\d\])?d (?:[a-zA-Z-]+ )?[a-zA-Z]+(?:s|ies)\b`)

func TestEnglishPluralForms(t *testing.T) {
	walkTable(reflect.ValueOf(en), "", func(path, s string) {
		for _, m := range fixedPlural.FindAllString(s, -1) {
			if strings.HasSuffix(m, " s") {
				continue
			}
			t.Errorf("%s: %q has a fixed plural %q; write {s}", path, s, m)
		}
	})
}

// Chinese has no plural: no {…} groups in the zh table.
func TestChineseHasNoPluralGroups(t *testing.T) {
	walkTable(reflect.ValueOf(zh), "", func(path, s string) {
		if strings.ContainsAny(s, "{}") {
			t.Errorf("%s: %q", path, s)
		}
	})
}

// walkStrings calls fn for every string in a table (fields, maps, slices).
func walkTable(v reflect.Value, path string, fn func(path, s string)) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			p := path
			if !f.Anonymous {
				p += f.Name
			}
			walkTable(v.Field(i), p, fn)
		}
	case reflect.String:
		fn(path, v.String())
	case reflect.Map:
		for _, k := range v.MapKeys() {
			walkTable(v.MapIndex(k), path+"["+k.String()+"]", fn)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walkTable(v.Index(i), path, fn)
		}
	}
}
