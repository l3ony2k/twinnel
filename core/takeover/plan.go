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
	"sort"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/config"
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

// Coexist describes the managed Tailscale layer the plan uses.
type Coexist struct {
	MagicDNSSuffix string `json:"magicdns_suffix,omitempty"`
	Interface      string `json:"interface,omitempty"`
	Resolver       string `json:"resolver"`
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
}

// Plan is a TakeoverPlan: how duet would manage the existing setup. It holds
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
	p := &Plan{Version: PlanVersion, CreatedAt: now(), Discovery: disc, Live: disc.Mihomo.Config}
	p.Warnings = append(p.Warnings, disc.Warnings...)
	liveData, err := os.ReadFile(p.Live)
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
	baseData, err := os.ReadFile(b.Path)
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
		Resolver: resolverOf(info), Peers: len(info.PeerShortNames)}

	// ---- layers ----
	der, err := DeriveLayers(liveData, baseData, info)
	if err != nil {
		return nil, err
	}
	p.Layers, p.Replaced = der.Layers, der.Replaced
	p.Warnings = append(p.Warnings, der.Warnings...)
	p.Layers = append(p.Layers, DerivedLayer{ID: coexist.ID, Name: coexist.Name, Kind: coexist.Kind,
		Stage: coexist.Stage.String(), stage: coexist.Stage, data: coexist.Override})

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
	p.Changes = classify(sd, ms, p.Coexist)
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
func classify(sd *config.SemDiff, ms *managedSet, cx Coexist) []Change {
	var out []Change
	type agg struct{ added, removed, changed int }
	groups := map[string]*agg{}
	var order []string
	note := func(area string, kind string) {
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
		if area, ok := coexistSetting(c, ms); ok {
			note(area, c.Kind)
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
		out = append(out, Change{Area: area, What: strings.Join(parts, ", "), Deliberate: true,
			Why: coexistWhy(area, cx)})
	}
	named := func(area string, n config.NamedDiff) {
		if n.Same() {
			return
		}
		out = append(out, Change{Area: area, What: fmt.Sprintf("only in live %v, only in composed %v, changed %v", n.OnlyA, n.OnlyB, n.Changed)})
	}
	named("proxies", sd.Proxies)
	named("proxy-providers", sd.ProxyProviders)
	named("proxy-groups", sd.ProxyGroups)
	named("rule-providers", sd.RuleProviders)
	for _, s := range sd.SubRules {
		out = append(out, Change{Area: "sub-rules", What: s + " differs"})
	}
	r := sd.Rules
	if r.Equivalent && r.Reordered {
		out = append(out, Change{Area: "rules", Deliberate: true,
			What: "same-target head rules in a different order; no connection is routed differently",
			Why:  "coexist rules are prepended after user layers"})
	}
	var coexistEdits, other []string
	for _, e := range r.Edits {
		pr := config.ParseRule(e.Rule)
		if ok, _ := ms.ruleExplained(pr); ok {
			coexistEdits = append(coexistEdits, e.String())
		} else {
			other = append(other, e.String())
		}
	}
	if len(coexistEdits) > 0 {
		out = append(out, Change{Area: "rules", Deliberate: true,
			What: fmt.Sprintf("%d Tailscale rule edit(s): %s", len(coexistEdits), strings.Join(firstN(coexistEdits, 6), "; ")),
			Why:  whyCoexist})
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
	case c.Path == "dns.fake-ip-filter":
		if ms.filter[val] || ms.tailscaleDomain(val) {
			return c.Path, true
		}
	case c.Path == "tun.route-exclude-address":
		if ms.inTailnet(val) {
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

func coexistWhy(area string, cx Coexist) string {
	switch area {
	case "dns.nameserver-policy":
		return fmt.Sprintf("%s: tailnet names (MagicDNS suffix, single-label names, tailnet reverse zones) resolve via %s instead of looping through the system resolver", whyCoexist, cx.Resolver)
	case "dns.fake-ip-filter":
		return whyCoexist + ": tailnet names and single-label names get real answers, not fake-ips"
	case "hosts":
		return fmt.Sprintf("%s: peer names answered locally (%d peers)", whyCoexist, cx.Peers)
	case "dns.use-hosts":
		return whyCoexist + ": needed for the peer hosts"
	}
	return whyCoexist
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
	f("duet takeover plan (%s)\n\n", p.CreatedAt.Format("2006-01-02 15:04"))
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
