package i18n

import (
	"reflect"
	"testing"
)

// Every string field must be filled.
func TestComplete(t *testing.T) {
	v := reflect.ValueOf(*T)
	for i := range v.NumField() {
		f := v.Field(i)
		if f.Kind() == reflect.String && f.String() == "" {
			t.Errorf("empty string %s", v.Type().Field(i).Name)
		}
	}
	if T.ModeName("global") != "全局" || T.TSState("Running") != "已连接" || T.Path("derp") != "DERP 中继" {
		t.Error("lookups")
	}
}
