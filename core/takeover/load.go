package takeover

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/l3ony2k/duet/core/config"
)

// LoadPlan reads a plan.json written by WriteFiles, together with its layer
// files (so Layer and Data work again) and the composed config.
func LoadPlan(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	if err := json.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.Version != PlanVersion {
		return nil, fmt.Errorf("%s: plan version %d, want %d", path, p.Version, PlanVersion)
	}
	for i := range p.Layers {
		l := &p.Layers[i]
		switch l.Stage {
		case config.StageBase.String():
			l.stage = config.StageBase
		case config.StageUser.String():
			l.stage = config.StageUser
		case config.StageCoexist.String():
			l.stage = config.StageCoexist
		default:
			return nil, fmt.Errorf("layer %s: unknown stage %q", l.ID, l.Stage)
		}
		if l.File == "" {
			return nil, fmt.Errorf("layer %s: no file", l.ID)
		}
		if l.data, err = os.ReadFile(l.File); err != nil {
			return nil, fmt.Errorf("layer %s: %w", l.ID, err)
		}
	}
	if f := p.Files["composed"]; f != "" {
		if p.composed, err = os.ReadFile(f); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// ProfileURL returns the unredacted subscription URL of a discovered
// profile, read again from its index file. The URL is a secret: keep it in
// the secret store, never in plans or logs.
func ProfileURL(pr Profile) (string, error) {
	b, err := config.ReadConfined(pr.From)
	if err != nil {
		return "", err
	}
	var items []map[string]any
	if err := json.Unmarshal(b, &items); err != nil {
		return "", fmt.Errorf("%s: %w", pr.From, err)
	}
	for _, it := range items {
		name, _ := it["name"].(string)
		if name != pr.Name {
			continue
		}
		src, _ := it["source"].(string)
		if src == "" {
			src, _ = it["url"].(string)
		}
		if strings.Contains(src, "://") && redactURL(src) == pr.Source {
			return src, nil
		}
	}
	return "", errors.New("profile " + pr.Name + " is no longer in " + pr.From)
}
