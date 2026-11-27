package engine

import (
	"fmt"
	"path/filepath"

	"github.com/l3ony2k/twinnel/core/config"
	"gopkg.in/yaml.v3"
)

// relocatePaths rewrites absolute paths that point into oldDir (mihomo's
// previous -d dir) into paths relative to the config dir, so a config that
// was written for a dir in the user's home keeps working, unchanged in
// meaning, once mihomo runs from the root-owned dir (service.harden). The
// keys are the ones mihomo resolves against its home dir: external-ui and
// the path of proxy and rule providers. Paths that are already relative,
// or absolute elsewhere, are left alone (mihomo -t refuses the latter
// unless they are in SAFE_PATHS).
//
// It returns the config (re-encoded only when something changed) and one
// line per rewrite.
func relocatePaths(cfg []byte, oldDir string) ([]byte, []string, error) {
	if oldDir == "" {
		return cfg, nil, nil
	}
	oldDir = filepath.Clean(oldDir)
	root, err := config.Parse(cfg)
	if err != nil {
		return nil, nil, err
	}
	var changes []string
	rewrite := func(where string, v *yaml.Node) {
		if v == nil || v.Kind != yaml.ScalarNode || !filepath.IsAbs(v.Value) {
			return
		}
		rel, err := filepath.Rel(oldDir, filepath.Clean(v.Value))
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			return
		}
		rel = filepath.ToSlash(rel)
		changes = append(changes, fmt.Sprintf("%s: %s → %s", where, v.Value, rel))
		v.Value, v.Tag, v.Style = rel, "!!str", 0
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		switch k {
		case "external-ui":
			rewrite(k, v)
		case "proxy-providers", "rule-providers":
			if v.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(v.Content); j += 2 {
				name, p := v.Content[j].Value, v.Content[j+1]
				if p.Kind != yaml.MappingNode {
					continue
				}
				for m := 0; m+1 < len(p.Content); m += 2 {
					if p.Content[m].Value == "path" {
						rewrite(k+"."+name+".path", p.Content[m+1])
					}
				}
			}
		}
	}
	if len(changes) == 0 {
		return cfg, nil, nil
	}
	out, err := config.Encode(root)
	return out, changes, err
}

// relocatedValues lists the dir-relative paths a config makes mihomo use:
// external-ui and provider paths that are local (inside the config dir).
// service.harden copies them into the new config dir.
func relocatedValues(cfg []byte) []string {
	root, err := config.Parse(cfg)
	if err != nil {
		return nil
	}
	var out []string
	add := func(v *yaml.Node) {
		if v != nil && v.Kind == yaml.ScalarNode && v.Value != "" && !filepath.IsAbs(v.Value) && filepath.IsLocal(filepath.FromSlash(v.Value)) {
			out = append(out, filepath.Clean(filepath.FromSlash(v.Value)))
		}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		switch k {
		case "external-ui":
			add(v)
		case "proxy-providers", "rule-providers":
			if v.Kind != yaml.MappingNode {
				continue
			}
			for j := 1; j < len(v.Content); j += 2 {
				p := v.Content[j]
				if p.Kind != yaml.MappingNode {
					continue
				}
				for m := 0; m+1 < len(p.Content); m += 2 {
					if p.Content[m].Value == "path" {
						add(p.Content[m+1])
					}
				}
			}
		}
	}
	return out
}
