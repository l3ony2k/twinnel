package takeover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/config"
)

// PlanVersion is the schema version of Plan.
const PlanVersion = 1

// Options configure BuildPlan.
type Options struct {
	DiscoverOptions
	// BasePath is an explicit subscription base; default: the best
	// candidate found next to the live config.
	BasePath string
	// NoValidate skips `mihomo -t`.
	NoValidate bool
	Now        func() time.Time
}

// ErrOwnSetup: the setup discovery found is Twinnel's own (Own), so
// there is nothing to take over.
var ErrOwnSetup = errors.New("this mihomo is already managed by Twinnel; there is nothing to take over")

// Coexist describes the managed Tailscale layer the plan uses.
type Coexist struct {
	MagicDNSSuffix string `json:"magicdns_suffix,omitempty"`
	Interface      string `json:"interface,omitempty"`
	Resolver       string `json:"resolver"` // unused when MagicDNSOff
	MagicDNSOff    bool   `json:"magicdns_off,omitempty"`
	Peers          int    `json:"peers"`
	LeakGuard      bool   `json:"leak_guard"` // always false in a takeover plan
}

// Validation is the outcome of mihomo -t on the composed config.
type Validation struct {
	Ran    bool     `json:"ran"`
	OK     bool     `json:"ok"`
	Binary string   `json:"binary,omitempty"`
	Errors []string `json:"errors,omitempty"`
	Skip   string   `json:"skip,omitempty"`
}

// Change is one behavior difference between the live config and the composed
// one. Deliberate changes are intended (e.g. the new MagicDNS resolver);
// anything else needs a human look before applying.
type Change struct {
	Area       string `json:"area"`
	What       string `json:"what"`
	Deliberate bool   `json:"deliberate"`
	Why        string `json:"why,omitempty"`
	// Code and Args name What for a UI that words it in its own language
	// (core/i18n TakeoverChange); WhyCode and WhyArgs do the same for Why.
	// Empty Code: What is data (a config diff line), shown as it is.
	Code    string   `json:"code,omitempty"`
	Args    []string `json:"args,omitempty"`
	WhyCode string   `json:"why_code,omitempty"`
	WhyArgs []string `json:"why_args,omitempty"`
	// Keys are the entries a counts change touched (e.g. the
	// nameserver-policy keys), at most maxChangeKeys, so a UI can show
	// samples and the full list.
	Keys []string `json:"keys,omitempty"`
}

// maxChangeKeys bounds Change.Keys.
const maxChangeKeys = 200

// Change codes (Change.Code). Args are listed after each.
const (
	ChangeCounts         = "counts"           // added, removed, changed (decimal)
	ChangeReordered      = "reordered"        // same items, order matters
	ChangeNamedDiff      = "named-diff"       // only in live, only in composed, changed (comma lists)
	ChangeSubRuleDiffers = "sub-rule-differs" // sub-rule name
	ChangeRulesReordered = "rules-reordered"  // same-target head rules reordered
	ChangeTailscaleRules = "tailscale-rules"  // count, edits ("; " list)
	WhyCoexist           = "coexist"
	WhyCoexistPolicy     = "coexist-nameserver-policy" // resolver
	WhyCoexistFakeIP     = "coexist-fake-ip-filter"
	WhyCoexistHosts      = "coexist-hosts" // peers
	WhyCoexistUseHosts   = "coexist-use-hosts"
	WhyCoexistPrepended  = "coexist-prepended"
)

