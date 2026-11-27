package i18n

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Every string field must be filled.
func TestComplete(t *testing.T) {
	checkFilled(t, reflect.ValueOf(zh))
	if zh.ModeName("global") != "全局" || zh.TSState("Running") != "已连接" || zh.Path("derp") != "DERP 中继" {
		t.Error("lookups")
	}
}

// Every language has every text Chinese has: no empty string, the same
// map keys, as many list items, and the same format verbs for the same
// arguments (a %d where Chinese has %s would print %!d(string=...)).
func TestLanguagesComplete(t *testing.T) {
	for _, l := range Langs()[1:] {
		t.Run(l.Tag, func(t *testing.T) {
			compareTables(t, "", reflect.ValueOf(zh), reflect.ValueOf(*l.S))
		})
	}
}

func compareTables(t *testing.T, path string, ref, got reflect.Value) {
	t.Helper()
	switch ref.Kind() {
	case reflect.Struct:
		for i := range ref.NumField() {
			name := ref.Type().Field(i).Name
			if !ref.Type().Field(i).Anonymous {
				name = path + name
			} else {
				name = path
			}
			compareTables(t, name, ref.Field(i), got.Field(i))
		}
	case reflect.String:
		if got.String() == "" {
			t.Errorf("%s: missing", path)
			return
		}
		if a, b := verbs(ref.String()), verbs(got.String()); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: format verbs %v, want %v as in %q", path, b, a, ref.String())
		}
	case reflect.Map:
		for _, k := range ref.MapKeys() {
			v := got.MapIndex(k)
			if !v.IsValid() {
				t.Errorf("%s[%v]: missing", path, k)
				continue
			}
			compareTables(t, fmt.Sprintf("%s[%v]", path, k), ref.MapIndex(k), v)
		}
		for _, k := range got.MapKeys() {
			if !ref.MapIndex(k).IsValid() {
				t.Errorf("%s[%v]: not in zh", path, k)
			}
		}
	case reflect.Slice:
		if ref.Len() != got.Len() {
			t.Errorf("%s: %d items, want %d", path, got.Len(), ref.Len())
			return
		}
		for i := range ref.Len() {
			compareTables(t, fmt.Sprintf("%s[%d]", path, i), ref.Index(i), got.Index(i))
		}
	}
}

// verbs maps each argument a format uses (1-based) to its verb, as
// "%s", "%d", "%.1f"; explicit indexes (%[2]s) count.
func verbs(f string) map[int]string {
	out := map[int]string{}
	arg := 1
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			continue
		}
		j := i + 1
		if j < len(f) && f[j] == '%' {
			i = j
			continue
		}
		if j < len(f) && f[j] == '[' {
			k := strings.IndexByte(f[j:], ']')
			if k < 0 {
				break
			}
			fmt.Sscan(f[j+1:j+k], &arg)
			j += k + 1
		}
		k := j
		for k < len(f) && strings.IndexByte("+-# 0123456789.", f[k]) >= 0 {
			k++
		}
		if k >= len(f) {
			break
		}
		out[arg] = "%" + f[j:k+1]
		arg++
		i = k
	}
	return out
}

func TestVerbs(t *testing.T) {
	got := verbs("%d of %s · %.1f%% %[1]d")
	want := map[int]string{1: "%d", 2: "%s", 3: "%.1f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("verbs = %v", got)
	}
	if v := verbs("%[2]s then %[1]s"); v[1] != "%s" || v[2] != "%s" {
		t.Errorf("indexed verbs = %v", v)
	}
}

func TestResolve(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"zh-Hans-CN", "en-US"}, "zh-Hans"},
		{[]string{"zh-Hant-TW"}, "zh-Hans"},
		{[]string{"en-GB", "zh-Hans"}, "en"},
		{[]string{"de-DE", "zh-Hans"}, "zh-Hans"},
		{[]string{"ja-JP"}, "en"},
		{nil, "en"},
	} {
		if got := Resolve(c.in); got != c.want {
			t.Errorf("Resolve(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestSet(t *testing.T) {
	defer Set("zh-Hans")
	Set("en-US")
	if T != &en || Cur() != &en || Tag() != "en" || T.PageHealth != "Health" {
		t.Errorf("Set(en): T=%p Cur=%p Tag=%s", T, Cur(), Tag())
	}
	Set("zh-Hant")
	if T != &zh || Tag() != "zh-Hans" {
		t.Error("Set(zh-Hant)")
	}
	if For("xx") != &zh {
		t.Error("unknown tag")
	}
}

// Each language's own name is unique and not empty.
func TestLangNames(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range Langs() {
		if l.Name == "" || seen[l.Name] || l.S == nil {
			t.Errorf("language %s", l.Tag)
		}
		seen[l.Name] = true
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
