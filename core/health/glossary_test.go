package health

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// TestPlatformGlossary: no health text a system shows names another
// system's thing (core/i18n/glossary.go). Fix texts are per system by
// key ("reason@platform" wins over the plain key, FixTextFor); the
// texts table's fields are shown everywhere unless textsShownOn says.
func TestPlatformGlossary(t *testing.T) {
	var bad []string
	check := func(goos, where, s string) {
		if f := i18n.ForeignTerms(goos, s); len(f) > 0 {
			bad = append(bad, goos+" "+where+" ["+strings.Join(f, ", ")+"]: "+s)
		}
	}
	for tag, tbl := range fixTextTables {
		for key := range tbl {
			reason, plat, _ := strings.Cut(key, "@")
			for _, goos := range i18n.Platforms {
				if plat != "" && plat != goos || plat == "" && hasOverride(tbl, reason, goos) || !reasonShownOn(reason, goos) {
					continue
				}
				ft, _ := FixTextFor(tag, goos, reason)
				check(goos, tag+" fix "+reason, ft.Plain)
				check(goos, tag+" fix "+reason, ft.How)
			}
		}
	}
	for tag, L := range textTables {
		v := reflect.ValueOf(*L)
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			for _, goos := range i18n.Platforms {
				if only, ok := textsShownOn[name]; ok && !contains(only, goos) {
					continue
				}
				check(goos, tag+" texts."+name, v.Field(i).String())
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

func hasOverride(tbl map[string]FixText, reason, goos string) bool {
	_, ok := tbl[reason+"@"+goos]
	return ok
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// reasonShownOn: a reason is shown on the systems its check runs on.
func reasonShownOn(reason, goos string) bool {
	only, ok := reasonsShownOn[reason]
	return !ok || contains(only, goos)
}

// reasonsShownOn are reasons only some systems' checks give.
var reasonsShownOn = map[string][]string{
	// The VPN list (with bundle IDs) is macOS's: Tailscale's network
	// extension is a macOS thing.
	ReasonVPNTailscale: {Darwin},
}

// textsShownOn are texts fields only some systems show.
var textsShownOn = map[string][]string{
	"fixDNS": {Darwin}, "fixDNSWindows": {Windows}, "fixDNSLinux": {Linux},
}