// Plan is a TakeoverPlan: how Twinnel would manage the existing setup. It holds
// no credentials; layer contents are only referenced by file.
type Plan struct {
	Version    int             `json:"version"`
	CreatedAt  time.Time       `json:"created_at"`
	Discovery  *Discovery      `json:"discovery"`
	Live       string          `json:"live"`
	LiveHash   string          `json:"live_sha256"`
	Base       *Candidate      `json:"base,omitempty"`
	Candidates []Candidate     `json:"candidates"`
	Layers     []DerivedLayer  `json:"layers"`
	Replaced   []Replaced      `json:"replaced_by_coexist,omitempty"`
	Coexist    Coexist         `json:"coexist"`
	Validation Validation      `json:"validation"`
	SemDiff    *config.SemDiff `json:"semdiff,omitempty"`
	// Equivalent: the composed config behaves like the live one apart from
	// the deliberate changes.
	Equivalent bool              `json:"equivalent_except_deliberate"`
	Changes    []Change          `json:"changes,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Files      map[string]string `json:"files,omitempty"` // role → path
	composed   []byte
}

// Composed is the config the plan would deploy (contains credentials).
func (p *Plan) Composed() []byte { return p.composed }

// BuildPlan discovers the setup, picks the base, derives layers and verifies
// them. It changes nothing on the system.
func BuildPlan(ctx context.Context, probe Probe, opt Options) (*Plan, error) {
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	disc, err := Discover(ctx, probe, opt.DiscoverOptions)
	if err != nil {
		return nil, err
	}
	if disc.Mihomo.Twinnel {
		return nil, fmt.Errorf("%w (%s)", ErrOwnSetup, disc.Mihomo.Config)
	}
	p := &Plan{Version: PlanVersion, CreatedAt: now(), Discovery: disc, Live: disc.Mihomo.Config}
	p.Warnings = append(p.Warnings, disc.Warnings...)
	liveData, err := config.ReadConfined(p.Live)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(liveData)
	p.LiveHash = hex.EncodeToString(sum[:])
	live, err := decodeDoc(liveData)
	if err != nil {
		return nil, fmt.Errorf("live config: %w", err)
	}

	// ---- base ----
	paths := FindCandidates(p.Live)
	if opt.BasePath != "" {
		paths = append([]string{opt.BasePath}, removePath(paths, opt.BasePath)...)
	}
	for _, path := range paths {
		c := ScoreCandidate(live, path)
		c.Explicit = opt.BasePath != "" && path == opt.BasePath
		if c.Error == "no proxies section" && !c.Explicit {
			continue
		}
		p.Candidates = append(p.Candidates, c)
	}
	best := ChooseBase(p.Candidates)
	if opt.BasePath != "" {
		best = 0
		if p.Candidates[0].Error != "" {
			return nil, fmt.Errorf("base %s: %s", opt.BasePath, p.Candidates[0].Error)
		}
	}
	if best < 0 {
		return nil, errors.New("no subscription base found next to the live config; pass --base")
	}
	p.Candidates[best].Chosen = true
	b := p.Candidates[best]
	p.Base = &b
	if !b.Explains {
		p.Warnings = append(p.Warnings, "the chosen base does not fully explain the live config (proxies, groups or rule tail differ)")
	}
	baseData, err := config.ReadConfined(b.Path)
	if err != nil {
		return nil, err
	}

	// ---- tailnet ----
	var info config.TailnetInfo
	if ts := disc.Tailscale; ts != nil && ts.status != nil {
		info, err = config.TailnetInfoFromStatus(ts.status)
		if err != nil {
			return nil, err
		}
		info.Interface = ts.Interface
	} else {
		p.Warnings = append(p.Warnings, "Tailscale status unavailable: the coexist layer uses defaults (no peers, no hosts)")
	}
	coexist, err := config.TailscaleCoexistLayer(info)
	if err != nil {
		return nil, err
	}
	p.Coexist = Coexist{MagicDNSSuffix: info.MagicDNSSuffix, Interface: info.Interface,
		Resolver: resolverOf(info), MagicDNSOff: info.MagicDNSOff, Peers: len(info.PeerShortNames)}
	if info.MagicDNSOff && disc.Tailscale != nil && disc.Tailscale.status != nil {
		p.Warnings = append(p.Warnings, "MagicDNS is off on this tailnet: tailnet names are not sent to 100.100.100.100 (it would forward unknown names back to mihomo); peer names are answered from hosts")
	}

	// ---- layers ----
	der, err := DeriveLayers(liveData, baseData, info)
	if err != nil {
		return nil, err
	}
	p.Layers, p.Replaced = der.Layers, der.Replaced
	p.Warnings = append(p.Warnings, der.Warnings...)
	p.Layers = append(p.Layers, DerivedLayer{ID: coexist.ID, Name: coexist.Name, NameCode: LayerNameCoexist, Kind: coexist.Kind,
		Stage: coexist.Stage.String(), stage: coexist.Stage, data: coexist.Override, transform: coexist.Transform})

	var layers []config.Layer
	for _, l := range p.Layers {
		layers = append(layers, l.Layer())
	}
	res, err := config.ComposeReport(baseData, layers)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	for _, w := range res.Warnings {
		p.Warnings = append(p.Warnings, "compose: "+w)
	}
	p.composed = res.Config

	// ---- verify ----
	p.Validation = validate(ctx, disc.Mihomo, res.Config, opt.NoValidate)
	sd, err := config.SemanticDiff(liveData, res.Config)
	if err != nil {
		return nil, err
	}
	p.SemDiff = sd
	ms, err := newManagedSet(info)
	if err != nil {
		return nil, err
	}
	p.Changes = classify(sd, ms, p.Coexist, coexistPrefixEquivalent(liveData, res.Config, coexist.Override))
	p.Equivalent = true
	for _, c := range p.Changes {
		if !c.Deliberate {
			p.Equivalent = false
		}
	}
	return p, nil
}

func resolverOf(info config.TailnetInfo) string {
	r := info.Resolver
	if r == "" {
		r = config.DefaultTailnetResolver
		if info.Interface != "" {
			r += "#" + info.Interface
		}
	}
	return r
}

func removePath(paths []string, p string) []string {
	abs, _ := filepath.Abs(p)
	var out []string
	for _, x := range paths {
		if a, _ := filepath.Abs(x); a != abs {
			out = append(out, x)
		}
	}
	return out
}

func validate(ctx context.Context, m *MihomoSetup, cfg []byte, skip bool) Validation {
	v := Validation{Binary: m.Binary}
	switch {
	case skip:
		v.Skip = "disabled"
		return v
	case m.Binary == "":
		v.Skip = "no mihomo binary found"
		return v
	}
	if _, err := os.Stat(m.Binary); err != nil {
		v.Skip = err.Error()
		return v
	}
	opts := []config.ValidateOption{config.WithGeoDir(m.Dir), config.WithSafePaths(m.Dir)}
	if m.ExternalUI != "" {
		opts = append(opts, config.WithSafePaths(filepath.Dir(m.ExternalUI)))
	}
	res, err := config.Validate(ctx, m.Binary, cfg, opts...)
	v.Ran = true
	if err != nil {
		v.Errors = []string{err.Error()}
		return v
	}
	v.OK = res.OK
	for _, e := range res.Errors {
		v.Errors = append(v.Errors, config.RedactText(e))
	}
	return v
}

const whyCoexist = "managed tailscale-coexist layer"

// classify turns the semantic diff into changes and marks the ones the
// coexist layer makes on purpose.
// coexistPrefixEquivalent reports whether the composed rules route like the
// live rules with the coexist layer's rules put in front. Then every rule
// edit is the coexist layer's doing, even when it makes a live rule
// redundant and so reshapes the same-target blocks DiffRules compares.
func coexistPrefixEquivalent(live, composed, coexistOverride []byte) bool {
	l, err1 := decodeDoc(live)
	c, err2 := decodeDoc(composed)
	var o struct {
		Rules []string `yaml:"+rules"`
	}
	err3 := yaml.Unmarshal(coexistOverride, &o)
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	withPrefix := append(append([]string(nil), o.Rules...), l.rules...)
	// The prefix must not take over a live rule that went elsewhere than
	// DIRECT (e.g. "DOMAIN-SUFFIX,funnel.ts.net,PROXY"): that is a real
	// change the plan has to show as unexpected.
	_, _, dead := config.LiveRules(withPrefix)
	for _, d := range dead {
		if d.Index >= len(o.Rules) && d.CoveredBy >= 0 && d.CoveredBy < len(o.Rules) &&
			config.ParseRule(d.Rule).Target != "DIRECT" {
			return false
		}
	}
	return config.DiffRules(withPrefix, c.rules).Equivalent
}

func classify(sd *config.SemDiff, ms *managedSet, cx Coexist, rulesCoexistOnly bool) []Change {
	var out []Change
	type agg struct {
		added, removed, changed int
		keys                    []string
	}
	groups := map[string]*agg{}
	var order []string
	note := func(area string, c config.SettingChange) {
		kind := c.Kind
		defer func() {
			a := groups[area]
			for _, k := range changeKeys(area, c) {
				if len(a.keys) < maxChangeKeys {
					a.keys = append(a.keys, k)
				}
			}
		}()
		a := groups[area]
		if a == nil {
			a = &agg{}
			groups[area] = a
			order = append(order, area)
		}
		switch kind {
		case "added":
			a.added++
		case "removed":
			a.removed++
		default:
			a.changed++
		}
	}
	for _, c := range sd.EffectiveSettings() {
		// A whole section added or removed ("tun: added {...}") is
		// classified entry by entry: deliberate only if every entry is.
		parts := expandSetting(c)
		var areas []string
		for _, pc := range parts {
			if area, ok := coexistSetting(pc, ms); ok {
				areas = append(areas, area)
			}
		}
		if len(areas) == len(parts) {
			for i, area := range areas {
				note(area, parts[i])
			}
			continue
		}
		out = append(out, Change{Area: c.Path, What: strings.TrimPrefix(c.String(), c.Path+": ")})
	}
	for _, area := range order {
		a := groups[area]
		var parts []string
		if a.added > 0 {
			parts = append(parts, fmt.Sprintf("%d added", a.added))
		}
		if a.removed > 0 {
			parts = append(parts, fmt.Sprintf("%d removed", a.removed))
		}
		if a.changed > 0 {
			parts = append(parts, fmt.Sprintf("%d changed", a.changed))
		}
		why, whyCode, whyArgs := coexistWhy(area, cx)
		out = append(out, Change{Area: area, What: strings.Join(parts, ", "), Deliberate: true,
			Why: why, WhyCode: whyCode, WhyArgs: whyArgs, Code: ChangeCounts,
			Args: []string{strconv.Itoa(a.added), strconv.Itoa(a.removed), strconv.Itoa(a.changed)}, Keys: a.keys})
	}
	named := func(area string, n config.NamedDiff) {
		if n.Same() {
			return
		}
		if n.Reordered && n.OrderMatters {
			out = append(out, Change{Area: area, Code: ChangeReordered,
				What: "same items in a different order; groups with include-all take them in list order, so priority changes"})
			return
		}
		out = append(out, Change{Area: area, What: fmt.Sprintf("only in live %v, only in composed %v, changed %v", n.OnlyA, n.OnlyB, n.Changed),
			Code: ChangeNamedDiff, Args: []string{strings.Join(n.OnlyA, ", "), strings.Join(n.OnlyB, ", "), strings.Join(n.Changed, ", ")}})
	}
	named("proxies", sd.Proxies)
	named("proxy-providers", sd.ProxyProviders)
	named("proxy-groups", sd.ProxyGroups)
	named("rule-providers", sd.RuleProviders)
	for _, s := range sd.SubRules {
		out = append(out, Change{Area: "sub-rules", What: s + " differs", Code: ChangeSubRuleDiffers, Args: []string{s}})
	}
	r := sd.Rules
	if r.Equivalent && r.Reordered {
		out = append(out, Change{Area: "rules", Deliberate: true,
			What: "same-target head rules in a different order; no connection is routed differently",
			Why:  "coexist rules are prepended after user layers",
			Code: ChangeRulesReordered, WhyCode: WhyCoexistPrepended})
	}
	var coexistEdits, other []string
	for _, e := range r.Edits {
		pr := config.ParseRule(e.Rule)
		if ok, _ := ms.ruleExplained(pr); ok || rulesCoexistOnly {
			coexistEdits = append(coexistEdits, e.String())
		} else {
			other = append(other, e.String())
		}
	}
	if len(coexistEdits) > 0 {
		out = append(out, Change{Area: "rules", Deliberate: true,
			What:    fmt.Sprintf("%d Tailscale rule edit(s): %s", len(coexistEdits), strings.Join(firstN(coexistEdits, 6), "; ")),
			Why:     whyCoexist,
			Code:    ChangeTailscaleRules,
			Args:    []string{strconv.Itoa(len(coexistEdits)), strings.Join(firstN(coexistEdits, 6), "; ")},
			WhyCode: WhyCoexist})
	}
	for _, e := range other {
		out = append(out, Change{Area: "rules", What: e})
	}
	sort.SliceStable(out, func(i, j int) bool { return !out[i].Deliberate && out[j].Deliberate })
	return out
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string(nil), s[:n]...), fmt.Sprintf("… %d more", len(s)-n))
}

// coexistSetting reports whether a setting change comes from the coexist
// layer, and the area to group it under.
// changeKeys names the entries one setting change touched in area: the
// key after the area's path, or for a list that changed as a whole (rule
// mode's fake-ip-filter) the entries only one side has.
func changeKeys(area string, c config.SettingChange) []string {
	if k, ok := strings.CutPrefix(c.Path, area+"."); ok && k != "" {
		return []string{k}
	}
	var a, b []string
	if json.Unmarshal([]byte(c.A), &a) != nil {
		a = nil
	}
	if json.Unmarshal([]byte(c.B), &b) != nil {
		b = nil
	}
	if a == nil && b == nil {
		if v := c.B; v != "" && c.Kind != "removed" {
			return []string{strings.Trim(v, `"`)}
		}
		return nil
	}
	var out []string
	for _, x := range b {
		if !slices.Contains(a, x) {
			out = append(out, x)
		}
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func coexistSetting(c config.SettingChange, ms *managedSet) (string, bool) {
	val := c.B
	if c.Kind == "removed" {
		val = c.A
	}
	switch {
	case strings.HasPrefix(c.Path, "dns.nameserver-policy."):
		key := strings.TrimPrefix(c.Path, "dns.nameserver-policy.")
		if ms.policy[key] || ms.tailscaleDomain(key) {
			return "dns.nameserver-policy", true
		}
	case c.Path == "dns.fake-ip-filter" && c.Kind == "changed":
		// Rule mode: the list is ordered, so it changes as a whole. It is
		// the coexist layer's change if both sides are equal once its
		// entries are taken out.
		var a, b []string
		if json.Unmarshal([]byte(c.A), &a) == nil && json.Unmarshal([]byte(c.B), &b) == nil {
			strip := func(l []string) []string {
				var out []string
				for _, x := range l {
					if !ms.filter[x] {
						out = append(out, x)
					}
				}
				return out
			}
			if slices.Equal(strip(a), strip(b)) {
				return c.Path, true
			}
		}
	case c.Path == "dns.fake-ip-filter":
		if allItems(val, func(v string) bool { return ms.filter[v] || ms.tailscaleDomain(v) }) {
			return c.Path, true
		}
	case c.Path == "tun.route-exclude-address":
		if allItems(val, ms.inTailnet) {
			return c.Path, true
		}
	case c.Path == "hosts" || strings.HasPrefix(c.Path, "hosts."):
		key := strings.TrimPrefix(c.Path, "hosts.")
		if c.Path == "hosts" || ms.hosts[strings.ToLower(key)] {
			return "hosts", true
		}
	case c.Path == "dns.use-hosts":
		return c.Path, true
	}
	return "", false
}

// expandSetting splits an added or removed mapping into one change per
// entry, recursively; other changes are returned as is.
func expandSetting(c config.SettingChange) []config.SettingChange {
	val := c.B
	if c.Kind == "removed" {
		val = c.A
	}
	if c.Kind != "added" && c.Kind != "removed" || !strings.HasPrefix(val, "{") {
		return []config.SettingChange{c}
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(val), &m) != nil || len(m) == 0 {
		return []config.SettingChange{c}
	}
	var out []config.SettingChange
	for k, raw := range m {
		v := string(raw)
		var str string
		if json.Unmarshal(raw, &str) == nil {
			v = str
		}
		sub := config.SettingChange{Path: c.Path + "." + k, Kind: c.Kind}
		if c.Kind == "added" {
			sub.B = v
		} else {
			sub.A = v
		}
		out = append(out, expandSetting(sub)...)
	}
	return out
}

// allItems applies ok to a set entry, or to every item when the whole list
// was added or removed (val is then its JSON rendering).
func allItems(val string, ok func(string) bool) bool {
	var items []string
	if strings.HasPrefix(val, "[") && json.Unmarshal([]byte(val), &items) == nil && len(items) > 0 {
		for _, it := range items {
			if !ok(it) {
				return false
			}
		}
		return true
	}
	return ok(val)
}

func coexistWhy(area string, cx Coexist) (why, code string, args []string) {
	switch area {
	case "dns.nameserver-policy":
		return fmt.Sprintf("%s: tailnet names (MagicDNS suffix, single-label names, tailnet reverse zones) resolve via %s instead of looping through the system resolver", whyCoexist, cx.Resolver),
			WhyCoexistPolicy, []string{cx.Resolver}
	case "dns.fake-ip-filter":
		return whyCoexist + ": tailnet names and single-label names get real answers, not fake-ips", WhyCoexistFakeIP, nil
	case "hosts":
		return fmt.Sprintf("%s: peer names answered locally (%d peers)", whyCoexist, cx.Peers), WhyCoexistHosts, []string{strconv.Itoa(cx.Peers)}
	case "dns.use-hosts":
		return whyCoexist + ": needed for the peer hosts", WhyCoexistUseHosts, nil
	}
	return whyCoexist, WhyCoexist, nil
}

// WriteFiles writes the layers, the composed config, the redacted diff and
// plan.json into dir (created 0700). Files holding config content are 0600.
func (p *Plan) WriteFiles(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "layers"), 0o700); err != nil {
		return err
	}
	p.Files = map[string]string{}
	for i := range p.Layers {
		l := &p.Layers[i]
		name := fmt.Sprintf("%02d-%s.yaml", i, l.ID)
		l.File = filepath.Join(dir, "layers", name)
		if err := os.WriteFile(l.File, l.data, 0o600); err != nil {
			return err
		}
	}
	write := func(role, name string, data []byte) error {
		path := filepath.Join(dir, name)
		p.Files[role] = path
		return os.WriteFile(path, data, 0o600)
	}
	if err := write("composed", "composed.yaml", p.composed); err != nil {
		return err
	}
	if p.SemDiff != nil {
		if err := write("semdiff", "semdiff.txt", []byte(p.SemDiff.Text(200))); err != nil {
			return err
		}
	}
	if err := write("summary", "summary.txt", []byte(p.Text())); err != nil {
		return err
	}
	p.Files["plan"] = filepath.Join(dir, "plan.json")
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.Files["plan"], append(b, '\n'), 0o600)
}

