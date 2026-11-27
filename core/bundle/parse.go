package bundle

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/store"
	"gopkg.in/yaml.v3"
)

// SourceConfig is a whole mihomo/Clash config (proxies and groups): it
// becomes a subscription with that content and no URL.
const SourceConfig = "config"

// parseURLList reads subscription URLs, one per line. A line is a URL, or
// a name then the URL ("name https://…" or "name,https://…"); a URL's
// #fragment names it too, as share links do. Blank lines and # comments
// are skipped. ok is false when any other line is in the text.
func parseURLList(text string) ([]Subscription, bool) {
	var out []Subscription
	taken := map[string]bool{}
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		name, raw := "", line
		if i := urlStart(line); i > 0 {
			name, raw = line[:i], line[i:]
		}
		if name != "" {
			name = strings.TrimSpace(strings.TrimRight(name, " \t,=|"))
			if name == "" || strings.ContainsAny(raw, " \t") {
				return nil, false
			}
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, false
		}
		if name == "" && u.Fragment != "" {
			name = u.Fragment
		}
		if name == "" {
			name = u.Hostname()
		}
		id := uniqueID(Slug(name, "sub"), taken)
		out = append(out, Subscription{ID: id, Name: name, Active: len(out) == 0, IntervalMinutes: 1440, URL: raw})
	}
	return out, len(out) > 0
}

// urlStart is where the first http:// or https:// starts in line, or -1.
func urlStart(line string) int {
	lower := strings.ToLower(line)
	i, j := strings.Index(lower, "http://"), strings.Index(lower, "https://")
	switch {
	case i < 0:
		return j
	case j < 0:
		return i
	}
	return min(i, j)
}

// parseOverride reads one YAML file: a whole config becomes a subscription
// with that content; anything else is one override layer named after the
// file.
func parseOverride(name string, data []byte) (*Bundle, error) {
	root, err := config.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("not a subscription URL list, and not YAML: %v", err)
	}
	if len(root.Content) == 0 {
		return nil, errors.New("the file has nothing in it to import")
	}
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if name == "" || base == "." || base == "" {
		base = "imported"
	}
	if isWholeConfig(root) {
		return &Bundle{Source: SourceConfig, Subscriptions: []Subscription{{
			ID: Slug(base, "imported"), Name: base, Active: true, Content: string(data),
		}}}, nil
	}
	return &Bundle{Source: SourceOverride, Layers: []Layer{{
		ID: Slug(base, "imported"), Name: base, Stage: store.StageUser, Format: DetectFormat(data),
		Enabled: true, Content: string(data),
	}}}, nil
}

// isWholeConfig: plain (unmarked) proxies or proxy-providers next to
// proxy-groups is a config, not a patch on one.
func isWholeConfig(root *yaml.Node) bool {
	var nodes, groups bool
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "proxies", "proxy-providers":
			nodes = true
		case "proxy-groups":
			groups = true
		}
	}
	return nodes && groups
}

// Slug turns a name into a layer or subscription id ([a-z0-9][a-z0-9_-]*,
// at most 64), or fallback when nothing is left.
func Slug(name, fallback string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '_' && b.Len() > 0:
			b.WriteRune(r)
			dash = false
		case r == '-' || unicode.IsSpace(r) || r == '.':
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-_")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-_")
	}
	if !store.ValidID(s) {
		return fallback
	}
	return s
}

// uniqueID returns id, or id-2, id-3, … when taken, and marks it taken.
func uniqueID(id string, taken map[string]bool) string {
	out := id
	for n := 2; taken[out]; n++ {
		suffix := fmt.Sprintf("-%d", n)
		out = strings.TrimRight(id[:min(len(id), 64-len(suffix))], "-_") + suffix
	}
	taken[out] = true
	return out
}

// FreeID returns id with a -N suffix that is not taken (taken(id) reports
// an id in use). Renaming a conflicting item uses it.
func FreeID(id string, taken func(string) bool) string {
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("-%d", n)
		out := strings.TrimRight(id[:min(len(id), 64-len(suffix))], "-_") + suffix
		if !taken(out) {
			return out
		}
	}
}
