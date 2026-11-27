package takeover

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/config"
)

func redactURL(s string) string { return config.RedactURL(s, true) }

// Candidate is a file that may be the subscription the live config was built
// from, with how well it explains the live config.
type Candidate struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// Proxies / groups of the candidate found unchanged in the live config.
	ProxiesSame  int `json:"proxies_same"`
	ProxiesTotal int `json:"proxies_total"`
	GroupsSame   int `json:"groups_same"`
	GroupsTotal  int `json:"groups_total"`
	// Rules: the candidate's rules, how many of them form a common tail with
	// the live rules, and how many live rules are in front of that tail.
	Rules      int `json:"rules"`
	CommonTail int `json:"common_tail"`
	HeadRules  int `json:"head_rules"`
	// SettingsDiffer counts top-level settings that differ from live.
	SettingsDiffer int `json:"settings_differ"`
	// Explains: all proxies, groups, rules, providers and sub-rules carried
	// over unchanged.
	Explains bool   `json:"explains"`
	Chosen   bool   `json:"chosen,omitempty"`
	Explicit bool   `json:"explicit,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (c Candidate) String() string {
	if c.Error != "" {
		return fmt.Sprintf("%s: %s", c.Path, c.Error)
	}
	return fmt.Sprintf("%s: proxies %d/%d, groups %d/%d, rules tail %d/%d (+%d head), %d settings differ",
		c.Path, c.ProxiesSame, c.ProxiesTotal, c.GroupsSame, c.GroupsTotal,
		c.CommonTail, c.Rules, c.HeadRules, c.SettingsDiffer)
}

// doc is a decoded config.
type doc struct {
	top     map[string]any
	rules   []string
	proxies map[string]string // name → content hash
	groups  map[string]any
	// other is a content hash of the data sections DeriveLayers does not
	// carry over (providers, sub-rules): the base must have them as is.
	other string
}

func decodeDoc(data []byte) (*doc, error) {
	root, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	var top map[string]any
	if err := root.Decode(&top); err != nil {
		return nil, err
	}
	d := &doc{top: top, proxies: map[string]string{}, groups: map[string]any{}}
	if top == nil {
		d.top = map[string]any{}
	}
	for _, r := range asList(d.top["rules"]) {
		d.rules = append(d.rules, fmt.Sprint(r))
	}
	for _, p := range asList(d.top["proxies"]) {
		if m, ok := p.(map[string]any); ok {
			d.proxies[fmt.Sprint(m["name"])] = config.ContentHash(m)
		}
	}
	d.other = config.ContentHash([]any{d.top["proxy-providers"], d.top["rule-providers"], d.top["sub-rules"]})
	for _, g := range asList(d.top["proxy-groups"]) {
		if m, ok := g.(map[string]any); ok {
			d.groups[fmt.Sprint(m["name"])] = m
		}
	}
	return d, nil
}

func asList(v any) []any { l, _ := v.([]any); return l }

// looksLikeConfig is a cheap filter before parsing a file.
func looksLikeConfig(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "\nproxies:") || strings.HasPrefix(s, "proxies:") ||
		strings.Contains(s, "\nproxy-providers:") || strings.HasPrefix(s, "proxy-providers:")
}

// maxCandidateSize skips files that can't be configs.
const maxCandidateSize = 64 << 20

// FindCandidates lists files near the live config that may be its base.
func FindCandidates(live string) []string {
	liveAbs, _ := filepath.Abs(live)
	var out []string
	for _, dir := range searchDirs(filepath.Dir(live)) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if e.IsDir() || !(strings.Contains(n, ".yaml") || strings.Contains(n, ".yml")) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if abs, _ := filepath.Abs(p); abs == liveAbs {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// ScoreCandidate measures how well the file at path explains live.
func ScoreCandidate(live *doc, path string) Candidate {
	c := Candidate{Path: path}
	fi, err := os.Lstat(path)
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.Size, c.ModTime = fi.Size(), fi.ModTime()
	if fi.Size() > maxCandidateSize {
		c.Error = "too large"
		return c
	}
	// Confined to the file's dir: a symlink out of it is refused.
	b, err := config.ReadConfined(path)
	if err != nil {
		c.Error = err.Error()
		return c
	}
	if !looksLikeConfig(b) {
		c.Error = "no proxies section"
		return c
	}
	d, err := decodeDoc(b)
	if err != nil {
		c.Error = "parse: " + err.Error()
		return c
	}
	scoreDoc(live, d, &c)
	return c
}

func scoreDoc(live, d *doc, c *Candidate) {
	c.ProxiesTotal = len(d.proxies)
	for n, h := range d.proxies {
		if live.proxies[n] == h {
			c.ProxiesSame++
		}
	}
	c.GroupsTotal = len(d.groups)
	for n, g := range d.groups {
		if reflect.DeepEqual(live.groups[n], g) {
			c.GroupsSame++
		}
	}
	c.Rules = len(d.rules)
	c.CommonTail = commonTail(live.rules, d.rules)
	c.HeadRules = len(live.rules) - c.CommonTail
	for _, k := range unionKeys(live.top, d.top) {
		if dataKey[k] {
			continue
		}
		if !reflect.DeepEqual(live.top[k], d.top[k]) {
			c.SettingsDiffer++
		}
	}
	c.Explains = c.ProxiesTotal > 0 && c.ProxiesSame == c.ProxiesTotal &&
		len(live.proxies) == c.ProxiesTotal &&
		c.GroupsSame == c.GroupsTotal && len(live.groups) == c.GroupsTotal &&
		c.CommonTail == c.Rules && live.other == d.other
}

func commonTail(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && strings.TrimSpace(a[len(a)-1-n]) == strings.TrimSpace(b[len(b)-1-n]) {
		n++
	}
	return n
}

// ChooseBase ranks candidates and marks the best one. A candidate that
// explains the live config (same proxies and groups, its rules a tail of the
// live rules) wins over one that doesn't; among those, the one closest to a
// raw subscription wins: fewest own rules (so the hand-added head rules
// become layers), then most settings differing (the hand-written header
// becomes the base layer), then the oldest file.
func ChooseBase(cands []Candidate) (best int) {
	best = -1
	better := func(a, b Candidate) bool {
		if a.Explains != b.Explains {
			return a.Explains
		}
		if !a.Explains {
			sa := a.ProxiesSame + a.GroupsSame + a.CommonTail
			sb := b.ProxiesSame + b.GroupsSame + b.CommonTail
			if sa != sb {
				return sa > sb
			}
		}
		if a.Rules != b.Rules {
			return a.Rules < b.Rules
		}
		if a.SettingsDiffer != b.SettingsDiffer {
			return a.SettingsDiffer > b.SettingsDiffer
		}
		return a.ModTime.Before(b.ModTime)
	}
	for i, c := range cands {
		if c.Error != "" || c.ProxiesTotal == 0 {
			continue
		}
		if best < 0 || better(c, cands[best]) {
			best = i
		}
	}
	return best
}

func unionKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

var dataKey = map[string]bool{
	"proxies": true, "proxy-providers": true, "proxy-groups": true,
	"rule-providers": true, "sub-rules": true, "rules": true,
}

// ruleNodes returns the rules sequence items of a parsed root (for comments).
func ruleNodes(root *yaml.Node) []*yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "rules" && root.Content[i+1].Kind == yaml.SequenceNode {
			return root.Content[i+1].Content
		}
	}
	return nil
}
