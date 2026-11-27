package i18n

import (
	"reflect"
	"testing"
)

// Every string field must be filled.
func TestComplete(t *testing.T) {
	checkFilled(t, reflect.ValueOf(*T))
	if T.ModeName("global") != "全局" || T.TSState("Running") != "已连接" || T.Path("derp") != "DERP 中继" {
		t.Error("lookups")
	}
}

// checkFilled reports empty string fields, also in embedded structs (the
// per-area string sets such as ControlStrings).
func checkFilled(t *testing.T, v reflect.Value) {
	t.Helper()
	for i := range v.NumField() {
		f, sf := v.Field(i), v.Type().Field(i)
		switch {
		case sf.Anonymous && f.Kind() == reflect.Struct:
			checkFilled(t, f)
		case f.Kind() == reflect.String && f.String() == "":
			t.Errorf("empty string %s", sf.Name)
		}
	}
}

// A field of an embedded per-area set (AboutStrings ...) must not share
// its name with a field of Strings or of another set: Go then reads the
// outer one (or none), and the text silently goes wrong.
func TestNoShadowedStrings(t *testing.T) {
	seen := map[string]string{}
	var walk func(rt reflect.Type, owner string)
	walk = func(rt reflect.Type, owner string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type, f.Type.Name())
				continue
			}
			if prev, ok := seen[f.Name]; ok {
				t.Errorf("%s is in both %s and %s", f.Name, prev, owner)
			}
			seen[f.Name] = owner
		}
	}
	walk(reflect.TypeOf(Strings{}), "Strings")
}