// Text is a readable summary. It contains no credentials.
func (p *Plan) Text() string {
	var w strings.Builder
	f := func(format string, a ...any) { fmt.Fprintf(&w, format, a...) }
	f("twinnel takeover plan (%s)\n\n", p.CreatedAt.Format("2006-01-02 15:04"))
	if m := p.Discovery.Mihomo; m != nil {
		f("mihomo\n")
		if m.Service != nil {
			f("  service:     %s (%s, %s)\n", m.Service.Label, m.Service.Kind, m.Service.Path)
		}
		f("  binary:      %s\n", m.Binary)
		f("  version:     %s\n", orDash(m.Version))
		f("  dir / file:  %s / %s\n", m.Dir, m.Config)
		f("  controller:  %s (secret %s, running %v)\n", orDash(m.Controller), setOrEmpty(m.SecretSet), m.Running)
		if m.ExternalUI != "" {
			f("  external-ui: %s (%s)\n", m.ExternalUI, orDash(m.ExternalUIName))
		}
		f("  geo files:   %s\n", orDash(strings.Join(m.GeoFiles, ", ")))
		for _, pr := range m.Profiles {
			f("  profile:     %s → %s (%s)\n", pr.Name, pr.Source, pr.From)
		}
	}
	if t := p.Discovery.Tailscale; t != nil {
		f("tailscale\n")
		f("  install:     %s %s\n", t.Install.Kind, t.Install.Source)
		if t.Service != nil {
			f("  service:     %s (%s)\n", t.Service.Label, t.Service.Path)
		}
		f("  version:     %s, state %s, %d peers\n", orDash(t.Version), orDash(t.BackendState), t.Peers)
		f("  MagicDNS:    %s via %s\n", orDash(t.MagicDNSSuffix), orDash(t.Interface))
	}
	f("\nbase candidates\n")
	for _, c := range p.Candidates {
		mark := " "
		if c.Chosen {
			mark = "*"
		}
		f(" %s %s\n", mark, c)
	}
	f("\nlayers (compose order: base → user → coexist)\n")
	for _, l := range p.Layers {
		desc := ""
		switch {
		case len(l.Keys) > 0:
			desc = "sets " + strings.Join(l.Keys, ", ")
		case l.Rules > 0:
			desc = fmt.Sprintf("+rules ×%d", l.Rules)
		case l.ID == "tailscale-coexist":
			desc = fmt.Sprintf("generated: resolver %s, %d peers", p.Coexist.Resolver, p.Coexist.Peers)
		}
		f("  %-18s %-8s %s — %s\n", l.ID, l.Kind, shorten(l.Name, 60), desc)
		if l.File != "" {
			f("  %-18s file: %s\n", "", l.File)
		}
	}
	f("\nreplaced by the managed tailscale-coexist layer (%d items)\n", len(p.Replaced))
	byPath := map[string][]string{}
	var paths []string
	for _, r := range p.Replaced {
		if _, ok := byPath[r.Path]; !ok {
			paths = append(paths, r.Path)
		}
		byPath[r.Path] = append(byPath[r.Path], r.Item)
	}
	for _, path := range paths {
		f("  %s (%d): %s\n", path, len(byPath[path]), strings.Join(firstN(byPath[path], 5), "; "))
	}
	f("\nverification\n")
	switch v := p.Validation; {
	case !v.Ran:
		f("  mihomo -t:   skipped (%s)\n", v.Skip)
	case v.OK:
		f("  mihomo -t:   ok (%s)\n", v.Binary)
	default:
		f("  mihomo -t:   FAILED: %s\n", strings.Join(v.Errors, "; "))
	}
	if sd := p.SemDiff; sd != nil {
		r := sd.Rules
		f("  semdiff:     settings %d effective change(s); proxies %d/%d %s; groups %d/%d %s\n",
			len(sd.EffectiveSettings()), sd.Proxies.CountA, sd.Proxies.CountB, sameWord(sd.Proxies),
			sd.ProxyGroups.CountA, sd.ProxyGroups.CountB, sameWord(sd.ProxyGroups))
		f("               rules live %d → composed %d; live rules %d → %d; equivalent %v (reordered %v)\n",
			r.CountA, r.CountB, r.LiveA, r.LiveB, r.Equivalent, r.Reordered)
		f("               live has %d duplicate and %d shadowed/redundant rules\n", r.DupA, len(r.DeadA))
	}
	f("  result:      ")
	if p.Equivalent {
		f("composed config behaves like the live one, except the deliberate changes below\n")
	} else {
		f("UNEXPECTED differences, review before applying\n")
	}
	if len(p.Changes) > 0 {
		f("\nchanges\n")
		for _, c := range p.Changes {
			tag := "UNEXPECTED"
			if c.Deliberate {
				tag = "deliberate"
			}
			f("  [%s] %s: %s\n", tag, c.Area, c.What)
			if c.Why != "" {
				f("               %s\n", c.Why)
			}
		}
	}
	if len(p.Warnings) > 0 {
		f("\nwarnings\n")
		for _, x := range p.Warnings {
			f("  - %s\n", config.RedactText(x))
		}
	}
	if len(p.Files) > 0 {
		f("\nfiles\n")
		var roles []string
		for r := range p.Files {
			roles = append(roles, r)
		}
		sort.Strings(roles)
		for _, r := range roles {
			f("  %-9s %s\n", r, p.Files[r])
		}
	}
	return w.String()
}

func sameWord(n config.NamedDiff) string {
	if n.Same() {
		return "same"
	}
	return "DIFFER"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func setOrEmpty(b bool) string {
	if b {
		return "set"
	}
	return "empty"
}
