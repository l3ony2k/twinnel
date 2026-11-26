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
