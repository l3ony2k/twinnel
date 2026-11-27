package healthfix

import (
	"reflect"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// TestPlatformGlossary: the fix previews are shown on every system, so
// they name no system's thing (core/i18n/glossary.go).
func TestPlatformGlossary(t *testing.T) {
	for tag, L := range map[string]*texts{"zh-Hans": &zh, "en": &en} {
		v := reflect.ValueOf(*L)
		for i := 0; i < v.NumField(); i++ {
			for _, goos := range i18n.Platforms {
				if f := i18n.ForeignTerms(goos, v.Field(i).String()); len(f) > 0 {
					t.Errorf("%s %s %s %v: %q", goos, tag, v.Type().Field(i).Name, f, v.Field(i).String())
				}
			}
		}
	}
}
