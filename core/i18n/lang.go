package i18n

import (
	"strings"
	"sync/atomic"
)

// Lang is one language the UI is written in.
type Lang struct {
	Tag  string   // BCP 47, e.g. "zh-Hans", "en"
	Name string   // the language's name in itself, as a language menu shows it
	S    *Strings // its texts
}

// langs are the UI languages. Chinese is first: it is the reference every
// other table is tested against (TestLanguagesComplete). To add a
// language, write its Strings in a file of its own (as en.go does) and
// list it here.
var langs = []Lang{
	{Tag: "zh-Hans", Name: "简体中文", S: &zh},
	{Tag: "en", Name: "English", S: &en},
}

// Fallback is the language shown when none of the user's languages is one
// the UI is written in.
const Fallback = "en"

// Langs returns the UI languages, the reference (Chinese) first.
func Langs() []Lang { return append([]Lang(nil), langs...) }

// Match returns the UI language for a BCP 47 tag, by its primary
// language subtag ("zh-Hant-TW" → "zh-Hans", "en-GB" → "en"), or "".
func Match(tag string) string {
	base, _, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"), "-")
	base = strings.ToLower(base)
	if base == "" {
		return ""
	}
	for _, l := range langs {
		b, _, _ := strings.Cut(l.Tag, "-")
		if strings.EqualFold(b, base) {
			return l.Tag
		}
	}
	return ""
}

// Resolve returns the UI language for the user's preferred languages, most
// preferred first: the first one the UI is written in, else Fallback.
func Resolve(preferred []string) string {
	for _, p := range preferred {
		if m := Match(p); m != "" {
			return m
		}
	}
	return Fallback
}

// For returns the texts of a language tag; an unknown tag gets Chinese.
func For(tag string) *Strings {
	if m := Match(tag); m != "" {
		for _, l := range langs {
			if l.Tag == m {
				return l.S
			}
		}
	}
	return &zh
}

// T is the current language's texts, for code on the app's main thread
// (views, menus, tray), which is where Set changes it. Code on other
// goroutines reads Cur.
var T = For("zh")

var cur atomic.Pointer[Strings]

func init() { cur.Store(T) }

// Cur returns the current language's texts; safe on any goroutine.
func Cur() *Strings { return cur.Load() }

// Set makes tag's language the current one (For). Call it on the app's
// main thread: T is a plain variable there.
func Set(tag string) {
	T = For(tag)
	cur.Store(T)
}

// Tag returns the tag of the current language.
func Tag() string {
	s := Cur()
	for _, l := range langs {
		if l.S == s {
			return l.Tag
		}
	}
	return langs[0].Tag
}
