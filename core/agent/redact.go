package agent

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/config"
)

// Redaction. Every tool output and error passes through redactJSON or
// redactString; config and override texts pass through RedactYAML first.
// What never leaves: the controller secret (and any string in
// Server.secrets), values under credential keys (password, uuid, token,
// private-key, ...), user info and query strings of URLs, and the URLs of
// proxy providers (subscription URLs carry their token in the path).

const redacted = "<redacted>"

// extraSensitiveRE adds keys config.IsSensitiveKey does not cover but node
// definitions use for credentials.
var extraSensitiveRE = regexp.MustCompile(`(?i)^(auth|short-id|username|user|key|cert|ca|ca-str|fingerprint|client-fingerprint-key)$`)

func sensitiveKey(k string) bool {
	return config.IsSensitiveKey(k) || extraSensitiveRE.MatchString(k)
}

// safeJSONKeys are Twinnel's own JSON fields whose names look sensitive but
// hold no secret (IDs, counts, booleans, key names).
var safeJSONKeys = map[string]bool{
	"active_subscription": true, "subscription": true, "subscriptions": true,
	"secret_set": true, "has_url": true, "subscription_url": true,
	"secret_keys": true, "tokens": true,
}

// redactString hides known secrets and URL credentials in free text.
func (s *Server) redactString(x string) string {
	for _, sec := range s.knownSecrets() {
		x = strings.ReplaceAll(x, sec, redacted)
	}
	return redactURLTokens(config.RedactText(x))
}

var urlRE = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)

// redactURLTokens hides URL path segments that look like tokens: a
// subscription URL often carries its key in the path, and the helper's
// texts or mihomo's log may quote one. A segment is token-like when it is
// long (16+ characters of letters, digits, - and _) or mixes upper case,
// lower case and digits in 8+ characters.
func redactURLTokens(x string) string {
	return urlRE.ReplaceAllStringFunc(x, func(u string) string {
		i := strings.Index(u, "://")
		rest := u[i+3:]
		j := strings.IndexByte(rest, '/')
		if j < 0 {
			return u
		}
		segs := strings.Split(rest[j:], "/")
		for k, seg := range segs {
			tail := ""
			if q := strings.IndexAny(seg, "?#"); q >= 0 {
				seg, tail = seg[:q], seg[q:]
			}
			if tokenLike(seg) {
				segs[k] = redacted + tail
			}
		}
		return u[:i+3] + rest[:j] + strings.Join(segs, "/")
	})
}

func tokenLike(seg string) bool {
	var up, low, dig, other bool
	for _, r := range seg {
		switch {
		case r >= 'A' && r <= 'Z':
			up = true
		case r >= 'a' && r <= 'z':
			low = true
		case r >= '0' && r <= '9':
			dig = true
		case r == '-' || r == '_':
		default:
			other = true
		}
	}
	if other {
		return false
	}
	return len(seg) >= 16 && dig || len(seg) >= 8 && up && low && dig
}

// redactJSON marshals v and redacts it value by value.
func (s *Server) redactJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var g any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&g); err != nil {
		return nil, err
	}
	g = s.redactValue("", g)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(g); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (s *Server) redactValue(key string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = s.redactValue(k, x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = s.redactValue(key, x)
		}
		return t
	case string:
		if t != "" && key != "" && !safeJSONKeys[key] && sensitiveKey(key) {
			return redacted
		}
		return s.redactString(t)
	}
	return v
}

// RedactYAML hides credentials in a mihomo config or override text: values
// under credential keys, the URLs of proxy providers, and URL user info or
// queries anywhere. Text that does not parse is redacted line by line.
func RedactYAML(text string) string {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(text), &root); err != nil || len(root.Content) == 0 {
		return redactLines(text)
	}
	redactNode(&root, nil)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return redactLines(text)
	}
	return buf.String()
}

func redactNode(n *yaml.Node, path []string) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			redactNode(c, path)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			p := append(path[:len(path):len(path)], k.Value)
			if sensitiveKey(k.Value) {
				blank(v)
				continue
			}
			if k.Value == "url" && inPath(path, "proxy-providers") && v.Kind == yaml.ScalarNode {
				v.Value, v.Style, v.Tag = config.RedactURL(v.Value, true), 0, "!!str"
				continue
			}
			redactNode(v, p)
		}
	case yaml.ScalarNode:
		if strings.Contains(n.Value, "://") {
			n.Value = config.RedactText(n.Value)
		}
	case yaml.AliasNode:
	}
}

// blank replaces every scalar under n.
func blank(n *yaml.Node) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != "" {
			n.Value, n.Style, n.Tag = redacted, 0, "!!str"
		}
	default:
		for _, c := range n.Content {
			blank(c)
		}
	}
}

func inPath(path []string, k string) bool {
	for _, p := range path {
		if p == k {
			return true
		}
	}
	return false
}

var lineKeyRE = regexp.MustCompile(`^(\s*-?\s*)([A-Za-z0-9_.-]+)(\s*:\s*)(.+)$`)

// inlineKVRE finds key: value pairs inside a flow mapping ({name: a, password: b}).
var inlineKVRE = regexp.MustCompile(`([A-Za-z0-9_.-]+)(\s*:\s*)("[^"]*"|'[^']*'|[^,}\s]+)`)

func redactLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if m := lineKeyRE.FindStringSubmatch(l); m != nil && sensitiveKey(m[2]) {
			lines[i] = m[1] + m[2] + m[3] + redacted
			continue
		}
		l = inlineKVRE.ReplaceAllStringFunc(l, func(kv string) string {
			m := inlineKVRE.FindStringSubmatch(kv)
			if sensitiveKey(m[1]) {
				return m[1] + m[2] + redacted
			}
			return kv
		})
		lines[i] = config.RedactText(l)
	}
	return strings.Join(lines, "\n")
}
