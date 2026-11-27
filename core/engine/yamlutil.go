package engine

import (
	"regexp"
	"slices"
	"strconv"

	"github.com/l3ony2k/twinnel/core/config"
	"gopkg.in/yaml.v3"
)

// The controller secret must not be stored in history or logs. Composed
// configs are kept without it; it is put back only into the bytes written
// to the live config file.

// topLevel returns the scalar value of a top-level key, if present.
func topLevel(cfg []byte, key string) (string, bool) {
	root, err := config.Parse(cfg)
	if err != nil {
		return "", false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.ScalarNode {
			return root.Content[i+1].Value, true
		}
	}
	return "", false
}

// topLevels is topLevel for several keys with one parse: the scalar
// values found, by key.
func topLevels(cfg []byte, keys ...string) map[string]string {
	out := map[string]string{}
	root, err := config.Parse(cfg)
	if err != nil {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i].Value
		if root.Content[i+1].Kind == yaml.ScalarNode && slices.Contains(keys, k) {
			if _, seen := out[k]; !seen {
				out[k] = root.Content[i+1].Value
			}
		}
	}
	return out
}

// withoutSecret removes the top-level secret from an encoded config. The
// rest of the document is re-encoded (configs produced by Compose round-trip
// unchanged).
func withoutSecret(cfg []byte) ([]byte, string, error) {
	root, err := config.Parse(cfg)
	if err != nil {
		return nil, "", err
	}
	secret := ""
	found := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "secret" {
			secret = root.Content[i+1].Value
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			found = true
			break
		}
	}
	if !found {
		return cfg, "", nil
	}
	out, err := config.Encode(root)
	return out, secret, err
}

// withSecret sets the top-level secret, placing it after
// external-controller when that key exists.
func withSecret(cfg []byte, secret string) ([]byte, error) {
	if secret == "" {
		return cfg, nil
	}
	root, err := config.Parse(cfg)
	if err != nil {
		return nil, err
	}
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: secret}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "secret"}
	at := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "secret":
			root.Content[i+1] = val
			return config.Encode(root)
		case "external-controller":
			at = i + 2
		}
	}
	if at < 0 {
		root.Content = append(root.Content, key, val)
	} else {
		root.Content = append(root.Content[:at], append([]*yaml.Node{key, val}, root.Content[at:]...)...)
	}
	return config.Encode(root)
}

// secretLineRE matches a top-level secret line in a file the helper did not
// write; used to strip it from backups without re-encoding the file.
var secretLineRE = regexp.MustCompile(`(?m)^secret:[^\n]*(\n|$)`)

// stripSecretBytes removes top-level secret lines, keeping all other bytes.
func stripSecretBytes(b []byte) []byte {
	return secretLineRE.ReplaceAll(b, nil)
}

// mixedPort reads mixed-port (or port) from a config.
func mixedPort(cfg []byte) int {
	for _, k := range []string{"mixed-port", "port"} {
		if v, ok := topLevel(cfg, k); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
				return n
			}
		}
	}
	return 0
}

// DashboardUIDir is the external-ui Twinnel adds when the person installs
// the dashboard from Twinnel and nothing else sets external-ui.
const DashboardUIDir = "ui"

// withExternalUI adds `external-ui: ui` after external-controller unless
// the config already sets external-ui.
func withExternalUI(cfg []byte) ([]byte, error) {
	root, err := config.Parse(cfg)
	if err != nil {
		return nil, err
	}
	at := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "external-ui":
			return cfg, nil
		case "external-controller", "secret":
			at = i + 2
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "external-ui"}
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: DashboardUIDir}
	if at < 0 {
		root.Content = append(root.Content, key, val)
	} else {
		root.Content = append(root.Content[:at], append([]*yaml.Node{key, val}, root.Content[at:]...)...)
	}
	return config.Encode(root)
}
